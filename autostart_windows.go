//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsRunKey  = `Software\Microsoft\Windows\CurrentVersion\Run`
	windowsRunName = "clipall"
)

func windowsCommandLine(executable string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, syscall.EscapeArg(executable))
	for _, arg := range args {
		parts = append(parts, syscall.EscapeArg(arg))
	}
	return strings.Join(parts, " ")
}

func installAutostart(executable string, args []string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Windows Run key: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(windowsRunName, windowsCommandLine(executable, args)); err != nil {
		return fmt.Errorf("set Windows Run value: %w", err)
	}
	return nil
}

func uninstallAutostart() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Windows Run key: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(windowsRunName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("delete Windows Run value: %w", err)
	}
	return nil
}
