package cmd

import (
	"os"
	"path/filepath"
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

func TestRenderWakeupScript(t *testing.T) {
	s := renderWakeupScript("/usr/local/bin/oci-context", "com.example.daemon")
	for _, want := range []string{
		"launchctl kickstart -k gui/$(id -u)/com.example.daemon",
		"daemon nudge",
		"/usr/local/bin/oci-context",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected wake script to contain %q", want)
		}
	}
}

func TestRenderSleepwatcherPlist(t *testing.T) {
	plist := renderSleepwatcherPlist("com.example.sleepwatcher", "/opt/homebrew/bin/sleepwatcher", "/Users/me/.wakeup")
	for _, want := range []string{
		"<string>com.example.sleepwatcher</string>",
		"<string>/opt/homebrew/bin/sleepwatcher</string>",
		"<string>-w</string>",
		"<string>/Users/me/.wakeup</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("expected sleepwatcher plist to contain %q", want)
		}
	}
}

func TestRenderWakeupScriptWithHammerspoon(t *testing.T) {
	s := renderWakeupScriptWithHammerspoon("/usr/local/bin/oci-context", "com.example.daemon")
	for _, want := range []string{
		`OCI_CONTEXT_BIN='/usr/local/bin/oci-context'`,
		`DAEMON_LABEL='com.example.daemon'`,
		`hammerspoon://oci-auth-needed`,
		`daemon auth-status`,
		`daemon nudge`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected hammerspoon wake script to contain %q", want)
		}
	}
}

func TestRenderHammerspoonModule(t *testing.T) {
	m := renderHammerspoonModule()
	for _, want := range []string{
		`hs.urlevent.bind("oci-auth-needed"`,
		`session authenticate --profile-name`,
		`--region`,
		`--tenancy-name`,
		`actionButtonTitle = "Re-auth now"`,
		`Authentication command completed for profile`,
		`~/.hammerspoon/oci-auth.log`,
	} {
		if !strings.Contains(m, want) {
			t.Fatalf("expected hammerspoon module to contain %q", want)
		}
	}
}

func TestEnsureHammerspoonInitLoadsModule_NewFile(t *testing.T) {
	dir := t.TempDir()
	initPath := filepath.Join(dir, "init.lua")
	if err := ensureHammerspoonInitLoadsModule(initPath); err != nil {
		t.Fatalf("ensure init load: %v", err)
	}
	b, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read init: %v", err)
	}
	if !strings.Contains(string(b), `pcall(require, "oci_context")`) {
		t.Fatalf("expected init to include require snippet, got %q", string(b))
	}
	if !strings.Contains(string(b), `Hammerspoon load error`) {
		t.Fatalf("expected init to include load-failure message, got %q", string(b))
	}
}

func TestEnsureHammerspoonInitLoadsModule_ExistingFileIdempotent(t *testing.T) {
	dir := t.TempDir()
	initPath := filepath.Join(dir, "init.lua")
	orig := "-- existing config\n"
	if err := os.WriteFile(initPath, []byte(orig), 0o644); err != nil {
		t.Fatalf("write init: %v", err)
	}
	if err := ensureHammerspoonInitLoadsModule(initPath); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := ensureHammerspoonInitLoadsModule(initPath); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	b, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read init: %v", err)
	}
	content := string(b)
	if strings.Count(content, `pcall(require, "oci_context")`) != 1 {
		t.Fatalf("expected require snippet once, got %q", content)
	}
	if !strings.Contains(content, orig) {
		t.Fatalf("expected existing content preserved, got %q", content)
	}
}

func TestBuildHammerspoonAuthNeededURL(t *testing.T) {
	got := buildHammerspoonAuthNeededURL("OPS", "dev", "us-chicago-1", "wake auth failed", "oabcs1")
	for _, want := range []string{
		"hammerspoon://oci-auth-needed?",
		"profile=OPS",
		"context=dev",
		"region=us-chicago-1",
		"reason=wake+auth+failed",
		"tenancy_name=oabcs1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in URL %q", want, got)
		}
	}
}
