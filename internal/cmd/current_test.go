package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestCurrentOutputs(t *testing.T) {
	cfg := config.Config{
		Options: config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{{
			Name:            "dev",
			Profile:         "DEFAULT",
			TenancyOCID:     "ocid1.tenancy.oc1..aaaa",
			CompartmentOCID: "ocid1.compartment.oc1..bbbb",
			Region:          "us-phoenix-1",
			User:            "ocid1.user.oc1..cccc",
		}},
		CurrentContext: "dev",
	}
	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newCurrentCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"current", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := buf.String(); got != "dev\n" {
		t.Fatalf("want dev, got %q", got)
	}
}

func TestCurrentNoCurrentContext(t *testing.T) {
	cfg := config.Config{}
	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newCurrentCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"current", "--config", cfgPath})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no current context set") {
		t.Fatalf("expected error for missing current context, got %v", err)
	}
}

func TestCurrentContextNotFound(t *testing.T) {
	cfg := config.Config{CurrentContext: "dev"}
	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newCurrentCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"current", "--config", cfgPath})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "context not found") {
		t.Fatalf("expected context not found error, got %v", err)
	}
}
