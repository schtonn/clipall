//go:build !windows && !darwin

package main

import (
	"context"

	"golang.design/x/clipboard"
)

func watchImage(ctx context.Context) <-chan []byte {
	return clipboard.Watch(ctx, clipboard.FmtImage)
}
