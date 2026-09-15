//go:build windows

package main

import "testing"

func TestWindowsCommandLineQuotesPathsAndArguments(t *testing.T) {
	got := windowsCommandLine(`C:\Program Files\clipall\clipall.exe`, []string{"--config", `C:\My Config\config.yaml`})
	want := `"C:\Program Files\clipall\clipall.exe" --config "C:\My Config\config.yaml"`
	if got != want {
		t.Fatalf("command line = %q, want %q", got, want)
	}
}
