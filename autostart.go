package main

import "strconv"

type autostartOptions struct {
	peers         string
	listenHost    string
	listenHostSet bool
	listenPort    int
	listenPortSet bool
	configFile    string
	imageDir      string
	imageMaxMB    int
	imageMaxSet   bool
	filesEnabled  bool
	filesSet      bool
}

// buildAutostartArgs keeps the runtime configuration supplied while
// registering autostart. Management-only flags are deliberately excluded.
func buildAutostartArgs(opts autostartOptions) []string {
	var args []string
	if opts.peers != "" {
		args = append(args, "--peers", opts.peers)
	}
	if opts.listenHostSet {
		args = append(args, "--listen-host", opts.listenHost)
	}
	if opts.listenPortSet {
		args = append(args, "--listen", strconv.Itoa(opts.listenPort))
	}
	if opts.configFile != "" {
		args = append(args, "--config", opts.configFile)
	}
	if opts.imageDir != "" {
		args = append(args, "--save-images-to", opts.imageDir)
	}
	if opts.imageMaxSet {
		args = append(args, "--image-max-size", strconv.Itoa(opts.imageMaxMB))
	}
	if opts.filesEnabled {
		args = append(args, "--files")
	} else if opts.filesSet {
		args = append(args, "--files=false")
	}
	return args
}
