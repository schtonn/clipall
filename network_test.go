package main

import "testing"

func TestResolveListenHostExplicit(t *testing.T) {
	got, err := resolveListenHost("127.0.0.1")
	if err != nil || got != "127.0.0.1" {
		t.Fatalf("resolveListenHost = %q, %v", got, err)
	}
}

func TestResolveListenHostAllInterfacesRequiresStar(t *testing.T) {
	got, err := resolveListenHost("*")
	if err != nil || got != "" {
		t.Fatalf("resolveListenHost(*) = %q, %v", got, err)
	}
	if _, err := resolveListenHost(""); err == nil {
		t.Fatal("empty listen host should be rejected")
	}
}
