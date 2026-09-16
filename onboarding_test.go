package main

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestParseTailscalePeers(t *testing.T) {
	data := []byte(`{
		"Peer": {
			"nodekey:a": {
				"DNSName": "desktop.example.ts.net.",
				"HostName": "desktop",
				"TailscaleIPs": ["100.127.211.29", "fd7a:115c:a1e0::1"],
				"Online": true
			},
			"nodekey:b": {
				"HostName": "macbook",
				"TailscaleIPs": ["100.88.53.98"]
			}
		}
	}`)
	got, err := parseTailscalePeers(data, 9876)
	if err != nil {
		t.Fatal(err)
	}
	want := []discoveredPeer{
		{Address: "desktop.example.ts.net:9876", Name: "desktop", Online: true},
		{Address: "macbook:9876", Name: "macbook", Online: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("peers = %#v, want %#v", got, want)
	}
}

func TestRunOnboardingPromptsAutostartThenPeers(t *testing.T) {
	original := discoverPeers
	t.Cleanup(func() { discoverPeers = original })
	discoverPeers = func(port int) ([]discoveredPeer, error) {
		return []discoveredPeer{{Address: "desktop:9876", Name: "desktop", Online: true}}, nil
	}
	var output bytes.Buffer
	got, err := runOnboarding(strings.NewReader("y\ny\n"), &output, 9876)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Accepted || !got.InstallAutostart || !reflect.DeepEqual(got.PeerAddrs, []string{"desktop:9876"}) {
		t.Fatalf("onboarding result = %#v", got)
	}
	text := output.String()
	startupIndex := strings.Index(text, "Start clipall automatically")
	peersIndex := strings.Index(text, "Use all Tailscale peers")
	if startupIndex < 0 || peersIndex < 0 || startupIndex >= peersIndex {
		t.Fatalf("unexpected prompt order: %q", text)
	}
	if !strings.Contains(text, "--peers host1:9876,host2:9876") {
		t.Fatalf("missing --peers override hint: %q", text)
	}
}

func TestPromptYesNo(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"\n", true},
		{"y\n", true},
		{"YES\n", true},
		{"n\n", false},
	}
	for _, test := range tests {
		reader := bufio.NewReader(strings.NewReader(test.input))
		got, err := promptYesNo(reader, io.Discard, "Continue?")
		if err != nil || got != test.want {
			t.Fatalf("promptYesNo(%q) = %v, %v", test.input, got, err)
		}
	}
}

func TestCompletedOnboardingDoesNotRepeat(t *testing.T) {
	cfg := Config{
		Peers:              []PeerConfig{{Hostname: "desktop", Port: 9876}},
		OnboardingComplete: true,
	}
	if shouldRunOnboarding(cfg, 0, true) {
		t.Fatal("completed onboarding should not run again")
	}
}
