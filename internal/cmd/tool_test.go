package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestToolSetupOChainJSONUsesCurrentService(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	cfg.CurrentService = "obpcs-testnet"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--config", cfgPath, "tool", "setup", "ochain", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	var payload toolSetupPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v\n%s", err, out.String())
	}
	if payload.SchemaVersion != "oci-context.tool-setup.v1" {
		t.Fatalf("unexpected schema: %s", payload.SchemaVersion)
	}
	if payload.Service != "obpcs-testnet" {
		t.Fatalf("expected current service, got %q", payload.Service)
	}
	if payload.TokenCommand != "oci-context auth token --no-login --format raw" {
		t.Fatalf("expected current-service token command, got %q", payload.TokenCommand)
	}
	if got := payload.AuthProfiles["oci-context-obp"].TokenCommand; got != payload.TokenCommand {
		t.Fatalf("expected auth profile token command %q, got %q", payload.TokenCommand, got)
	}
	if len(payload.Environment) != 1 || payload.Environment[0].Name != "OCHAIN_TOKEN_COMMAND" {
		t.Fatalf("unexpected environment payload: %+v", payload.Environment)
	}
}

func TestToolSetupOChainShellWithExplicitService(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	cfg.CurrentService = "obpcs-testnet"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--config", cfgPath, "tool", "setup", "ochain", "--shell", "--service", "obp"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	got := strings.TrimSpace(out.String())
	want := `export OCHAIN_TOKEN_COMMAND="oci-context auth token --service obp --no-login --format raw"`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
