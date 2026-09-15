//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestWindowsCommandLineQuotesPathsAndArguments(t *testing.T) {
	got := windowsCommandLine(`C:\Program Files\clipall\clipall.exe`, []string{"--config", `C:\My Config\config.yaml`})
	want := `"C:\Program Files\clipall\clipall.exe" --config "C:\My Config\config.yaml"`
	if got != want {
		t.Fatalf("command line = %q, want %q", got, want)
	}
}

func TestWindowsAutostartCommandUsesHiddenPowerShell(t *testing.T) {
	got := windowsAutostartCommand(`C:\Program Files\clipall\clipall.exe`, []string{"--peers", "mac:9876"})
	for _, want := range []string{"powershell.exe", "-WindowStyle", "Hidden", `C:\Program Files\clipall\clipall.exe`, "mac:9876"} {
		if !strings.Contains(got, want) {
			t.Fatalf("autostart command %q does not contain %q", got, want)
		}
	}
}
