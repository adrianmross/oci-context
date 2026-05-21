package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func TestAuthCapabilityForMethod(t *testing.T) {
	sec := authCapabilityForMethod(config.AuthMethodSecurityToken)
	if !sec.CanLogin || !sec.CanRefresh || !sec.CanValidate || !sec.CanSetup {
		t.Fatalf("expected full security_token capabilities, got %+v", sec)
	}

	api := authCapabilityForMethod(config.AuthMethodAPIKey)
	if api.CanRefresh || api.CanLogin || !api.CanSetup || !api.CanValidate {
		t.Fatalf("unexpected api_key capabilities: %+v", api)
	}

	rp := authCapabilityForMethod(config.AuthMethodResourcePrincipal)
	if rp.CanLogin || rp.CanRefresh || rp.CanSetup || !rp.CanValidate {
		t.Fatalf("unexpected resource_principal capabilities: %+v", rp)
	}
}

func TestAuthMethodsCommandOutput(t *testing.T) {
	cmd := newAuthCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"methods"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"api_key",
		"security_token",
		"instance_principal",
		"resource_principal",
		"instance_obo_user",
		"oke_workload_identity",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected methods output to contain %q, got %q", want, got)
		}
	}
}

func TestAuthMethodsCommandJSONOutput(t *testing.T) {
	cmd := newAuthCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"methods", "--output", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var got authMethodsResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal methods json: %v\n%s", err, out.String())
	}
	if len(got.Methods) != len(config.ValidAuthMethods()) {
		t.Fatalf("expected %d methods, got %d: %+v", len(config.ValidAuthMethods()), len(got.Methods), got.Methods)
	}
	if got.Methods[0].Method != config.AuthMethodAPIKey || got.Methods[0].Actions.Validate != true {
		t.Fatalf("unexpected first method: %+v", got.Methods[0])
	}
}

func TestAuthMethodsCommandYAMLOutput(t *testing.T) {
	cmd := newAuthCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"methods", "--output", "yaml"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := out.String()
	for _, want := range []string{"methods:", "method: api_key", "method: security_token"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected yaml output to contain %q, got %q", want, got)
		}
	}
}

func TestAuthShowJSONIncludesDaemonUnavailable(t *testing.T) {
	cfg := config.Config{
		Options: config.Options{
			OCIConfigPath: "/tmp/oci",
			SocketPath:    t.TempDir() + "/missing.sock",
		},
		Contexts: []config.Context{{
			Name:       "dev",
			Profile:    "DEFAULT",
			AuthMethod: config.AuthMethodSecurityToken,
			User:       "ocid1.user.oc1..cccc",
		}},
		CurrentContext: "dev",
	}
	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newAuthCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"show", "--config", cfgPath, "--output", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth show: %v\n%s", err, out.String())
	}

	var got authShowResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal auth show json: %v\n%s", err, out.String())
	}
	if got.DaemonAvailable {
		t.Fatalf("expected daemon unavailable, got %+v", got)
	}
	if got.DaemonError == "" {
		t.Fatalf("expected daemon_error in output, got %+v", got)
	}
	if !strings.Contains(out.String(), `"daemon_available": false`) || !strings.Contains(out.String(), `"daemon_error":`) {
		t.Fatalf("expected daemon availability fields in json, got %s", out.String())
	}
}

func TestFindHomeRegion(t *testing.T) {
	payload := []byte(`{
  "data": [
    {"is-home-region": false, "region-key": "PHX", "region-name": "us-phoenix-1", "status": "READY"},
    {"is-home-region": true, "region-key": "IAD", "region-name": "us-ashburn-1", "status": "READY"}
  ]
}`)
	region, err := findHomeRegion(payload)
	if err != nil {
		t.Fatalf("expected home region, got error %v", err)
	}
	if region.RegionName != "us-ashburn-1" || region.RegionKey != "IAD" || !region.IsHomeRegion {
		t.Fatalf("unexpected home region parsed: %+v", region)
	}
}

