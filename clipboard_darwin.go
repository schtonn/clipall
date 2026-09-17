//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fno-objc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation

#include <stdlib.h>
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

enum {
	clipallClipboardText = 1,
	clipallClipboardImage = 2,
	clipallClipboardEmpty = 0,
	clipallClipboardOK = 1,
	clipallClipboardTooLarge = 2,
	clipallClipboardFailed = 3
};

static NSPasteboardType clipallClipboardType(int kind) {
	return kind == clipallClipboardImage ? NSPasteboardTypePNG : NSPasteboardTypeString;
}

// Copy at most maxSize bytes. size_t is intentional: the previous clipboard
// dependency returned an NSUInteger through unsigned int, which could truncate
// large pasteboard values before C.GoBytes copied them.
static int clipallReadClipboardData(int kind, void **out, size_t *outSize, size_t maxSize) {
	@autoreleasepool {
		*out = NULL;
		*outSize = 0;
		NSData *data = [[NSPasteboard generalPasteboard] dataForType:clipallClipboardType(kind)];
		if (data == nil) {
			return clipallClipboardEmpty;
		}
		NSUInteger length = [data length];
		if (length > maxSize) {
			*outSize = (size_t)length;
			return clipallClipboardTooLarge;
		}
		if (length == 0) {
			return clipallClipboardOK;
		}
		void *buffer = malloc((size_t)length);
		if (buffer == NULL) {
			return clipallClipboardFailed;
		}
		[data getBytes:buffer length:length];
		*out = buffer;
		*outSize = (size_t)length;
		return clipallClipboardOK;
	}
}

static int clipallWriteClipboardData(int kind, const void *bytes, size_t length) {
	@autoreleasepool {
		NSData *data = [NSData dataWithBytes:bytes length:(NSUInteger)length];
		if (data == nil) {
			return 0;
		}
		NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
		[pasteboard clearContents];
		return [pasteboard setData:data forType:clipallClipboardType(kind)] ? 1 : 0;
	}
}

static long long clipallClipboardSequence(void) {
	@autoreleasepool {
		return (long long)[[NSPasteboard generalPasteboard] changeCount];
	}
}
*/
import "C"

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

var darwinClipboardMu sync.Mutex

type darwinClipboardRead struct {
	data     []byte
	size     uint64
	tooLarge bool
}

func initClipboard() error { return nil }

func readDarwinClipboard(kind C.int) darwinClipboardRead {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	darwinClipboardMu.Lock()
	defer darwinClipboardMu.Unlock()
	var data unsafe.Pointer
	var size C.size_t
	status := C.clipallReadClipboardData(kind, &data, &size, C.size_t(MaxPayloadSize))
	if data != nil {
		defer C.free(data)
	}
	result := darwinClipboardRead{size: uint64(size)}
	switch status {
	case C.clipallClipboardOK:
		if size > 0 {
			result.data = C.GoBytes(data, C.int(size))
		}
	case C.clipallClipboardTooLarge:
		result.tooLarge = true
	}
	return result
}

func writeDarwinClipboard(kind C.int, data []byte) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	darwinClipboardMu.Lock()
	defer darwinClipboardMu.Unlock()
	var pointer unsafe.Pointer
	if len(data) > 0 {
		pointer = unsafe.Pointer(&data[0])
	}
	if C.clipallWriteClipboardData(kind, pointer, C.size_t(len(data))) == 0 {
		return fmt.Errorf("clipboard rejected write")
	}
	return nil
}

func watchDarwinClipboard(ctx context.Context, kind C.int, label string) <-chan []byte {
	output := make(chan []byte, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(output)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		darwinClipboardMu.Lock()
		sequence := int64(C.clipallClipboardSequence())
		darwinClipboardMu.Unlock()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				darwinClipboardMu.Lock()
				current := int64(C.clipallClipboardSequence())
				darwinClipboardMu.Unlock()
				if current == sequence {
					continue
				}
				sequence = current
				result := readDarwinClipboard(kind)
				if result.tooLarge {
					log.Printf("[clipboard] ignoring %s larger than %d bytes (got %d)", label, MaxPayloadSize, result.size)
					continue
				}
				if len(result.data) == 0 {
					continue
				}
				select {
				case output <- result.data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return output
}

func watchText(ctx context.Context) <-chan []byte {
	return watchDarwinClipboard(ctx, C.clipallClipboardText, "text")
}

func watchImage(ctx context.Context) <-chan []byte {
	return watchDarwinClipboard(ctx, C.clipallClipboardImage, "image")
}

func writeText(data []byte) error {
	return writeDarwinClipboard(C.clipallClipboardText, data)
}

func readText() []byte {
	return readDarwinClipboard(C.clipallClipboardText).data
}

func writeImage(data []byte) error {
	return writeDarwinClipboard(C.clipallClipboardImage, data)
}

func readImage() []byte {
	return readDarwinClipboard(C.clipallClipboardImage).data
}
