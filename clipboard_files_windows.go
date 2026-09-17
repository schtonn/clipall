//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
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
	maxHDropBytes     = 16 << 20
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
	size, _, _ := procGlobalSize.Call(hDrop)
	if size < dropFilesHeaderSize || size > maxHDropBytes {
		log.Printf("[files] ignoring invalid CF_HDROP size %d", size)
		return nil
	}
	ptr, _, _ := procGlobalLock.Call(hDrop)
	if ptr == 0 {
		return nil
	}
	// Clipboard ownership can change immediately after CloseClipboard. Copy the
	// complete HGLOBAL while it is both locked and protected by OpenClipboard,
	// then parse only Go-owned memory. Calling DragQueryFileW with the clipboard
	// handle allowed malformed or concurrently-replaced data to crash inside
	// shell32.dll with an unrecoverable access violation.
	data := make([]byte, int(size))
	copy(data, unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size)))
	procGlobalUnlock.Call(hDrop)
	paths, err := decodeHDrop(data)
	if err != nil {
		log.Printf("[files] ignoring invalid CF_HDROP data: %v", err)
		return nil
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
	copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(data)), data)
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
	data := make([]byte, int(size))
	copy(data, unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size)))
	return strings.TrimRight(string(data), "\x00") == offerID
}

func writeClipboardFiles(paths []string) error {
	data, err := encodeHDrop(paths)
	if err != nil {
		return err
	}
	return writeClipboardMemory(cfHDrop, data)
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
