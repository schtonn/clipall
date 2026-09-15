//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fno-objc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation

#include <stdlib.h>
#include <string.h>
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

static long long clipallPasteboardChangeCount(void) {
	@autoreleasepool {
		return (long long)[[NSPasteboard generalPasteboard] changeCount];
	}
}

static int clipallPasteboardHasFilePaths(void) {
	@autoreleasepool {
		NSDictionary *options = @{NSPasteboardURLReadingFileURLsOnlyKey: @YES};
		return [[NSPasteboard generalPasteboard] canReadObjectForClasses:@[[NSURL class]] options:options] ? 1 : 0;
	}
}

static char *clipallPasteboardFilePaths(void) {
	@autoreleasepool {
		NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
		NSDictionary *options = @{NSPasteboardURLReadingFileURLsOnlyKey: @YES};
		NSArray *urls = [pasteboard readObjectsForClasses:@[[NSURL class]] options:options];
		if (urls == nil || [urls count] == 0) {
			return NULL;
		}
		NSMutableArray *paths = [NSMutableArray arrayWithCapacity:[urls count]];
		for (NSURL *url in urls) {
			if ([url isFileURL] && [url path] != nil) {
				[paths addObject:[url path]];
			}
		}
		if ([paths count] == 0) {
			return NULL;
		}
		NSError *error = nil;
		NSData *json = [NSJSONSerialization dataWithJSONObject:paths options:0 error:&error];
		if (json == nil || error != nil) {
			return NULL;
		}
		NSString *text = [[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding];
		if (text == nil) {
			return NULL;
		}
		char *result = strdup([text UTF8String]);
		[text release];
		return result;
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"log"
	"time"
	"unsafe"
)

func clipboardContainsFiles() bool {
	return C.clipallPasteboardHasFilePaths() != 0
}

func watchFiles(ctx context.Context) <-chan []string {
	ch := make(chan []string, 1)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		sequence := int64(C.clipallPasteboardChangeCount())
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current := int64(C.clipallPasteboardChangeCount())
				if current == sequence {
					continue
				}
				sequence = current
				encoded := C.clipallPasteboardFilePaths()
				if encoded == nil {
					select {
					case ch <- nil:
					case <-ctx.Done():
						return
					}
					continue
				}
				data := C.GoString(encoded)
				C.free(unsafe.Pointer(encoded))
				var paths []string
				if json.Unmarshal([]byte(data), &paths) != nil {
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

func runFilePasteHandler(ctx context.Context, offers <-chan FileOffer) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-offers:
			log.Printf("[files] incoming file offer ignored: this prototype supports Windows as the paste destination")
			// Finder-targeted lazy file promises require a native pasteboard
			// provider. The first prototype supports macOS as a source and
			// Windows Explorer as the destination.
		}
	}
}
