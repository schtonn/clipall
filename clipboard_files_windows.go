//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	shell32Files = syscall.NewLazyDLL("shell32.dll")

	procDragQueryFileW           = shell32Files.NewProc("DragQueryFileW")
	procEmptyClipboard           = user32w.NewProc("EmptyClipboard")
	procSetClipboardData         = user32w.NewProc("SetClipboardData")
	procRegisterClipboardFormatW = user32w.NewProc("RegisterClipboardFormatW")
	procGetAsyncKeyState         = user32w.NewProc("GetAsyncKeyState")
	procKeybdEvent               = user32w.NewProc("keybd_event")
	procGlobalAlloc              = kernel32w.NewProc("GlobalAlloc")
	procGlobalFree               = kernel32w.NewProc("GlobalFree")
)

const (
	cfHDrop           = 15
	gmemMoveable      = 0x0002
	vkControl         = 0x11
	vkShift           = 0x10
	vkMenu            = 0x12
	vkV               = 0x56
	keyeventfKeyUp    = 0x0002
	remoteFilesFormat = "clipall.remote-files.v1"
)

func openClipboardForFiles() bool {
	for attempt := 0; attempt < 20; attempt++ {
		opened, _, _ := procOpenClipboard.Call(0)
		if opened != 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func readClipboardFiles() []string {
	if !openClipboardForFiles() {
		return nil
	}
	defer procCloseClipboard.Call()
	available, _, _ := procIsClipboardFormatAvailable.Call(cfHDrop)
	if available == 0 {
		return nil
	}
	hDrop, _, _ := procGetClipboardData.Call(cfHDrop)
	if hDrop == 0 {
		return nil
	}
	count, _, _ := procDragQueryFileW.Call(hDrop, ^uintptr(0), 0, 0)
	paths := make([]string, 0, count)
	for index := uintptr(0); index < count; index++ {
		length, _, _ := procDragQueryFileW.Call(hDrop, index, 0, 0)
		if length == 0 {
			continue
		}
		buffer := make([]uint16, length+1)
		procDragQueryFileW.Call(hDrop, index, uintptr(unsafe.Pointer(&buffer[0])), length+1)
		paths = append(paths, syscall.UTF16ToString(buffer))
	}
	return paths
}

func clipboardContainsFiles() bool {
	available, _, _ := procIsClipboardFormatAvailable.Call(cfHDrop)
	return available != 0
}

func watchFiles(ctx context.Context) <-chan []string {
	ch := make(chan []string, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(ch)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		sequence, _, _ := procGetClipboardSequenceNumber.Call()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, _, _ := procGetClipboardSequenceNumber.Call()
				if current == sequence {
					continue
				}
				sequence = current
				paths := readClipboardFiles()
				if receivedStagingSelection(paths) {
					paths = nil
				}
				select {
				case ch <- paths:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch
}

func receivedStagingSelection(paths []string) bool {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return false
	}
	stagingRoot := filepath.Clean(filepath.Join(cacheDir, "clipall", "files")) + string(filepath.Separator)
	for _, path := range paths {
		clean := filepath.Clean(path)
		if !strings.HasPrefix(strings.ToLower(clean), strings.ToLower(stagingRoot)) {
			return false
		}
	}
	return true
}

func registeredRemoteFilesFormat() (uintptr, error) {
	name, err := syscall.UTF16PtrFromString(remoteFilesFormat)
	if err != nil {
		return 0, err
	}
	format, _, callErr := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	if format == 0 {
		return 0, fmt.Errorf("RegisterClipboardFormatW: %v", callErr)
	}
	return format, nil
}

func writeClipboardMemory(format uintptr, data []byte) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hMem, _, callErr := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc: %v", callErr)
	}
	ptr, _, callErr := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return fmt.Errorf("GlobalLock: %v", callErr)
	}
	copy(unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&ptr))), len(data)), data)
	procGlobalUnlock.Call(hMem)
	if !openClipboardForFiles() {
		procGlobalFree.Call(hMem)
		return fmt.Errorf("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()
	if emptied, _, callErr := procEmptyClipboard.Call(); emptied == 0 {
		procGlobalFree.Call(hMem)
		return fmt.Errorf("EmptyClipboard: %v", callErr)
	}
	if result, _, callErr := procSetClipboardData.Call(format, hMem); result == 0 {
		procGlobalFree.Call(hMem)
		return fmt.Errorf("SetClipboardData: %v", callErr)
	}
	return nil
}

func writeRemoteFileMarker(offerID string) error {
	format, err := registeredRemoteFilesFormat()
	if err != nil {
		return err
	}
	return writeClipboardMemory(format, append([]byte(offerID), 0))
}

func remoteFileMarkerMatches(offerID string) bool {
	format, err := registeredRemoteFilesFormat()
	if err != nil || !openClipboardForFiles() {
		return false
	}
	defer procCloseClipboard.Call()
	available, _, _ := procIsClipboardFormatAvailable.Call(format)
	if available == 0 {
		return false
	}
	hMem, _, _ := procGetClipboardData.Call(format)
	if hMem == 0 {
		return false
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return false
	}
	defer procGlobalUnlock.Call(hMem)
	size, _, _ := procGlobalSize.Call(hMem)
	if size == 0 || size > 128 {
		return false
	}
	data := make([]byte, size)
	copy(data, unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&ptr))), size))
	return strings.TrimRight(string(data), "\x00") == offerID
}

