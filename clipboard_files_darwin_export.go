//go:build darwin && cgo

package main

/*
*/
import "C"

import (
	"errors"
	"log"
	"time"
)

//export clipallRemoteFilesRequested
func clipallRemoteFilesRequested(offerID *C.char, requestedAtMillis C.longlong) {
	if offerID == nil {
		return
	}
	id := C.GoString(offerID)
	delay := time.Now().UnixMilli() - int64(requestedAtMillis)
	log.Printf("[files-debug] pasteboard provider dispatch reached Go: offer=%s, delay_ms=%d", shortMacOfferID(id), delay)
	err := macRemoteFiles.requestDownload(id)
	if err != nil {
		if !errors.Is(err, errMacFileDownloadPending) {
			log.Printf("[files] on-demand Finder request failed: %v", err)
		}
	}
}
