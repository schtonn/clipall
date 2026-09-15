package main

import (
	"reflect"
	"testing"
)

func TestBuildAutostartArgsDefaults(t *testing.T) {
	if got := buildAutostartArgs(autostartOptions{}); len(got) != 0 {
		t.Fatalf("default args = %q, want none", got)
	}
}

func TestBuildAutostartArgsPreservesRuntimeConfiguration(t *testing.T) {
	got := buildAutostartArgs(autostartOptions{
		peers:         "mac:9876,windows:9876",
		listenHost:    "127.0.0.1",
		listenHostSet: true,
		listenPort:    9999,
		listenPortSet: true,
		configFile:    "/tmp/config file.yaml",
		imageDir:      "/tmp/images",
		imageMaxMB:    250,
		imageMaxSet:   true,
	})
	want := []string{
		"--peers", "mac:9876,windows:9876",
		"--listen-host", "127.0.0.1",
		"--listen", "9999",
		"--config", "/tmp/config file.yaml",
		"--save-images-to", "/tmp/images",
		"--image-max-size", "250",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestBuildAutostartArgsPreservesExplicitDefaults(t *testing.T) {
	got := buildAutostartArgs(autostartOptions{
		listenHost:    "tailscale",
		listenHostSet: true,
		listenPort:    9876,
		listenPortSet: true,
		imageMaxMB:    100,
		imageMaxSet:   true,
	})
	want := []string{"--listen-host", "tailscale", "--listen", "9876", "--image-max-size", "100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}
