package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestAuthTokenOAuthDeviceServiceCachesAndEmitsJSON(t *testing.T) {
	var tokenRequests int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			fmt.Fprintf(w, `{"device_authorization_endpoint":"%s/device","token_endpoint":"%s/token"}`, server.URL, server.URL)
		case "/device":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("client_id") != "obp-native" || r.Form.Get("scope") != "https://obp.example.com/restproxy" {
				t.Fatalf("unexpected device form: %v", r.Form)
			}
			fmt.Fprint(w, `{"device_code":"device-1","user_code":"ABCD-EFGH","verification_uri":"https://login.example.com/device","expires_in":60,"interval":1}`)
		case "/token":
			tokenRequests++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || r.Form.Get("device_code") != "device-1" {
				t.Fatalf("unexpected token form: %v", r.Form)
			}
			fmt.Fprint(w, `{"access_token":"obp-token","token_type":"Bearer","expires_in":3600}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Config{
		Options: config.Options{
			OCIConfigPath: "/tmp/oci",
			SocketPath:    t.TempDir() + "/daemon.sock",
		},
		TokenServices: []config.TokenService{{
			Name:     "chaincode-obp",
			Type:     config.TokenServiceTypeOAuthDevice,
			Issuer:   server.URL,
			ClientID: "obp-native",
			Scope:    "https://obp.example.com/restproxy",
		}},
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
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{
		"auth", "token",
		"--config", cfgPath,
		"--service", "chaincode-obp",
		"--no-browser",
		"--format", "json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth token: %v\nstdout=%s\nstderr=%s", err, out.String(), errOut.String())
	}

	var got struct {
		AccessToken string `json:"access_token"`
		Service     string `json:"service"`
		Context     string `json:"context"`
		Profile     string `json:"profile"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal token json: %v\n%s", err, out.String())
	}
	if got.AccessToken != "obp-token" || got.Service != "chaincode-obp" || got.Context != "dev" || got.Profile != "DEFAULT" {
		t.Fatalf("unexpected token payload: %+v", got)
	}
	if !strings.Contains(errOut.String(), "ABCD-EFGH") {
		t.Fatalf("expected device code on stderr, got %q", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	cmd = newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{
		"auth", "token",
		"--config", cfgPath,
		"--service", "chaincode-obp",
		"--no-login",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cached auth token: %v", err)
	}
	if out.String() != "obp-token\n" {
		t.Fatalf("expected raw cached token, got %q", out.String())
	}
	if tokenRequests != 1 {
		t.Fatalf("expected cached raw output to avoid another token request, got %d", tokenRequests)
	}
}

func TestAuthTokenNoInteractiveRequiresCachedToken(t *testing.T) {
	cfg := config.Config{
		Options: config.Options{
			OCIConfigPath: "/tmp/oci",
			SocketPath:    t.TempDir() + "/daemon.sock",
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
	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--no-interactive",
		"auth", "token",
		"--config", cfgPath,
		"--issuer", "https://issuer.example.com",
		"--scope", "https://obp.example.com/restproxy",
	})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "no cached obp token") {
		t.Fatalf("expected cached-token error, got %v", err)
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
