//go:build !darwin && !windows

package main

import "golang.design/x/clipboard"

func readImage() []byte {
	return clipboard.Read(clipboard.FmtImage)
}
