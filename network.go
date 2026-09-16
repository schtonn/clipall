package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var tailscalePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

var (
	lookupTailscaleIPs = tailscaleIPsFromCLI
	listInterfaceAddrs = net.InterfaceAddrs
)

func isTailscaleAddr(ip netip.Addr) bool {
	for _, prefix := range tailscalePrefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
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
		// Do not identify Tailscale solely by its CGNAT range. Proxy TUNs and
		// other VPNs can also assign addresses from 100.64.0.0/10. Ask
		// Tailscale first so we bind the interface that peers actually reach.
		if ips, err := lookupTailscaleIPs(); err == nil && len(ips) > 0 {
			return ips[0].String(), nil
		}

		addrs, err := listInterfaceAddrs()
		if err != nil {
			return "", fmt.Errorf("list network addresses: %w", err)
		}
		var candidates []netip.Addr
		for _, prefix := range tailscalePrefixes {
			for _, addr := range addrs {
				ipText := addr.String()
				if slash := strings.IndexByte(ipText, '/'); slash >= 0 {
					ipText = ipText[:slash]
				}
				ip, err := netip.ParseAddr(ipText)
				if err == nil && prefix.Contains(ip) && !containsAddr(candidates, ip) {
					candidates = append(candidates, ip)
				}
			}
		}
		if len(candidates) == 1 {
			return candidates[0].String(), nil
		}
		if len(candidates) > 1 {
			return "", fmt.Errorf("multiple Tailscale-range addresses found (%s); cannot safely identify the Tailscale interface",
				joinAddrs(candidates))
		}
		return "", fmt.Errorf("no local Tailscale address found; connect Tailscale or use --listen-host with an explicit address")
	default:
		return host, nil
	}
}

func tailscaleIPsFromCLI() ([]netip.Addr, error) {
	output, err := runTailscaleCLI("ip", "-4")
	if err != nil {
		return nil, err
	}
	ips := parseTailscaleIPs(string(output))
	if len(ips) == 0 {
		return nil, fmt.Errorf("tailscale returned no IPv4 address")
	}
	return ips, nil
}

func tailscaleCommandCandidates() []string {
	var candidates []string
	if path, err := exec.LookPath("tailscale"); err == nil {
		candidates = append(candidates, path)
	}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/usr/local/bin/tailscale",
			"/opt/homebrew/bin/tailscale",
			"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		)
	case "windows":
		if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
			candidates = append(candidates, filepath.Join(programFiles, "Tailscale", "tailscale.exe"))
		}
	}
	return candidates
}

func runTailscaleCLI(args ...string) ([]byte, error) {
	var lastErr error
	seen := make(map[string]bool)
	for _, command := range tailscaleCommandCandidates() {
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		output, err := exec.CommandContext(ctx, command, args...).Output()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		return output, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("tailscale CLI not found")
	}
	return nil, lastErr
}

func parseTailscaleIPs(output string) []netip.Addr {
	var ips []netip.Addr
	for _, field := range strings.Fields(output) {
		ip, err := netip.ParseAddr(field)
		if err == nil && isTailscaleAddr(ip) && !containsAddr(ips, ip) {
			ips = append(ips, ip)
		}
	}
	return ips
}

func containsAddr(addrs []netip.Addr, target netip.Addr) bool {
	for _, addr := range addrs {
		if addr == target {
			return true
		}
	}
	return false
}

func joinAddrs(addrs []netip.Addr) string {
	values := make([]string, len(addrs))
	for i, addr := range addrs {
		values[i] = addr.String()
	}
	return strings.Join(values, ", ")
}
