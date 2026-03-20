package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestRenderLaunchdPlistIncludesArgs(t *testing.T) {
	plist := renderLaunchdPlist(
		"com.example.oci-context.daemon",
		"/usr/local/bin/oci-context",
		"/Users/me/.oci-context/config.yml",
		true,
		5*time.Minute,
		15*time.Minute,
		"/Users/me/.oci-context/daemon.out.log",
		"/Users/me/.oci-context/daemon.err.log",
	)
	for _, want := range []string{
		"<string>com.example.oci-context.daemon</string>",
		"<string>/usr/local/bin/oci-context</string>",
		"<string>daemon</string>",
		"<string>serve</string>",
		"<string>--auto-refresh</string>",
		"<string>5m0s</string>",
		"<string>15m0s</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("expected plist to contain %q", want)
		}
	}
}
