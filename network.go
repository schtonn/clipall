package main

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

var tailscalePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

// resolveListenHost resolves the secure-by-default "tailscale" alias to a
// local Tailscale address. Use "*" only when listening on every interface is
// explicitly desired.
func resolveListenHost(configured string) (string, error) {
	host := strings.TrimSpace(configured)
	switch host {
	case "*":
		return "", nil
	case "":
		return "", fmt.Errorf("listen host is empty; use tailscale, an IP address, or *")
	case "tailscale":
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return "", fmt.Errorf("list network addresses: %w", err)
		}
		for _, prefix := range tailscalePrefixes {
			for _, addr := range addrs {
				ipText := addr.String()
				if slash := strings.IndexByte(ipText, '/'); slash >= 0 {
					ipText = ipText[:slash]
				}
				ip, err := netip.ParseAddr(ipText)
				if err == nil && prefix.Contains(ip) {
					return ip.String(), nil
				}
			}
		}
		return "", fmt.Errorf("no local Tailscale address found; connect Tailscale or use --listen-host with an explicit address")
	default:
		return host, nil
	}
}
