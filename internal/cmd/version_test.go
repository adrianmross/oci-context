package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func withVersionMetadata(t *testing.T, v, c, d string) {
	t.Helper()
	origVersion, origCommit, origDate := version, commit, date
	version = v
	commit = c
	date = d
	t.Cleanup(func() {
		version = origVersion
		commit = origCommit
		date = origDate
	})
}

func TestVersionCommandJSON(t *testing.T) {
	withVersionMetadata(t, "v1.2.3", "abc123", "2026-05-18T12:00:00Z")

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version", "-o", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v\n%s", err, out.String())
	}

	var got versionResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal version json: %v\n%s", err, out.String())
	}
	if got.Version != "v1.2.3" || got.Commit != "abc123" || got.Date != "2026-05-18T12:00:00Z" {
		t.Fatalf("unexpected version result: %+v", got)
	}
	if got.Summary != "v1.2.3 commit=abc123 built=2026-05-18T12:00:00Z" {
		t.Fatalf("unexpected summary: %q", got.Summary)
	}
}

func TestVersionCommandTextAndYAML(t *testing.T) {
	withVersionMetadata(t, "v9.0.0", "def456", "2026-05-18T13:00:00Z")

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "text",
			args: []string{"version"},
			want: []string{"v9.0.0 commit=def456 built=2026-05-18T13:00:00Z"},
		},
		{
			name: "yaml",
			args: []string{"version", "-o", "yaml"},
			want: []string{
				"version: v9.0.0",
				"commit: def456",
				"date: \"2026-05-18T13:00:00Z\"",
				"summary: v9.0.0 commit=def456 built=2026-05-18T13:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("version: %v\n%s", err, out.String())
			}
			got := out.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("expected output to contain %q, got %q", want, got)
				}
			}
		})
	}
}
