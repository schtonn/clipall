//go:build !windows && (!darwin || !cgo)

package main

import "context"

func watchFiles(context.Context) <-chan []string {
	return nil
}

func clipboardContainsFiles() bool {
	return false
}

func runFilePasteHandler(ctx context.Context, offers <-chan FileOffer) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-offers:
		}
	}
}
