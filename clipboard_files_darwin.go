//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fno-objc-arc -fblocks
#cgo LDFLAGS: -framework AppKit -framework Foundation

#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <pthread.h>
#include <sys/time.h>
#include <dispatch/dispatch.h>
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

extern void clipallRemoteFilesRequested(char *offerID, long long requestedAtMillis);

static NSString *clipallRemoteFilesType(void) {
	return @"io.github.schtonn.clipall.remote-files.v1";
}

static NSString *clipallReceivedFilesType(void) {
	return @"io.github.schtonn.clipall.received-files.v1";
}

static long long clipallUnixMillis(void) {
	struct timeval now;
	gettimeofday(&now, NULL);
	return ((long long)now.tv_sec * 1000LL) + (now.tv_usec / 1000LL);
}

static unsigned long long clipallCurrentThreadID(void) {
	return (unsigned long long)pthread_mach_thread_np(pthread_self());
}

static void clipallProviderLog(const char *phase, NSString *offerID, int result) {
	fprintf(stderr,
	        "%lld [files-debug] pasteboard-provider phase=%s offer=%.12s thread=%llu result=%d\n",
	        clipallUnixMillis(), phase,
	        offerID == nil ? "(nil)" : [offerID UTF8String],
	        clipallCurrentThreadID(), result);
	fflush(stderr);
}

@interface ClipallRemoteFileProvider : NSObject <NSPasteboardItemDataProvider> {
	NSString *_offerID;
}
@property(copy) NSString *offerID;
@end

@implementation ClipallRemoteFileProvider
@synthesize offerID = _offerID;

- (void)pasteboard:(NSPasteboard *)pasteboard
              item:(NSPasteboardItem *)item
provideDataForType:(NSPasteboardType)type {
	clipallProviderLog("entered", _offerID, 0);
	if (![type isEqualToString:NSPasteboardTypeFileURL] || _offerID == nil) {
		clipallProviderLog("ignored", _offerID, 0);
		return;
	}

	// Finder waits synchronously for every advertised pasteboard flavor. Always
	// fulfill this request before doing anything else; returning without setting
	// data leaves pasteboardd waiting until its roughly 40-second timeout.
	BOOL fulfilled = [item setData:[NSData data] forType:NSPasteboardTypeFileURL];
	clipallProviderLog("fulfilled-empty", _offerID, fulfilled ? 1 : 0);

	// Starting Go code from the provider callback can itself delay pasteboardd.
	// Notify the downloader only after this synchronous callback has returned.
	char *identifier = strdup([_offerID UTF8String]);
	if (identifier != NULL) {
		long long requestedAtMillis = clipallUnixMillis();
		dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
			clipallRemoteFilesRequested(identifier, requestedAtMillis);
			free(identifier);
		});
		clipallProviderLog("download-dispatched", _offerID, 1);
	} else {
		clipallProviderLog("download-dispatched", _offerID, 0);
	}
	clipallProviderLog("returned", _offerID, fulfilled ? 1 : 0);
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
		NSRunLoop *loop = [NSRunLoop currentRunLoop];
		[loop runMode:NSDefaultRunLoopMode
		    beforeDate:[NSDate dateWithTimeIntervalSinceNow:0.01]];
		// Promise delivery can arrive through a common or AppKit-specific mode.
		// Drain those modes too instead of only servicing the default mode.
		NSDate *now = [NSDate date];
		[loop runMode:NSRunLoopCommonModes beforeDate:now];
		[loop runMode:NSEventTrackingRunLoopMode beforeDate:now];
		[loop runMode:NSModalPanelRunLoopMode beforeDate:now];
	}
}

static int clipallPasteboardHasRemoteFiles(void) {
	@autoreleasepool {
		return [[NSPasteboard generalPasteboard] availableTypeFromArray:@[clipallRemoteFilesType()]] != nil ? 1 : 0;
	}
}

