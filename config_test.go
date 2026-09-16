package main

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestSaveConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	want := Config{
		Peers:              []PeerConfig{{Hostname: "desktop.example.ts.net", Port: 9876}},
		Listen:             ListenConfig{Host: "tailscale", Port: 9876},
		OnboardingComplete: true,
	}
	if err := SaveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Peers) != 1 || got.Peers[0] != want.Peers[0] || got.Listen != want.Listen || !got.OnboardingComplete {
		t.Fatalf("config = %+v, want %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("config permissions = %o, want private", info.Mode().Perm())
	}
}

func TestLegacyConfigRequiresOnboarding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("peers:\n  - hostname: desktop\n    port: 9876\nlisten:\n  host: tailscale\n  port: 9876\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldRunOnboarding(cfg, 0, true) {
		t.Fatal("legacy config with saved peers should still run onboarding")
	}
	if shouldRunOnboarding(cfg, 1, true) {
		t.Fatal("explicit flags should suppress onboarding")
	}
	if shouldRunOnboarding(cfg, 0, false) {
		t.Fatal("background invocation should suppress onboarding")
	}
}
