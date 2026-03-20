package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootVersionFlagLong(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	version = "v1.2.3-test"
	commit = "abc1234"
	date = "2026-03-20T10:11:12Z"
	t.Cleanup(func() {
		version = origVersion
		commit = origCommit
		date = origDate
	})

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := strings.TrimSpace(out.String())
	want := "v1.2.3-test commit=abc1234 built=2026-03-20T10:11:12Z"
	if got != want {
		t.Fatalf("expected version output %q, got %q", want, got)
	}
}

func TestRootVersionFlagShort(t *testing.T) {
	origVersion, origCommit, origDate := version, commit, date
	version = "v9.9.9-short"
	commit = "none"
	date = "unknown"
	t.Cleanup(func() {
		version = origVersion
		commit = origCommit
		date = origDate
	})

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-v"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "v9.9.9-short" {
		t.Fatalf("expected version output, got %q", got)
	}
}

func TestRootNoArgsShowsHelp(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Manage OCI contexts") {
		t.Fatalf("expected help output, got %q", got)
	}
}
