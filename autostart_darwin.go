//go:build darwin

package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchAgentLabel = "io.github.schtonn.clipall"

func plistEscape(value string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}

func autostartPlist(executable string, args []string, logPath string) []byte {
	var arguments strings.Builder
	for _, arg := range append([]string{executable}, args...) {
		fmt.Fprintf(&arguments, "        <string>%s</string>\n", plistEscape(arg))
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
%s    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>ProcessType</key>
    <string>Background</string>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
</dict>
</plist>
`, launchAgentLabel, arguments.String(), plistEscape(logPath), plistEscape(logPath)))
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
}

func installAutostart(executable string, args []string) error {
	plistPath, err := launchAgentPath()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, "Library", "Logs", "clipall.log")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	if err := os.WriteFile(plistPath, autostartPlist(executable, args, logPath), 0644); err != nil {
		return fmt.Errorf("write LaunchAgent: %w", err)
	}

	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain, plistPath).Run()
	if output, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("load LaunchAgent: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func uninstallAutostart() error {
	plistPath, err := launchAgentPath()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain, plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove LaunchAgent: %w", err)
	}
	return nil
}
