//go:build windows

package main

import (
	"errors"
	"fmt"
	"os/exec"
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

func powershellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// windowsAutostartCommand runs the console binary inside a hidden PowerShell
// window so logging in does not leave a terminal window on the desktop.
func windowsAutostartCommand(executable string, args []string) string {
	parts := make([]string, 0, len(args)+2)
	parts = append(parts, "&", powershellLiteral(executable))
	for _, arg := range args {
		parts = append(parts, powershellLiteral(arg))
	}
	return windowsCommandLine("powershell.exe", []string{
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", strings.Join(parts, " "),
	})
}

func installAutostart(executable string, args []string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Windows Run key: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(windowsRunName, windowsAutostartCommand(executable, args)); err != nil {
		return fmt.Errorf("set Windows Run value: %w", err)
	}
	return nil
}

func startAutostartNow(executable string, args []string) error {
	parts := make([]string, 0, len(args)+2)
	parts = append(parts, "&", powershellLiteral(executable))
	for _, arg := range args {
		parts = append(parts, powershellLiteral(arg))
	}
	command := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", strings.Join(parts, " "))
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start hidden clipall process: %w", err)
	}
	return command.Process.Release()
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
