package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestCreateInheritsProfileAndUsesOverrides(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	ociConfigPath := filepath.Join(tmp, "oci-config")
	if err := os.WriteFile(ociConfigPath, []byte("[DEFAULT]\nuser=ocid1.user.oc1..user\ntenancy=ocid1.tenancy.oc1..tenancy\nregion=us-phoenix-1\n"), 0o600); err != nil {
		t.Fatalf("write OCI config: %v", err)
	}
	cfgPath := filepath.Join(tmp, "config.yml")
	if err := config.Save(cfgPath, config.Config{Options: config.Options{OCIConfigPath: ociConfigPath}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newRootCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"create", "context", "new-region",
		"--config", cfgPath,
		"--region", "us-ashburn-1",
		"--compartment", "ocid1.compartment.oc1..compartment",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ctx, err := got.GetContext("new-region")
	if err != nil {
		t.Fatalf("get context: %v", err)
	}
	if ctx.Profile != "DEFAULT" || ctx.AuthMethod != config.AuthMethodAPIKey || ctx.TenancyOCID != "ocid1.tenancy.oc1..tenancy" || ctx.Region != "us-ashburn-1" || ctx.CompartmentOCID != "ocid1.compartment.oc1..compartment" || ctx.User != "ocid1.user.oc1..user" {
		t.Fatalf("unexpected context: %+v", ctx)
	}
	if !strings.Contains(out.String(), "Created/updated context new-region") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestCreateMonitorPreservesAndDeduplicates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	path := filepath.Join(tmp, "config.yml")
	cfg := config.Config{Options: config.Options{DaemonContexts: []string{"existing"}}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		cmd := newRootCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetArgs([]string{"create", "session", "--config", path, "--profile", "session", "--tenancy", "ocid1.tenancy.oc1..example", "--user", "ocid1.user.oc1..example", "--region", "us-phoenix-1", "--auth-method", "security_token", "--monitor"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Options.DaemonContexts, ",") != "existing,session" {
		t.Fatalf("monitor list: %v", got.Options.DaemonContexts)
	}
	ctx, err := got.GetContext("session")
	if err != nil || ctx.AuthMethod != config.AuthMethodSecurityToken {
		t.Fatalf("context: %+v, %v", ctx, err)
	}
}

func TestEditAndDeleteContextForms(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	if err := config.Save(cfgPath, config.Config{Contexts: []config.Context{{Name: "dev", Profile: "DEFAULT", Region: "us-phoenix-1"}}}); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"edit", "context", "dev", "--notes", "updated", "--config", cfgPath},
		{"delete", "context", "dev", "--config", cfgPath},
	} {
		cmd := newRootCmd()
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}

	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Contexts) != 0 {
		t.Fatalf("expected context deletion, got %+v", got.Contexts)
	}
}