func writeClipboardFiles(paths []string) error {
	data, err := encodeHDrop(paths)
	if err != nil {
		return err
	}
	return writeClipboardMemory(cfHDrop, data)
}

func encodeHDrop(paths []string) ([]byte, error) {
	words := make([]uint16, 0)
	for _, path := range paths {
		encoded, err := syscall.UTF16FromString(path)
		if err != nil {
			return nil, err
		}
		words = append(words, encoded...)
	}
	words = append(words, 0)
	data := make([]byte, 20+len(words)*2)
	data[0] = 20 // DROPFILES.pFiles
	data[16] = 1 // DROPFILES.fWide
	for i, word := range words {
		data[20+i*2] = byte(word)
		data[21+i*2] = byte(word >> 8)
	}
	return data, nil
}

func keyDown(key uintptr) bool {
	state, _, _ := procGetAsyncKeyState.Call(key)
	return state&0x8000 != 0
}

func injectPasteWhenReleased(ctx context.Context) {
	for keyDown(vkControl) || keyDown(vkV) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	procKeybdEvent.Call(vkControl, 0, 0, 0)
	procKeybdEvent.Call(vkV, 0, 0, 0)
	procKeybdEvent.Call(vkV, 0, keyeventfKeyUp, 0)
	procKeybdEvent.Call(vkControl, 0, keyeventfKeyUp, 0)
}

type filePasteResult struct {
	offerID string
	paths   []string
	err     error
}

func runFilePasteHandler(ctx context.Context, offers <-chan FileOffer) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	results := make(chan filePasteResult, 1)
	var current *FileOffer
	var wasPasteDown bool
	downloading := false
	for {
		select {
		case <-ctx.Done():
			return
		case offer := <-offers:
			if err := writeRemoteFileMarker(offer.ID); err != nil {
				log.Printf("[files] failed to put remote file marker on clipboard: %v", err)
				current = nil
				continue
			}
			current = &offer
		case result := <-results:
			downloading = false
			if result.err != nil {
				log.Printf("[files] on-demand download failed: %v", result.err)
				continue
			}
			if current == nil || current.ID != result.offerID || !remoteFileMarkerMatches(result.offerID) {
				continue
			}
			if err := writeClipboardFiles(result.paths); err != nil {
				log.Printf("[files] failed to publish downloaded files: %v", err)
				continue
			}
			log.Printf("[files] download complete; continuing the requested paste")
			current = nil
			go injectPasteWhenReleased(ctx)
		case <-ticker.C:
			pasteDown := keyDown(vkControl) && keyDown(vkV) && !keyDown(vkShift) && !keyDown(vkMenu)
			if pasteDown && !wasPasteDown && !downloading && current != nil && remoteFileMarkerMatches(current.ID) {
				offer := *current
				downloading = true
				log.Printf("[files] remote file paste requested; downloading on demand")
				go func() {
					paths, err := receiveFileOffer(ctx, offer)
					select {
					case results <- filePasteResult{offerID: offer.ID, paths: paths, err: err}:
					case <-ctx.Done():
					}
				}()
			}
			wasPasteDown = pasteDown
		}
	}
}
