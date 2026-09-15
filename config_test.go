package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigKeepsSecureListenDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("listen:\n  port: 9999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.Host != "tailscale" || cfg.Listen.Port != 9999 {
		t.Fatalf("listen config = %+v", cfg.Listen)
	}
}
