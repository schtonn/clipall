package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
)

type discoveredPeer struct {
	Address string
	Name    string
	Online  bool
}

type tailscalePeerStatus struct {
	DNSName      string   `json:"DNSName"`
	HostName     string   `json:"HostName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
}

type tailscaleStatus struct {
	Peer map[string]tailscalePeerStatus `json:"Peer"`
}

type onboardingResult struct {
	PeerAddrs        []string
	InstallAutostart bool
	Accepted         bool
}

var discoverPeers = discoverTailscalePeers

// shouldRunOnboarding deliberately does not use the presence of saved peers as
// a proxy for setup completion. Config files created by older versions already
// contain peers but have never offered the interactive autostart setup.
func shouldRunOnboarding(cfg Config, flagCount int, interactive bool) bool {
	return interactive && flagCount == 0 && !cfg.OnboardingComplete
}

func discoverTailscalePeers(port int) ([]discoveredPeer, error) {
	output, err := runTailscaleCLI("status", "--json")
	if err != nil {
		return nil, fmt.Errorf("read Tailscale peers: %w", err)
	}
	return parseTailscalePeers(output, port)
}

func parseTailscalePeers(data []byte, port int) ([]discoveredPeer, error) {
	var status tailscaleStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("parse Tailscale status: %w", err)
	}
	seen := make(map[string]bool)
	peers := make([]discoveredPeer, 0, len(status.Peer))
	for _, peer := range status.Peer {
		host := strings.TrimSuffix(strings.TrimSpace(peer.DNSName), ".")
		if host == "" {
			host = strings.TrimSpace(peer.HostName)
		}
		if host == "" {
			for _, candidate := range peer.TailscaleIPs {
				ip, err := netip.ParseAddr(candidate)
				if err == nil && isTailscaleAddr(ip) {
					host = ip.String()
					if ip.Is4() {
						break
					}
				}
			}
		}
		if host == "" {
			continue
		}
		address := net.JoinHostPort(host, strconv.Itoa(port))
		key := strings.ToLower(address)
		if seen[key] {
			continue
		}
		seen[key] = true
		name := strings.TrimSpace(peer.HostName)
		if name == "" {
			name = host
		}
		peers = append(peers, discoveredPeer{Address: address, Name: name, Online: peer.Online})
	}
	sort.Slice(peers, func(i, j int) bool {
		return strings.ToLower(peers[i].Address) < strings.ToLower(peers[j].Address)
	})
	return peers, nil
}

func stdinIsInteractive() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func promptYesNo(reader *bufio.Reader, writer io.Writer, prompt string) (bool, error) {
	fmt.Fprintf(writer, "%s [Y/n]: ", prompt)
	answer, err := reader.ReadString('\n')
	if err != nil && len(answer) == 0 {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		fmt.Fprintln(writer, "Please answer y or n.")
		return promptYesNo(reader, writer, prompt)
	}
}

func runOnboarding(reader io.Reader, writer io.Writer, listenPort int) (onboardingResult, error) {
	buffered := bufio.NewReader(reader)
	autostart, err := promptYesNo(buffered, writer, "Start clipall automatically at login and run it in the background now?")
	if err != nil {
		return onboardingResult{}, err
	}

	peers, err := discoverPeers(listenPort)
	if err != nil {
		return onboardingResult{}, err
	}
	if len(peers) == 0 {
		return onboardingResult{}, fmt.Errorf("no Tailscale peers found; use --peers host:port to configure them manually")
	}

	fmt.Fprintln(writer, "\nTailscale peers found:")
	for _, peer := range peers {
		state := "offline"
		if peer.Online {
			state = "online"
		}
		fmt.Fprintf(writer, "  - %s (%s, %s)\n", peer.Name, peer.Address, state)
	}
	fmt.Fprintln(writer, "You can override this selection with --peers host1:9876,host2:9876.")
	accepted, err := promptYesNo(buffered, writer, "Use all Tailscale peers listed above?")
	if err != nil {
		return onboardingResult{}, err
	}
	if !accepted {
		return onboardingResult{Accepted: false}, nil
	}

	addresses := make([]string, len(peers))
	for index, peer := range peers {
		addresses[index] = peer.Address
	}
	return onboardingResult{
		PeerAddrs:        addresses,
		InstallAutostart: autostart,
		Accepted:         true,
	}, nil
}

func peerConfigsFromAddrs(addresses []string) ([]PeerConfig, error) {
	peers := make([]PeerConfig, 0, len(addresses))
	for _, address := range addresses {
		host, portText, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid peer address %q: %w", address, err)
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid peer port in %q", address)
		}
		peers = append(peers, PeerConfig{Hostname: host, Port: port})
	}
	return peers, nil
}
