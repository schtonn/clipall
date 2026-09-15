package main

import "strconv"

// buildAutostartArgs keeps the runtime configuration supplied while
// registering autostart. Management-only flags are deliberately excluded.
func buildAutostartArgs(peers string, listen int, configFile, imageDir string, imageMaxMB int) []string {
	var args []string
	if peers != "" {
		args = append(args, "--peers", peers)
	}
	if listen != 9876 {
		args = append(args, "--listen", strconv.Itoa(listen))
	}
	if configFile != "" {
		args = append(args, "--config", configFile)
	}
	if imageDir != "" {
		args = append(args, "--save-images-to", imageDir)
	}
	if imageMaxMB != 100 {
		args = append(args, "--image-max-size", strconv.Itoa(imageMaxMB))
	}
	return args
}
