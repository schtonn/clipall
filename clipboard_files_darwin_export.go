//go:build darwin && cgo

package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"errors"
	"log"
)

//export clipallProvideRemoteFiles
func clipallProvideRemoteFiles(offerID *C.char) *C.char {
	if offerID == nil {
		return nil
	}
	paths, err := macRemoteFiles.resolve(C.GoString(offerID))
	if err != nil {
		if !errors.Is(err, errMacFileDownloadPending) {
			log.Printf("[files] on-demand Finder request failed: %v", err)
		}
		return nil
	}
	encoded, err := json.Marshal(paths)
	if err != nil {
		return nil
	}
	return C.CString(string(encoded))
}
