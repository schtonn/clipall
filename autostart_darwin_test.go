//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestAutostartPlistEscapesArguments(t *testing.T) {
	plist := string(autostartPlist("/Applications/Clip & All/clipall", []string{"--peers", "mac<pc>:9876"}, "/tmp/clipall.log"))
	for _, want := range []string{
		"<string>/Applications/Clip &amp; All/clipall</string>",
		"<string>mac&lt;pc&gt;:9876</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist does not contain %q:\n%s", want, plist)
		}
	}
}
