//go:build !darwin || !cgo

package main

import "context"

func runNodeWithPlatformLoop(ctx context.Context, node *Node, _ bool) error {
	return node.Run(ctx)
}
