//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/png"
	"log"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/image/bmp"
)

var (
	user32w   = syscall.NewLazyDLL("user32.dll")
	kernel32w = syscall.NewLazyDLL("kernel32.dll")

	procOpenClipboard              = user32w.NewProc("OpenClipboard")
	procCloseClipboard             = user32w.NewProc("CloseClipboard")
	procGetClipboardData           = user32w.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32w.NewProc("IsClipboardFormatAvailable")
	procGetClipboardSequenceNumber = user32w.NewProc("GetClipboardSequenceNumber")
	procGlobalLock                 = kernel32w.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32w.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32w.NewProc("GlobalSize")
)

const cfDIB = 8

// readImageDIB reads the clipboard image as CF_DIB and converts to PNG. Keep
// OpenClipboard, GlobalLock and CloseClipboard on one OS thread: the Go
// scheduler may otherwise migrate the caller while the clipboard is open.
func readImageDIB() []byte {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return nil
	}
	defer procCloseClipboard.Call()

	r, _, _ = procIsClipboardFormatAvailable.Call(cfDIB)
	if r == 0 {
		return nil
	}

	hMem, _, _ := procGetClipboardData.Call(cfDIB)
	if hMem == 0 {
		return nil
	}

	ptrVal, _, _ := procGlobalLock.Call(hMem)
	if ptrVal == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(hMem)

	size, _, _ := procGlobalSize.Call(hMem)
	if size == 0 || size > MaxPayloadSize+4096 {
		return nil
	}

	// Copy DIB data from global memory.
	dibData := make([]byte, int(size))
	copy(dibData, unsafe.Slice((*byte)(windowsPointer(ptrVal)), int(size)))

	if len(dibData) < 40 {
		return nil
	}
	headerSize := binary.LittleEndian.Uint32(dibData[:4])

	// Compute pixel data offset: header + optional color table.
	dataOffset := headerSize
	bitCount := binary.LittleEndian.Uint16(dibData[14:16])
	compression := binary.LittleEndian.Uint32(dibData[16:20])
	if bitCount <= 8 {
		clrUsed := binary.LittleEndian.Uint32(dibData[32:36])
		if clrUsed == 0 {
			clrUsed = 1 << bitCount
		}
		dataOffset += clrUsed * 4
	} else if headerSize == 40 && compression == 3 /* BI_BITFIELDS */ {
		dataOffset += 12 // three DWORD color masks
	}

	// Construct BMP file: 14-byte file header + DIB data.
	fileHeader := make([]byte, 14)
	fileHeader[0] = 'B'
	fileHeader[1] = 'M'
	binary.LittleEndian.PutUint32(fileHeader[2:6], uint32(14+len(dibData)))
	binary.LittleEndian.PutUint32(fileHeader[10:14], 14+dataOffset)

	bmpData := append(fileHeader, dibData...)

	img, err := bmp.Decode(bytes.NewReader(bmpData))
	if err != nil {
		log.Printf("[clipboard] DIB: bmp decode failed: %v", err)
		return nil
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Printf("[clipboard] DIB: png encode failed: %v", err)
		return nil
	}

	return buf.Bytes()
}

// readImage is also used immediately after writing a remotely received image
// so its re-encoded bytes can be registered as an expected clipboard echo.
// Do not use x/clipboard's Windows readImage here: it dereferences clipboard
// memory owned by another process and can raise an unrecoverable access
// violation when the clipboard changes concurrently.
func readImage() []byte {
	return readImageDIB()
}

// watchImage watches the clipboard for image changes on Windows.
// Uses our own CF_DIB reader instead of the library's Read(FmtImage),
// which has two issues: (1) rejects 24-bit DIBV5 without CF_DIB fallback,
// (2) opens/closes the clipboard without LockOSThread, risking thread
// migration between Open and Close that permanently locks the clipboard.
func watchImage(ctx context.Context) <-chan []byte {
	ch := make(chan []byte, 1)
	go func() {
		// Windows clipboard API has thread affinity: OpenClipboard and
		// CloseClipboard MUST run on the same OS thread. Without this
		// lock, Go's goroutine scheduler can migrate us between threads,
		// causing CloseClipboard to run on a different thread than
		// OpenClipboard, which silently leaves the clipboard locked.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(ch)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		seq, _, _ := procGetClipboardSequenceNumber.Call()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cur, _, _ := procGetClipboardSequenceNumber.Call()
				if cur == seq {
					continue
				}
				// Read image via CF_DIB (handles all bit depths).
				// Retry up to 3 times with short delays to handle clipboard
				// contention with the library's text watcher.
				var data []byte
				for attempt := 0; attempt < 3; attempt++ {
					if attempt > 0 {
						time.Sleep(100 * time.Millisecond)
					}
					data = readImageDIB()
					if data != nil {
						break
					}
				}
				if len(data) == 0 {
					continue // Don't update seq — retry on next tick.
				}
				log.Printf("[clipboard] image read via CF_DIB (%d bytes)", len(data))
				seq = cur // Only update after successful read.
				select {
				case ch <- data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch
}
