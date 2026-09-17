//go:build darwin && !cgo

package main

import (
	"context"
	"fmt"
)

func initClipboard() error { return fmt.Errorf("macOS clipboard requires cgo") }

func closedClipboardChannel() <-chan []byte {
	channel := make(chan []byte)
	close(channel)
	return channel
}

func watchText(context.Context) <-chan []byte  { return closedClipboardChannel() }
func watchImage(context.Context) <-chan []byte { return closedClipboardChannel() }
func writeText([]byte) error                   { return fmt.Errorf("macOS clipboard requires cgo") }
func readText() []byte                         { return nil }
func writeImage([]byte) error                  { return fmt.Errorf("macOS clipboard requires cgo") }
func readImage() []byte                        { return nil }
