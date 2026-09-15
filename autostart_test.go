package main

import (
	"reflect"
	"testing"
)

func TestBuildAutostartArgsDefaults(t *testing.T) {
	if got := buildAutostartArgs("", 9876, "", "", 100); len(got) != 0 {
		t.Fatalf("default args = %q, want none", got)
	}
}

func TestBuildAutostartArgsPreservesRuntimeConfiguration(t *testing.T) {
	got := buildAutostartArgs("mac:9876,windows:9876", 9999, "/tmp/config file.yaml", "/tmp/images", 250)
	want := []string{
		"--peers", "mac:9876,windows:9876",
		"--listen", "9999",
		"--config", "/tmp/config file.yaml",
		"--save-images-to", "/tmp/images",
		"--image-max-size", "250",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}