func TestFindHomeRegionReturnsErrorWhenMissing(t *testing.T) {
	payload := []byte(`{"data":[{"is-home-region":false,"region-key":"PHX","region-name":"us-phoenix-1","status":"READY"}]}`)
	_, err := findHomeRegion(payload)
	if err == nil {
		t.Fatalf("expected error when home region is missing")
	}
}

func TestAuthEnsureRefreshesAndOutputsJSON(t *testing.T) {
	origCapture := runOCICaptureForAuth
	origRun := runOCIForAuth
	defer func() {
		runOCICaptureForAuth = origCapture
		runOCIForAuth = origRun
	}()

	calls := 0
	runOCICaptureForAuth = func(_ *cobra.Command, _ []string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, fmt.Errorf("expired")
		}
		return []byte(`{"data":[{"is-home-region":true,"region-key":"IAD","region-name":"us-ashburn-1","status":"READY"}]}`), nil
	}
	refreshes := 0
	runOCIForAuth = func(_ *cobra.Command, args []string) error {
		refreshes++
		if strings.Join(args[:2], " ") != "session refresh" {
			t.Fatalf("expected session refresh, got %v", args)
		}
		return nil
	}

	cfg := config.Config{
		Options: config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{{
			Name:        "dev",
			Profile:     "DEFAULT",
			AuthMethod:  config.AuthMethodSecurityToken,
			TenancyOCID: "ocid1.tenancy.oc1..aaaa",
			Region:      "us-phoenix-1",
		}},
		CurrentContext: "dev",
	}
	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newAuthCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"ensure", "--config", cfgPath, "--output", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth ensure: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{`"ok": true`, `"state": "refreshed"`, `"refreshed": true`, `"ready": true`, `"action_required": false`, `"action": "none"`, `"context": "dev"`, `"region-name": "us-ashburn-1"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got %s", want, got)
		}
	}
	if refreshes != 1 {
		t.Fatalf("expected one refresh, got %d", refreshes)
	}
}

func TestAuthEnsureNoInteractiveLoginRequiredDoesNotAuthenticate(t *testing.T) {
	origCapture := runOCICaptureForAuth
	origRun := runOCIForAuth
	origNoInteractive := cliNoInteractive
	defer func() {
		runOCICaptureForAuth = origCapture
		runOCIForAuth = origRun
		cliNoInteractive = origNoInteractive
	}()
	cliNoInteractive = false

	runOCICaptureForAuth = func(_ *cobra.Command, _ []string) ([]byte, error) {
		return nil, fmt.Errorf("expired")
	}
	var authenticateCalls int
	runOCIForAuth = func(_ *cobra.Command, args []string) error {
		if strings.Join(args[:2], " ") == "session authenticate" {
			authenticateCalls++
		}
		return fmt.Errorf("refresh failed")
	}

	cfg := config.Config{
		Options: config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{{
			Name:        "dev",
			Profile:     "DEFAULT",
			AuthMethod:  config.AuthMethodSecurityToken,
			TenancyOCID: "ocid1.tenancy.oc1..aaaa",
			Region:      "us-phoenix-1",
		}},
		CurrentContext: "dev",
	}
	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--no-interactive", "auth", "ensure", "--config", cfgPath, "--output", "json", "--login"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected auth ensure to fail when login is required")
	}
	if authenticateCalls != 0 {
		t.Fatalf("expected no interactive authenticate call, got %d", authenticateCalls)
	}

	var got authEnsureResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal ensure json: %v\n%s", err, out.String())
	}
	if got.State != authEnsureStateLoginRequired || !got.LoginRequired || got.LoginCommand == "" {
		t.Fatalf("expected structured login_required result, got %+v", got)
	}
	if got.Ready || !got.ActionRequired || got.Action != "login" || got.Severity != "error" {
		t.Fatalf("expected machine-readable login action, got %+v", got)
	}
	if got.LoginAttempted {
		t.Fatalf("expected login_attempted=false, got %+v", got)
	}
}