static int clipallPasteboardHasReceivedFiles(void) {
	@autoreleasepool {
		return [[NSPasteboard generalPasteboard] availableTypeFromArray:@[clipallReceivedFilesType()]] != nil ? 1 : 0;
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
		NSMutableArray *items = [NSMutableArray arrayWithCapacity:[decoded count]];
		for (NSUInteger index = 0; index < [decoded count]; index++) {
			id path = [decoded objectAtIndex:index];
			if (![path isKindOfClass:[NSString class]]) {
				return 0;
			}
			NSURL *url = [NSURL fileURLWithPath:path];
			if (url == nil) {
				return 0;
			}
			NSPasteboardItem *item = [[[NSPasteboardItem alloc] init] autorelease];
			if (![item setString:[url absoluteString] forType:NSPasteboardTypeFileURL]) {
				return 0;
			}
			if (index == 0 && ![item setData:[NSData data] forType:clipallReceivedFilesType()]) {
				return 0;
			}
			[items addObject:item];
		}
		[pasteboard clearContents];
		if (![pasteboard writeObjects:items]) {
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
		heartbeatTicker := time.NewTicker(10 * time.Second)
		defer heartbeatTicker.Stop()
		darwinClipboardMu.Lock()
		sequence := int64(C.clipallPasteboardChangeCount())
		threadID := uint64(C.clipallCurrentThreadID())
		darwinClipboardMu.Unlock()
		log.Printf("[files-debug] pasteboard loop started: thread=%d, change=%d, pump=continuous, modes=default+common+event+modal", threadID, sequence)
		var pumpCount uint64
		var slowestPump time.Duration
		for {
			select {
			case <-ctx.Done():
				log.Printf("[files-debug] pasteboard loop stopped: thread=%d", threadID)
				return
			case request := <-macPasteboardOfferRequests:
				started := time.Now()
				darwinClipboardMu.Lock()
				var err error
				if len(request.paths) > 0 {
					err = writeDownloadedFilePaths(request.offerID, request.paths)
				} else {
					err = writeRemoteFileOffer(request.offer)
				}
				darwinClipboardMu.Unlock()
				request.result <- err
				log.Printf("[files-debug] pasteboard write finished: offer=%s, downloaded=%t, paths=%d, elapsed=%s, err=%v",
					shortMacOfferID(firstNonEmpty(request.offerID, request.offer.ID)), len(request.paths) > 0,
					len(request.paths), time.Since(started).Round(time.Millisecond), err)
			case <-heartbeatTicker.C:
				darwinClipboardMu.Lock()
				current := int64(C.clipallPasteboardChangeCount())
				hasRemote := C.clipallPasteboardHasRemoteFiles() != 0
				hasReceived := C.clipallPasteboardHasReceivedFiles() != 0
				darwinClipboardMu.Unlock()
				offerID, inFlightID, ready := macRemoteFiles.diagnosticSnapshot()
				if hasRemote || hasReceived || offerID != "" || inFlightID != "" {
					log.Printf("[files-debug] pasteboard heartbeat: thread=%d, pumps=%d, slowest=%s, observed_change=%d, current_change=%d, remote_marker=%t, received_marker=%t, offer=%s, inflight=%s, ready=%t",
						threadID, pumpCount, slowestPump.Round(time.Millisecond), sequence, current,
						hasRemote, hasReceived, shortMacOfferID(offerID), shortMacOfferID(inFlightID), ready)
				}
				pumpCount = 0
				slowestPump = 0
			case <-clipboardTicker.C:
				darwinClipboardMu.Lock()
				current := int64(C.clipallPasteboardChangeCount())
				darwinClipboardMu.Unlock()
				if current == sequence {
					continue
				}
				previous := sequence
				sequence = current
				darwinClipboardMu.Lock()
				hasRemoteFiles := C.clipallPasteboardHasRemoteFiles() != 0
				hasReceivedFiles := C.clipallPasteboardHasReceivedFiles() != 0
				darwinClipboardMu.Unlock()
				log.Printf("[files-debug] pasteboard changed: previous=%d, current=%d, remote_marker=%t, received_marker=%t",
					previous, current, hasRemoteFiles, hasReceivedFiles)
				// Reading the lazy file URL would itself trigger the download. The
				// marker tells our source watcher to leave a remote offer alone.
				if hasRemoteFiles {
					select {
					case ch <- nil:
					case <-ctx.Done():
						return
					}
					continue
				}
				// Downloaded paths are real file URLs, but they originated remotely.
				// Do not announce them back to peers as a fresh local copy.
				if hasReceivedFiles {
					macRemoteFiles.clear()
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
			default:
				// NSPasteboardItemDataProvider is serviced by the run loop of the
				// thread that registered it. Keep that loop active continuously;
				// the old ticker left a blind window after every run-loop slice.
				started := time.Now()
				darwinClipboardMu.Lock()
				C.clipallRunLoopStep()
				darwinClipboardMu.Unlock()
				elapsed := time.Since(started)
				pumpCount++
				if elapsed > slowestPump {
					slowestPump = elapsed
				}
				if elapsed >= 250*time.Millisecond {
					log.Printf("[files-debug] pasteboard run-loop step was slow: elapsed=%s, thread=%d", elapsed.Round(time.Millisecond), threadID)
				}
				// Some run loops return immediately while idle. Avoid spinning a
				// CPU while preserving sub-20 ms provider response latency.
				if elapsed < time.Millisecond {
					time.Sleep(time.Millisecond - elapsed)
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

func (s *macRemoteFileState) diagnosticSnapshot() (offerID, inFlightID string, ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offer.ID, s.inFlightID, s.resultReady
}

func shortMacOfferID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	if id == "" {
		return "-"
	}
	return id
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

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

func (s *macRemoteFileState) requestDownload(offerID string) error {
	s.mu.Lock()
	if s.ctx == nil || s.offer.ID != offerID {
		s.mu.Unlock()
		return fmt.Errorf("remote file offer is no longer current")
	}
	if s.resultReady {
		err := s.resultErr
		s.mu.Unlock()
		return err
	}
	if s.inFlightID == offerID {
		s.mu.Unlock()
		return errMacFileDownloadPending
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
	return errMacFileDownloadPending
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
	started := time.Now()
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
	files, directories, bytes := fileOfferStats(offer)
	log.Printf("[files-debug] lazy offer published: offer=%s, roots=%d, entries=%d, files=%d, folders=%d, bytes=%d, change=%d, elapsed=%s",
		shortMacOfferID(offer.ID), len(offer.Roots), len(offer.Entries), files, directories, bytes,
		int64(C.clipallPasteboardChangeCount()), time.Since(started).Round(time.Millisecond))
	return nil
}

func writeDownloadedFilePaths(offerID string, paths []string) error {
	started := time.Now()
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
		log.Printf("[files-debug] downloaded file URLs published: offer=%s, paths=%d, change=%d, elapsed=%s",
			shortMacOfferID(offerID), len(paths), int64(C.clipallPasteboardChangeCount()), time.Since(started).Round(time.Millisecond))
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
