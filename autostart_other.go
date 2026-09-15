//go:build !darwin && !windows

package main

import "fmt"

func installAutostart(_ string, _ []string) error {
	return fmt.Errorf("autostart is supported only on macOS and Windows")
}

func uninstallAutostart() error {
	return fmt.Errorf("autostart is supported only on macOS and Windows")
}
