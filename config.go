package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Peers  []PeerConfig `yaml:"peers"`
	Listen ListenConfig `yaml:"listen"`
}

type PeerConfig struct {
	Hostname string `yaml:"hostname"`
	Port     int    `yaml:"port"`
}

type ListenConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

func DefaultConfig() Config {
	return Config{
		Listen: ListenConfig{Host: "tailscale", Port: 9876},
	}
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// DefaultConfigPath returns the platform-specific default config file path.
func DefaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("APPDATA"), "clipall", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clipall", "config.yaml")
}

// PeerAddrs returns peer addresses as "host:port" strings.
func (c Config) PeerAddrs() []string {
	addrs := make([]string, 0, len(c.Peers))
	for _, p := range c.Peers {
		port := p.Port
		if port == 0 {
			port = 9876
		}
		addrs = append(addrs, fmt.Sprintf("%s:%d", p.Hostname, port))
	}
	return addrs
}
