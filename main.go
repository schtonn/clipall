package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	peers := flag.String("peers", "", "comma-separated peer addresses (host:port)")
	listenHost := flag.String("listen-host", "tailscale", "address to listen on (tailscale, an IP, or * for all interfaces)")
	listen := flag.Int("listen", 9876, "port to listen on")
	configFile := flag.String("config", "", "path to config file (default: auto-detect)")
	imageDir := flag.String("save-images-to", "", "save incoming images to this directory (e.g. /tmp/clipall)")
	imageMaxMB := flag.Int("image-max-size", 100, "max total size of saved images in MB (0 = unlimited)")
	filesEnabled := flag.Bool("files", false, "enable experimental on-demand file copy and paste")
	installAutostartFlag := flag.Bool("install-autostart", false, "start clipall automatically when you log in")
	uninstallAutostartFlag := flag.Bool("uninstall-autostart", false, "remove clipall automatic startup")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	if *listen < 1 || *listen > 65535 {
		fmt.Fprintf(os.Stderr, "error: listen port must be between 1 and 65535 (got %d)\n", *listen)
		os.Exit(1)
	}
	if *imageMaxMB < 0 {
		fmt.Fprintf(os.Stderr, "error: image-max-size must not be negative (got %d)\n", *imageMaxMB)
		os.Exit(1)
	}

	if *showVersion {
		fmt.Printf("clipall %s\n", version)
		os.Exit(0)
	}
	if *installAutostartFlag && *uninstallAutostartFlag {
		fmt.Fprintln(os.Stderr, "error: --install-autostart and --uninstall-autostart cannot be used together")
		os.Exit(1)
	}
	if *installAutostartFlag {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: find executable: %v\n", err)
			os.Exit(1)
		}
		args := buildAutostartArgs(autostartOptions{
			peers:         *peers,
			listenHost:    *listenHost,
			listenHostSet: setFlags["listen-host"],
			listenPort:    *listen,
			listenPortSet: setFlags["listen"],
			configFile:    *configFile,
			imageDir:      *imageDir,
			imageMaxMB:    *imageMaxMB,
			imageMaxSet:   setFlags["image-max-size"],
			filesEnabled:  *filesEnabled,
		})
		if err := installAutostart(executable, args); err != nil {
			fmt.Fprintf(os.Stderr, "error: install autostart: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("clipall autostart installed for %s\n", executable)
		return
	}
	if *uninstallAutostartFlag {
		if err := uninstallAutostart(); err != nil {
			fmt.Fprintf(os.Stderr, "error: uninstall autostart: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("clipall autostart removed")
		return
	}

	cfg := DefaultConfig()

	// Load config file if specified or if default exists.
	cfgPath := *configFile
	if cfgPath == "" {
		cfgPath = DefaultConfigPath()
	}
	if *configFile != "" {
		// Explicitly specified config must exist.
		var err error
		cfg, err = LoadConfig(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else if _, err := os.Stat(cfgPath); err == nil {
		// Default config path exists, load it.
		loaded, err := LoadConfig(cfgPath)
		if err != nil {
			log.Printf("[main] warning: ignoring config %s: %v", cfgPath, err)
		} else {
			cfg = loaded
		}
	}

	// CLI flags override config file.
	if setFlags["listen-host"] {
		cfg.Listen.Host = *listenHost
	}
	if setFlags["listen"] {
		cfg.Listen.Port = *listen
	}
	if cfg.Listen.Port < 1 || cfg.Listen.Port > 65535 {
		fmt.Fprintf(os.Stderr, "error: listen port must be between 1 and 65535 (got %d)\n", cfg.Listen.Port)
		os.Exit(1)
	}

	// Build peer address list.
	var peerAddrs []string
	if *peers != "" {
		for _, p := range strings.Split(*peers, ",") {
			addr := strings.TrimSpace(p)
			if addr != "" {
				peerAddrs = append(peerAddrs, addr)
			}
		}
	} else {
		peerAddrs = cfg.PeerAddrs()
	}

	if len(peerAddrs) == 0 {
		fmt.Fprintln(os.Stderr, "error: no peers configured. Use --peers flag or config file.")
		fmt.Fprintf(os.Stderr, "  example: clipall --peers windows:9876\n")
		fmt.Fprintf(os.Stderr, "  config:  %s\n", DefaultConfigPath())
		os.Exit(1)
	}

	if *imageDir != "" {
		log.Printf("[main] clipall starting, peers: %v, listen: %s:%d, images: %s", peerAddrs, cfg.Listen.Host, cfg.Listen.Port, *imageDir)
	} else {
		log.Printf("[main] clipall starting, peers: %v, listen: %s:%d", peerAddrs, cfg.Listen.Host, cfg.Listen.Port)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	node := NewNodeAt(cfg.Listen.Host, cfg.Listen.Port, peerAddrs, *imageDir, *imageMaxMB)
	node.filesEnabled = *filesEnabled
	if err := node.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	log.Println("[main] clipall stopped")
}
