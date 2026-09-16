//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fno-objc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation

#include <stdlib.h>
#include <string.h>
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

extern char *clipallProvideRemoteFiles(char *offerID);

static NSString *clipallRemoteFilesType(void) {
	return @"io.github.schtonn.clipall.remote-files.v1";
}

@interface ClipallRemoteFileProvider : NSObject <NSPasteboardItemDataProvider> {
	NSString *_offerID;
	NSUInteger _rootIndex;
}
@property(copy) NSString *offerID;
@property(assign) NSUInteger rootIndex;
@end

@implementation ClipallRemoteFileProvider
@synthesize offerID = _offerID;
@synthesize rootIndex = _rootIndex;

- (void)pasteboard:(NSPasteboard *)pasteboard
              item:(NSPasteboardItem *)item
provideDataForType:(NSPasteboardType)type {
	if (![type isEqualToString:NSPasteboardTypeFileURL] || _offerID == nil) {
		return;
	}
	char *encodedPaths = clipallProvideRemoteFiles((char *)[_offerID UTF8String]);
	if (encodedPaths == NULL) {
		return;
	}
	NSData *data = [NSData dataWithBytes:encodedPaths length:strlen(encodedPaths)];
	free(encodedPaths);
	NSError *error = nil;
	id decoded = [NSJSONSerialization JSONObjectWithData:data options:0 error:&error];
	if (error != nil || ![decoded isKindOfClass:[NSArray class]] || _rootIndex >= [decoded count]) {
		return;
	}
	id path = [decoded objectAtIndex:_rootIndex];
	if (![path isKindOfClass:[NSString class]]) {
		return;
	}
	NSURL *url = [NSURL fileURLWithPath:path];
	if (url != nil) {
		[item setString:[url absoluteString] forType:NSPasteboardTypeFileURL];
	}
}

- (void)pasteboardFinishedWithDataProvider:(NSPasteboard *)pasteboard {
}

- (void)dealloc {
	[_offerID release];
	[super dealloc];
}
@end

static NSMutableArray *clipallRemoteFileProviders = nil;

static long long clipallPasteboardChangeCount(void) {
	@autoreleasepool {
		return (long long)[[NSPasteboard generalPasteboard] changeCount];
	}
}

static void clipallRunLoopStep(void) {
	@autoreleasepool {
		[[NSRunLoop currentRunLoop] runMode:NSDefaultRunLoopMode
		                         beforeDate:[NSDate dateWithTimeIntervalSinceNow:0.01]];
	}
}

