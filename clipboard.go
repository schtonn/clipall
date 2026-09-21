//go:build !darwin

package main

import (
	"context"
	"fmt"

	"golang.design/x/clipboard"
)

func initClipboard() error {
	return clipboard.Init()
}

func watchText(ctx context.Context) <-chan []byte {
	return clipboard.Watch(ctx, clipboard.FmtText)
}

func writeText(data []byte) error {
	if done := clipboard.Write(clipboard.FmtText, data); done == nil {
		return fmt.Errorf("clipboard rejected text write")
	}
	return nil
}

func readText() []byte {
	return clipboard.Read(clipboard.FmtText)
}

func writeImage(data []byte) error {
	if done := clipboard.Write(clipboard.FmtImage, data); done == nil {
		return fmt.Errorf("clipboard rejected image write")
	}
	return nil
}
