package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestPathsCommandJSONProjectConfig(t *testing.T) {
	withTempWd(t, func(tmp string) {
		cfgPath := filepath.Join(tmp, ".oci-context.yml")
		cfg := config.DefaultConfig(tmp)
		cfg.Options.OCIConfigPath = filepath.Join(tmp, ".oci", "config")
		cfg.Options.SocketPath = filepath.Join(tmp, ".oci-context", "daemon.sock")
		if err := config.Save(cfgPath, cfg); err != nil {
			t.Fatalf("save config: %v", err)
		}
		touch(t, filepath.Join(tmp, "oci-context.yml"))

		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"paths", "-o", "json"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("paths: %v\n%s", err, out.String())
		}

		var got pathsResult
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal paths json: %v\n%s", err, out.String())
		}
		if !pathsEqual(got.ConfigPath, cfgPath) {
			t.Fatalf("expected config path %s, got %s", cfgPath, got.ConfigPath)
		}
		if got.ConfigSource != "project" {
			t.Fatalf("expected project source, got %q", got.ConfigSource)
		}
		if !pathsEqual(got.WorkingDirectory, tmp) {
			t.Fatalf("expected working directory %s, got %s", tmp, got.WorkingDirectory)
		}
		if got.OCIConfigPath != cfg.Options.OCIConfigPath || got.SocketPath != cfg.Options.SocketPath {
			t.Fatalf("expected loaded option paths, got %+v", got)
		}
		if len(got.ProjectCandidates) != len(configCandidateRelPaths()) {
			t.Fatalf("expected all project candidates, got %d", len(got.ProjectCandidates))
		}
		if got.ProjectCandidates[0].RelativePath != ".oci-context.yml" || !got.ProjectCandidates[0].Exists || !got.ProjectCandidates[0].IsFile {
			t.Fatalf("expected first candidate to be existing .oci-context.yml, got %+v", got.ProjectCandidates[0])
		}
	})
}

func TestPathsCommandGlobalAndMissingConfig(t *testing.T) {
	withTempWd(t, func(tmp string) {
		missing := filepath.Join(tmp, "missing.yml")

		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"paths", "--config", missing})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("paths should explain missing config without failing: %v\n%s", err, out.String())
		}

		got := out.String()
		for _, want := range []string{
			"config_path: " + missing,
			"config_source: explicit",
			"config_error:",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("expected output to contain %q, got %q", want, got)
			}
		}
	})
}

func TestResolveConfigPathInfoSources(t *testing.T) {
	withTempWd(t, func(tmp string) {
		touch(t, filepath.Join(tmp, ".oci-context", "config.json"))

		got, err := resolveConfigPathInfo("", false)
		if err != nil {
			t.Fatalf("resolve project: %v", err)
		}
		want := filepath.Join(tmp, ".oci-context", "config.json")
		if got.Source != "project" || !pathsEqual(got.Path, want) {
			t.Fatalf("expected project path %s, got %+v", want, got)
		}

		got, err = resolveConfigPathInfo("", true)
		if err != nil {
			t.Fatalf("resolve global: %v", err)
		}
		if got.Source != "global_flag" || got.Path != got.GlobalPath {
			t.Fatalf("expected global flag resolution, got %+v", got)
		}

		got, err = resolveConfigPathInfo("/tmp/explicit.yml", false)
		if err != nil {
			t.Fatalf("resolve explicit: %v", err)
		}
		if got.Source != "explicit" || got.Path != "/tmp/explicit.yml" {
			t.Fatalf("expected explicit resolution, got %+v", got)
		}
	})
}
