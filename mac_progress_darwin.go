//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fno-objc-arc -fblocks
#cgo LDFLAGS: -framework AppKit -framework Foundation

#include <stdlib.h>
#include <string.h>
#include <dispatch/dispatch.h>
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>

static NSPanel *clipallProgressPanel;
static NSTextField *clipallProgressLabel;
static NSProgressIndicator *clipallProgressBar;

static void clipallProgressPrepare(void) {
	@autoreleasepool {
		NSApplication *application = [NSApplication sharedApplication];
		[application setActivationPolicy:NSApplicationActivationPolicyAccessory];
		NSRect frame = NSMakeRect(0, 0, 430, 142);
		clipallProgressPanel = [[NSPanel alloc]
			initWithContentRect:frame
			styleMask:NSWindowStyleMaskTitled
			backing:NSBackingStoreBuffered
			defer:NO];
		[clipallProgressPanel setTitle:@"Clipall 文件传输"];
		[clipallProgressPanel setLevel:NSFloatingWindowLevel];
		[clipallProgressPanel center];

		clipallProgressLabel = [[NSTextField alloc] initWithFrame:NSMakeRect(24, 76, 382, 42)];
		[clipallProgressLabel setEditable:NO];
		[clipallProgressLabel setSelectable:NO];
		[clipallProgressLabel setBezeled:NO];
		[clipallProgressLabel setDrawsBackground:NO];
		[clipallProgressLabel setStringValue:@"正在准备远程文件…"];
		[[clipallProgressPanel contentView] addSubview:clipallProgressLabel];

		clipallProgressBar = [[NSProgressIndicator alloc] initWithFrame:NSMakeRect(24, 42, 382, 18)];
		[clipallProgressBar setMinValue:0.0];
		[clipallProgressBar setMaxValue:1.0];
		[clipallProgressBar setDoubleValue:0.0];
		[clipallProgressBar setIndeterminate:NO];
		[[clipallProgressPanel contentView] addSubview:clipallProgressBar];
		[clipallProgressPanel orderFrontRegardless];
	}
}

static void clipallProgressUpdate(double fraction, int indeterminate, const char *text, int done, int holdMS) {
	char *copied = text == NULL ? strdup("") : strdup(text);
	dispatch_async(dispatch_get_main_queue(), ^{
		@autoreleasepool {
			NSString *message = [NSString stringWithUTF8String:copied];
			free(copied);
			if (message != nil) {
				[clipallProgressLabel setStringValue:message];
			}
			[clipallProgressBar setIndeterminate:indeterminate != 0];
			if (indeterminate != 0) {
				[clipallProgressBar startAnimation:nil];
			} else {
				[clipallProgressBar stopAnimation:nil];
				[clipallProgressBar setDoubleValue:fraction];
			}
			if (done != 0) {
				dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)holdMS * NSEC_PER_MSEC),
				               dispatch_get_main_queue(), ^{
					[NSApp stop:nil];
					NSEvent *event = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
					                              location:NSZeroPoint modifierFlags:0 timestamp:0
					                           windowNumber:0 context:nil subtype:0 data1:0 data2:0];
					[NSApp postEvent:event atStart:NO];
				});
			}
		}
	});
}

static void clipallProgressRun(void) {
	@autoreleasepool {
		[NSApp run];
		[clipallProgressPanel orderOut:nil];
		[clipallProgressBar release];
		[clipallProgressLabel release];
		[clipallProgressPanel release];
		clipallProgressBar = nil;
		clipallProgressLabel = nil;
		clipallProgressPanel = nil;
	}
}
*/
import "C"

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"unsafe"
)

const macProgressHelperArg = "--clipall-internal-macos-progress"

type macProgressMessage struct {
	Fraction      float64 `json:"fraction,omitempty"`
	Indeterminate bool    `json:"indeterminate,omitempty"`
	Text          string  `json:"text"`
	Done          bool    `json:"done,omitempty"`
	HoldMS        int     `json:"hold_ms,omitempty"`
}

func runInternalPlatformMode(args []string) (bool, error) {
	if len(args) != 1 || args[0] != macProgressHelperArg {
		return false, nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	C.clipallProgressPrepare()
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		finished := false
		for scanner.Scan() {
			var message macProgressMessage
			if json.Unmarshal(scanner.Bytes(), &message) != nil {
				continue
			}
			encoded := C.CString(message.Text)
			indeterminate := 0
			if message.Indeterminate {
				indeterminate = 1
			}
			done := 0
			if message.Done {
				done = 1
				finished = true
			}
			hold := message.HoldMS
			if hold < 0 {
				hold = 0
			}
			C.clipallProgressUpdate(C.double(message.Fraction), C.int(indeterminate), encoded, C.int(done), C.int(hold))
			C.free(unsafe.Pointer(encoded))
		}
		if !finished {
			encoded := C.CString("文件传输已中止")
			C.clipallProgressUpdate(0, 0, encoded, 1, 1500)
			C.free(unsafe.Pointer(encoded))
		}
	}()
	C.clipallProgressRun()
	return true, nil
}

type macProgressWindow struct {
	mu     sync.Mutex
	input  io.WriteCloser
	writer *json.Encoder
	closed bool
}

func startMacProgressWindow() (*macProgressWindow, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, macProgressHelperArg)
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		input.Close()
		return nil, err
	}
	go func() { _ = command.Wait() }()
	return &macProgressWindow{input: input, writer: json.NewEncoder(input)}, nil
}

func (window *macProgressWindow) send(message macProgressMessage) {
	if window == nil {
		return
	}
	window.mu.Lock()
	defer window.mu.Unlock()
	if window.closed {
		return
	}
	if err := window.writer.Encode(message); err != nil {
		window.closed = true
		_ = window.input.Close()
	}
}

func (window *macProgressWindow) progress(completed, total int64) {
	fraction := 0.0
	if total > 0 {
		fraction = float64(completed) / float64(total)
	}
	window.send(macProgressMessage{Fraction: fraction, Text: fmt.Sprintf("正在下载文件… %.0f%%", fraction*100)})
}

func (window *macProgressWindow) indeterminate(text string) {
	window.send(macProgressMessage{Indeterminate: true, Text: text})
}

func (window *macProgressWindow) finish(text string, success bool) {
	if window == nil {
		return
	}
	hold := 4000
	if !success {
		hold = 12000
	}
	window.send(macProgressMessage{Fraction: 1, Text: text, Done: true, HoldMS: hold})
	window.mu.Lock()
	if !window.closed {
		window.closed = true
		_ = window.input.Close()
	}
	window.mu.Unlock()
}

func shouldShowMacProgress(offer FileOffer) bool {
	_, _, bytes := fileOfferStats(offer)
	return bytes >= 8<<20 || len(offer.Entries) >= 32
}
