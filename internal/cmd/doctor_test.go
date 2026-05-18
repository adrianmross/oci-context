package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	daemonpkg "github.com/adrianmross/oci-context/internal/daemon"
	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func TestDoctorJSONUsesBestEffortProbes(t *testing.T) {
	origCapture := runOCICaptureForAuth
	origRun := runOCIForAuth
	origLookPath := lookPathForDoctor
	origRunDoctor := runDoctorCommandOutput
	origFetchDaemon := fetchDaemonAuthStatusForDoctor
	defer func() {
		runOCICaptureForAuth = origCapture
		runOCIForAuth = origRun
		lookPathForDoctor = origLookPath
		runDoctorCommandOutput = origRunDoctor
		fetchDaemonAuthStatusForDoctor = origFetchDaemon
	}()

	tmp := t.TempDir()
	ociConfigPath := tmp + "/oci-config"
	if err := config.Save(tmp+"/config.yml", config.Config{
		Options: config.Options{
			OCIConfigPath: ociConfigPath,
			SocketPath:    tmp + "/daemon.sock",
		},
		Contexts: []config.Context{{
			Name:        "dev",
			Profile:     "DEFAULT",
			AuthMethod:  config.AuthMethodSecurityToken,
			TenancyOCID: "ocid1.tenancy.oc1..aaaa",
			Region:      "us-ashburn-1",
		}},
		CurrentContext: "dev",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := os.WriteFile(ociConfigPath, []byte("[DEFAULT]\n"), 0o600); err != nil {
		t.Fatalf("write oci config: %v", err)
	}

	runOCICaptureForAuth = func(_ *cobra.Command, _ []string) ([]byte, error) {
		return []byte(`{"data":[{"is-home-region":true,"region-key":"IAD","region-name":"us-ashburn-1","status":"READY"}]}`), nil
	}
	runOCIForAuth = func(_ *cobra.Command, args []string) error {
		t.Fatalf("expected no refresh/login when validation succeeds, got %v", args)
		return nil
	}
	lookPathForDoctor = func(file string) (string, error) {
		if file != "oci" {
			t.Fatalf("expected oci lookup, got %s", file)
		}
		return "/usr/local/bin/oci", nil
	}
	runDoctorCommandOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "/usr/local/bin/oci" || strings.Join(args, " ") != "--version" {
			t.Fatalf("unexpected doctor command: %s %v", name, args)
		}
		return []byte("3.55.0\n"), nil
	}
	fetchDaemonAuthStatusForDoctor = func(_ config.Config, contextName string) (daemonpkg.AuthStatus, error) {
		return daemonpkg.AuthStatus{ContextName: contextName, AuthMethod: config.AuthMethodSecurityToken, Mode: "auto"}, nil
	}

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--config", tmp + "/config.yml", "--output", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out.String())
	}

	var got doctorResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal doctor json: %v\n%s", err, out.String())
	}
	if got.ConfigPath != tmp+"/config.yml" || got.CurrentContext != "dev" {
		t.Fatalf("unexpected config context fields: %+v", got)
	}
	if !got.OCIConfig.Exists || !got.OCIConfig.IsFile {
		t.Fatalf("expected OCI config presence, got %+v", got.OCIConfig)
	}
	if !got.OCICLI.Available || got.OCICLI.Version != "3.55.0" {
		t.Fatalf("expected OCI CLI version, got %+v", got.OCICLI)
	}
	if !got.Daemon.Available || got.Daemon.Status == nil || got.Daemon.Status.ContextName != "dev" {
		t.Fatalf("expected daemon status, got %+v", got.Daemon)
	}
	if got.AuthEnsure.State != authEnsureStateReady || !got.AuthEnsure.OK {
		t.Fatalf("expected ready auth ensure, got %+v", got.AuthEnsure)
	}
}

func TestDoctorTextIncludesBestEffortFailures(t *testing.T) {
	origCapture := runOCICaptureForAuth
	origRun := runOCIForAuth
	origLookPath := lookPathForDoctor
	origFetchDaemon := fetchDaemonAuthStatusForDoctor
	defer func() {
		runOCICaptureForAuth = origCapture
		runOCIForAuth = origRun
		lookPathForDoctor = origLookPath
		fetchDaemonAuthStatusForDoctor = origFetchDaemon
	}()

	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	cfg := config.Config{
		Options: config.Options{
			OCIConfigPath: tmp + "/missing-oci-config",
			SocketPath:    tmp + "/missing.sock",
		},
		Contexts: []config.Context{{
			Name:        "dev",
			Profile:     "DEFAULT",
			AuthMethod:  config.AuthMethodSecurityToken,
			TenancyOCID: "ocid1.tenancy.oc1..aaaa",
			Region:      "us-phoenix-1",
		}},
		CurrentContext: "dev",
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	runOCICaptureForAuth = func(_ *cobra.Command, _ []string) ([]byte, error) {
		return nil, fmt.Errorf("expired")
	}
	runOCIForAuth = func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf("refresh failed")
	}
	lookPathForDoctor = func(file string) (string, error) {
		return "", fmt.Errorf("%s not found", file)
	}
	fetchDaemonAuthStatusForDoctor = func(_ config.Config, _ string) (daemonpkg.AuthStatus, error) {
		return daemonpkg.AuthStatus{}, fmt.Errorf("socket missing")
	}

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor should report best-effort failures without failing: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"oci_cli: unavailable",
		"daemon: unavailable",
		"auth_ensure: state=login_required",
		"login_command: oci-context auth login --context dev",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected text output to contain %q, got %s", want, got)
		}
	}
}
