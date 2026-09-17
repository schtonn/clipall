//go:build darwin && cgo

package main

/*
*/
import "C"

import (
	"errors"
	"log"
)

//export clipallRemoteFilesRequested
func clipallRemoteFilesRequested(offerID *C.char) {
	if offerID == nil {
		return
	}
	err := macRemoteFiles.requestDownload(C.GoString(offerID))
	if err != nil {
		if !errors.Is(err, errMacFileDownloadPending) {
			log.Printf("[files] on-demand Finder request failed: %v", err)
		}
	}
}
