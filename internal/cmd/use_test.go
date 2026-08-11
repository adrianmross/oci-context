package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestUsePrintsEnvironmentReminder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmp := t.TempDir()
	path := tmp + "/config.yml"
	if err := config.Save(path, config.Config{
		Contexts: []config.Context{{Name: "dev", Profile: "DEFAULT", Region: "us-phoenix-1"}},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newUseCmd()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"dev", "--config", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, `eval "$(oci-context export --format env)"`) {
		t.Fatalf("expected environment reminder, got %q", got)
	}
}