static int clipallPasteboardHasRemoteFiles(void) {
	@autoreleasepool {
		return [[NSPasteboard generalPasteboard] availableTypeFromArray:@[clipallRemoteFilesType()]] != nil ? 1 : 0;
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

static int clipallWriteRemoteFileOffer(const char *offerID, const char *encodedRoots) {
	@autoreleasepool {
		if (offerID == NULL || encodedRoots == NULL) {
			return 0;
		}
		NSString *identifier = [NSString stringWithUTF8String:offerID];
		NSData *rootData = [NSData dataWithBytes:encodedRoots length:strlen(encodedRoots)];
		NSError *error = nil;
		id decoded = [NSJSONSerialization JSONObjectWithData:rootData options:0 error:&error];
		if (identifier == nil || error != nil || ![decoded isKindOfClass:[NSArray class]] || [decoded count] == 0) {
			return 0;
		}

		NSMutableArray *items = [NSMutableArray arrayWithCapacity:[decoded count]];
		NSMutableArray *providers = [NSMutableArray arrayWithCapacity:[decoded count]];
		NSData *marker = [identifier dataUsingEncoding:NSUTF8StringEncoding];
		for (NSUInteger index = 0; index < [decoded count]; index++) {
			NSPasteboardItem *item = [[[NSPasteboardItem alloc] init] autorelease];
			ClipallRemoteFileProvider *provider = [[[ClipallRemoteFileProvider alloc] init] autorelease];
			provider.offerID = identifier;
			provider.rootIndex = index;
			if (![item setDataProvider:provider forTypes:@[NSPasteboardTypeFileURL]]) {
				return 0;
			}
			if (index == 0 && ![item setData:marker forType:clipallRemoteFilesType()]) {
				return 0;
			}
			[items addObject:item];
			[providers addObject:provider];
		}

		NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
		[pasteboard clearContents];
		if (![pasteboard writeObjects:items]) {
			return 0;
		}
		[clipallRemoteFileProviders release];
		clipallRemoteFileProviders = [providers mutableCopy];
		return 1;
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
	"unsafe"
)

func clipboardContainsFiles() bool {
	return C.clipallPasteboardHasRemoteFiles() != 0 || C.clipallPasteboardHasFilePaths() != 0
}

type macPasteboardOfferRequest struct {
	offer  FileOffer
	result chan error
}

var macPasteboardOfferRequests = make(chan macPasteboardOfferRequest)

func watchFiles(ctx context.Context) <-chan []string {
	ch := make(chan []string, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(ch)
		clipboardTicker := time.NewTicker(500 * time.Millisecond)
		defer clipboardTicker.Stop()
		runLoopTicker := time.NewTicker(20 * time.Millisecond)
		defer runLoopTicker.Stop()
		sequence := int64(C.clipallPasteboardChangeCount())
		for {
			select {
			case <-ctx.Done():
				return
			case request := <-macPasteboardOfferRequests:
				request.result <- writeRemoteFileOffer(request.offer)
			case <-runLoopTicker.C:
				// Lazy pasteboard providers are serviced through the run loop of
				// the thread that registered them. A CLI has no AppKit main loop,
				// so keep this dedicated thread's loop moving explicitly.
				C.clipallRunLoopStep()
			case <-clipboardTicker.C:
				current := int64(C.clipallPasteboardChangeCount())
				if current == sequence {
					continue
				}
				sequence = current
				// Reading the lazy file URL would itself trigger the download. The
				// marker tells our source watcher to leave a remote offer alone.
				if C.clipallPasteboardHasRemoteFiles() != 0 {
					select {
					case ch <- nil:
					case <-ctx.Done():
						return
					}
					continue
				}
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

type macRemoteFileState struct {
	mu          sync.Mutex
	ctx         context.Context
	offer       FileOffer
	paths       []string
	resultErr   error
	resultReady bool
	inFlightID  string
	done        chan struct{}
}

var macRemoteFiles macRemoteFileState

func (s *macRemoteFileState) set(ctx context.Context, offer FileOffer) {
	s.mu.Lock()
	s.cancelInFlightLocked()
	s.ctx = ctx
	s.offer = offer
	s.paths = nil
	s.resultErr = nil
	s.resultReady = false
	s.mu.Unlock()
}

func (s *macRemoteFileState) clear() {
	s.mu.Lock()
	s.cancelInFlightLocked()
	s.ctx = nil
	s.offer = FileOffer{}
	s.paths = nil
	s.resultErr = nil
	s.resultReady = false
	s.mu.Unlock()
}

func (s *macRemoteFileState) cancelInFlightLocked() {
	if s.done != nil {
		close(s.done)
	}
	s.inFlightID = ""
	s.done = nil
}

func (s *macRemoteFileState) resolve(offerID string) ([]string, error) {
	for {
		s.mu.Lock()
		if s.ctx == nil || s.offer.ID != offerID {
			s.mu.Unlock()
			return nil, fmt.Errorf("remote file offer is no longer current")
		}
		if s.resultReady {
			paths := append([]string(nil), s.paths...)
			err := s.resultErr
			s.mu.Unlock()
			return paths, err
		}
		if s.inFlightID == offerID {
			done := s.done
			ctx := s.ctx
			s.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		ctx := s.ctx
		offer := s.offer
		s.inFlightID = offerID
		s.done = make(chan struct{})
		done := s.done
		s.mu.Unlock()

		log.Printf("[files] Finder requested remote files; downloading on demand")
		paths, err := receiveFileOffer(ctx, offer)

		s.mu.Lock()
		if s.inFlightID == offerID {
			s.inFlightID = ""
			s.done = nil
			if s.offer.ID == offerID {
				s.paths = append([]string(nil), paths...)
				s.resultErr = err
				s.resultReady = true
			}
			close(done)
		}
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		log.Printf("[files] Finder file download complete")
		return paths, nil
	}
}

func writeRemoteFileOffer(offer FileOffer) error {
	roots, err := json.Marshal(offer.Roots)
	if err != nil {
		return err
	}
	offerID := C.CString(offer.ID)
	encodedRoots := C.CString(string(roots))
	defer C.free(unsafe.Pointer(offerID))
	defer C.free(unsafe.Pointer(encodedRoots))
	if C.clipallWriteRemoteFileOffer(offerID, encodedRoots) == 0 {
		return fmt.Errorf("write lazy remote files to pasteboard")
	}
	return nil
}

func publishRemoteFileOffer(ctx context.Context, offer FileOffer) error {
	result := make(chan error, 1)
	request := macPasteboardOfferRequest{offer: offer, result: result}
	select {
	case macPasteboardOfferRequests <- request:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runFilePasteHandler(ctx context.Context, offers <-chan FileOffer) {
	defer macRemoteFiles.clear()
	for {
		select {
		case <-ctx.Done():
			return
		case offer := <-offers:
			macRemoteFiles.set(ctx, offer)
			if err := publishRemoteFileOffer(ctx, offer); err != nil {
				macRemoteFiles.clear()
				log.Printf("[files] failed to publish lazy Finder paste: %v", err)
				continue
			}
			log.Printf("[files] remote files are ready for on-demand Finder paste")
		}
	}
}
