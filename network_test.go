package main

import (
	"net"
	"net/netip"
	"strings"
	"testing"
)

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

func TestResolveListenHostUsesTailscaleCLI(t *testing.T) {
	originalLookup := lookupTailscaleIPs
	originalList := listInterfaceAddrs
	t.Cleanup(func() {
		lookupTailscaleIPs = originalLookup
		listInterfaceAddrs = originalList
	})
	lookupTailscaleIPs = func() ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("100.88.53.98")}, nil
	}
	listInterfaceAddrs = func() ([]net.Addr, error) {
		t.Fatal("interface fallback should not run when the CLI succeeds")
		return nil, nil
	}

	got, err := resolveListenHost("tailscale")
	if err != nil || got != "100.88.53.98" {
		t.Fatalf("resolveListenHost(tailscale) = %q, %v", got, err)
	}
}

func TestResolveListenHostRejectsAmbiguousFallback(t *testing.T) {
	originalLookup := lookupTailscaleIPs
	originalList := listInterfaceAddrs
	t.Cleanup(func() {
		lookupTailscaleIPs = originalLookup
		listInterfaceAddrs = originalList
	})
	lookupTailscaleIPs = func() ([]netip.Addr, error) {
		return nil, net.ErrClosed
	}
	listInterfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("100.78.30.88"), Mask: net.CIDRMask(10, 32)},
			&net.IPNet{IP: net.ParseIP("100.88.53.98"), Mask: net.CIDRMask(10, 32)},
		}, nil
	}

	_, err := resolveListenHost("tailscale")
	if err == nil || !strings.Contains(err.Error(), "multiple Tailscale-range addresses") {
		t.Fatalf("resolveListenHost(tailscale) error = %v", err)
	}
}

func TestParseTailscaleIPs(t *testing.T) {
	got := parseTailscaleIPs("100.88.53.98\nnot-an-ip\n192.168.1.2\n100.88.53.98\n")
	if len(got) != 1 || got[0].String() != "100.88.53.98" {
		t.Fatalf("parseTailscaleIPs = %v", got)
	}
}
