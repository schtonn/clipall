//go:build !darwin || !cgo

package main

func runInternalPlatformMode([]string) (bool, error) {
	return false, nil
}
