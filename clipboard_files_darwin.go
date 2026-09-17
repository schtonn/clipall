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

// Replace a still-current lazy offer with ordinary local file URLs after the
// background download finishes. Verifying the marker prevents a slow download
// from overwriting clipboard content the user copied in the meantime.
static int clipallWriteDownloadedFilePaths(const char *offerID, const char *encodedPaths) {
	@autoreleasepool {
		if (offerID == NULL || encodedPaths == NULL) {
			return 0;
		}
		NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
		NSData *marker = [pasteboard dataForType:clipallRemoteFilesType()];
		NSString *currentID = marker == nil ? nil : [[[NSString alloc] initWithData:marker encoding:NSUTF8StringEncoding] autorelease];
		NSString *expectedID = [NSString stringWithUTF8String:offerID];
		if (currentID == nil || expectedID == nil || ![currentID isEqualToString:expectedID]) {
			return 2;
		}

		NSData *pathData = [NSData dataWithBytes:encodedPaths length:strlen(encodedPaths)];
		NSError *error = nil;
		id decoded = [NSJSONSerialization JSONObjectWithData:pathData options:0 error:&error];
		if (error != nil || ![decoded isKindOfClass:[NSArray class]] || [decoded count] == 0) {
			return 0;
		}
		NSMutableArray *urls = [NSMutableArray arrayWithCapacity:[decoded count]];
		for (id path in decoded) {
			if (![path isKindOfClass:[NSString class]]) {
				return 0;
			}
			NSURL *url = [NSURL fileURLWithPath:path];
			if (url == nil) {
				return 0;
			}
			[urls addObject:url];
		}
		[pasteboard clearContents];
		if (![pasteboard writeObjects:urls]) {
			return 0;
		}
		[clipallRemoteFileProviders release];
		clipallRemoteFileProviders = nil;
		return 1;
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func clipboardContainsFiles() bool {
	darwinClipboardMu.Lock()
	defer darwinClipboardMu.Unlock()
	return C.clipallPasteboardHasRemoteFiles() != 0 || C.clipallPasteboardHasFilePaths() != 0
}

type macPasteboardOfferRequest struct {
	offer   FileOffer
	offerID string
	paths   []string
	result  chan error
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
		darwinClipboardMu.Lock()
		sequence := int64(C.clipallPasteboardChangeCount())
		darwinClipboardMu.Unlock()
		for {
			select {
			case <-ctx.Done():
				return
			case request := <-macPasteboardOfferRequests:
				darwinClipboardMu.Lock()
				if len(request.paths) > 0 {
					request.result <- writeDownloadedFilePaths(request.offerID, request.paths)
				} else {
					request.result <- writeRemoteFileOffer(request.offer)
				}
				darwinClipboardMu.Unlock()
			case <-runLoopTicker.C:
				// Lazy pasteboard providers are serviced through the run loop of
				// the thread that registered them. A CLI has no AppKit main loop,
				// so keep this dedicated thread's loop moving explicitly.
				darwinClipboardMu.Lock()
				C.clipallRunLoopStep()
				darwinClipboardMu.Unlock()
			case <-clipboardTicker.C:
				darwinClipboardMu.Lock()
				current := int64(C.clipallPasteboardChangeCount())
				darwinClipboardMu.Unlock()
				if current == sequence {
					continue
				}
				sequence = current
				// Reading the lazy file URL would itself trigger the download. The
				// marker tells our source watcher to leave a remote offer alone.
				darwinClipboardMu.Lock()
				hasRemoteFiles := C.clipallPasteboardHasRemoteFiles() != 0
				darwinClipboardMu.Unlock()
				if hasRemoteFiles {
					select {
					case ch <- nil:
					case <-ctx.Done():
						return
					}
					continue
				}
				macRemoteFiles.clear()
				darwinClipboardMu.Lock()
				encoded := C.clipallPasteboardFilePaths()
				darwinClipboardMu.Unlock()
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
	cancel      context.CancelFunc
}

var macRemoteFiles macRemoteFileState

func (s *macRemoteFileState) set(ctx context.Context, offer FileOffer) {
	s.mu.Lock()
	s.cancelInFlightLocked()
	s.ctx, s.cancel = context.WithCancel(ctx)
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
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.done != nil {
		close(s.done)
	}
	s.inFlightID = ""
	s.done = nil
}

var errMacFileDownloadPending = errors.New("remote files are downloading")

func (s *macRemoteFileState) resolve(offerID string) ([]string, error) {
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
		s.mu.Unlock()
		return nil, errMacFileDownloadPending
	}
	ctx := s.ctx
	offer := s.offer
	s.inFlightID = offerID
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()

	// A pasteboard data provider runs synchronously on Finder's main thread.
	// Never wait for network or disk I/O here: start the transfer and return so
	// Finder can finish handling Command-V without becoming unresponsive.
	log.Printf("[files] Finder requested remote files; downloading in background")
	go s.download(ctx, offer, done)
	return nil, errMacFileDownloadPending
}

func (s *macRemoteFileState) download(ctx context.Context, offer FileOffer, done chan struct{}) {
	destinationResult := make(chan macFinderDestination, 1)
	go func() {
		destinationResult <- findMacFinderDestination(ctx)
	}()
	var progressWindow *macProgressWindow
	if shouldShowMacProgress(offer) {
		progressWindow, _ = startMacProgressWindow()
	}
	paths, err := receiveFileOfferWithProgress(ctx, offer, func(completed, total int64) {
		progressWindow.progress(completed, total)
	})
	if err != nil {
		if progressWindow == nil {
			progressWindow, _ = startMacProgressWindow()
		}
		progressWindow.finish("文件下载失败，请查看 Clipall 日志", false)
	}
	automaticallyPlaced := false
	if err == nil {
		destination := <-destinationResult
		if destination.err == nil {
			progressWindow.indeterminate("下载完成，正在放入 Finder…")
			var placementErr error
			paths, placementErr = placeMacDownloadedPaths(paths, destination.path)
			if placementErr == nil {
				automaticallyPlaced = true
			} else {
				log.Printf("[files] automatic Finder placement failed: %v", placementErr)
			}
		} else {
			log.Printf("[files] Finder destination unavailable (Automation may be denied): %v", destination.err)
		}
	}
	ready := false
	s.mu.Lock()
	if s.inFlightID == offer.ID && s.done == done {
		s.inFlightID = ""
		s.done = nil
		if s.offer.ID == offer.ID {
			s.paths = append([]string(nil), paths...)
			s.resultErr = err
			s.resultReady = true
			ready = err == nil
		}
		close(done)
	}
	s.mu.Unlock()
	if err != nil {
		log.Printf("[files] on-demand Finder download failed: %v", err)
		return
	}
	if !ready {
		return
	}
	if err := publishDownloadedFilePaths(ctx, offer.ID, paths); err != nil {
		log.Printf("[files] downloaded files were not republished: %v", err)
		if progressWindow == nil {
			progressWindow, _ = startMacProgressWindow()
		}
		progressWindow.finish("下载完成，但剪贴板已经改变", false)
		return
	}
	if automaticallyPlaced {
		progressWindow.finish("文件传输完成，已放入 Finder", true)
		log.Printf("[files] Finder file download complete and placed automatically")
		return
	}
	if progressWindow == nil {
		progressWindow, _ = startMacProgressWindow()
	}
	progressWindow.finish("文件下载完成，请在 Finder 中再次按 ⌘V", false)
	log.Printf("[files] Finder file download complete; paste again to place the files")
}

type macFinderDestination struct {
	path string
	err  error
}

func findMacFinderDestination(ctx context.Context) macFinderDestination {
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	script := `tell application "Finder" to POSIX path of (insertion location as alias)`
	output, err := exec.CommandContext(queryCtx, "/usr/bin/osascript", "-e", script).Output()
	if err != nil {
		return macFinderDestination{err: err}
	}
	destination := strings.TrimSpace(string(output))
	info, err := os.Stat(destination)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("Finder insertion location is not a directory")
		}
		return macFinderDestination{err: err}
	}
	return macFinderDestination{path: destination}
}

func placeMacDownloadedPaths(paths []string, destination string) ([]string, error) {
	placed := append([]string(nil), paths...)
	reserved := make(map[string]bool)
	targets := make([]string, len(paths))
	for index, source := range paths {
		target, err := availableMacDestination(destination, filepath.Base(source), reserved)
		if err != nil {
			return placed, err
		}
		targets[index] = target
		reserved[strings.ToLower(targets[index])] = true
	}
	for index, source := range paths {
		target := targets[index]
		if err := os.Rename(source, target); err == nil {
			placed[index] = target
			continue
		} else if !errors.Is(err, syscall.EXDEV) {
			return placed, fmt.Errorf("move %s: %w", filepath.Base(source), err)
		}
		temporary := target + fmt.Sprintf(".clipall-%d.part", os.Getpid())
		_ = os.RemoveAll(temporary)
		if err := copyMacPath(source, temporary); err != nil {
			_ = os.RemoveAll(temporary)
			return placed, fmt.Errorf("copy %s: %w", filepath.Base(source), err)
		}
		if err := os.Rename(temporary, target); err != nil {
			_ = os.RemoveAll(temporary)
			return placed, fmt.Errorf("finish %s: %w", filepath.Base(source), err)
		}
		if err := os.RemoveAll(source); err != nil {
			log.Printf("[files] warning: could not remove staged %s: %v", source, err)
		}
		placed[index] = target
	}
	return placed, nil
}

func availableMacDestination(directory, name string, reserved map[string]bool) (string, error) {
	candidate := filepath.Join(directory, name)
	if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) && !reserved[strings.ToLower(candidate)] {
		return candidate, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	for copyNumber := 2; ; copyNumber++ {
		candidate = filepath.Join(directory, fmt.Sprintf("%s (%d)%s", base, copyNumber, extension))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) && !reserved[strings.ToLower(candidate)] {
			return candidate, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
}

func copyMacPath(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyMacFile(source, target, info)
	}
	if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyMacPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	_ = os.Chtimes(target, info.ModTime(), info.ModTime())
	return nil
}

func copyMacFile(source, target string, info os.FileInfo) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	_ = os.Chtimes(target, info.ModTime(), info.ModTime())
	return nil
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

func writeDownloadedFilePaths(offerID string, paths []string) error {
	encoded, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	cOfferID := C.CString(offerID)
	cPaths := C.CString(string(encoded))
	defer C.free(unsafe.Pointer(cOfferID))
	defer C.free(unsafe.Pointer(cPaths))
	switch C.clipallWriteDownloadedFilePaths(cOfferID, cPaths) {
	case 1:
		return nil
	case 2:
		return fmt.Errorf("clipboard changed before download completed")
	default:
		return fmt.Errorf("write downloaded file paths to pasteboard")
	}
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

func publishDownloadedFilePaths(ctx context.Context, offerID string, paths []string) error {
	result := make(chan error, 1)
	request := macPasteboardOfferRequest{offerID: offerID, paths: paths, result: result}
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
