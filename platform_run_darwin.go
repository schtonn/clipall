//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fno-objc-arc -fblocks
#cgo LDFLAGS: -framework AppKit -framework Foundation

#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#include <dispatch/dispatch.h>

static void clipallApplicationPrepare(void) {
	@autoreleasepool {
		NSApplication *application = [NSApplication sharedApplication];
		[application setActivationPolicy:NSApplicationActivationPolicyAccessory];
	}
}

static void clipallApplicationStop(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp stop:nil];
		NSEvent *event = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
		                              location:NSZeroPoint modifierFlags:0 timestamp:0
		                           windowNumber:0 context:nil subtype:0 data1:0 data2:0];
		[NSApp postEvent:event atStart:NO];
	});
}

static void clipallApplicationRun(void) {
	@autoreleasepool {
		[NSApp run];
	}
}
*/
import "C"

import (
	"context"
	"log"
	"runtime"
)

// AppKit requires its application event loop on the initial macOS thread.
func init() {
	runtime.LockOSThread()
}

func runNodeWithPlatformLoop(ctx context.Context, node *Node, filesEnabled bool) error {
	if !filesEnabled {
		return node.Run(ctx)
	}

	C.clipallApplicationPrepare()
	nodeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = node.Run(nodeCtx)
		close(done)
	}()
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		C.clipallApplicationStop()
	}()

	log.Printf("[files-debug] hidden AppKit main loop started")
	C.clipallApplicationRun()
	cancel()
	<-done
	log.Printf("[files-debug] hidden AppKit main loop stopped")
	return runErr
}
