package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestAuthServiceDiscoverSavesDiscoveredEndpoints(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"authorization_endpoint":%q,"token_endpoint":%q,"device_authorization_endpoint":%q}`,
			server.URL+"/authorize", server.URL+"/token", server.URL+"/device")
	}))
	defer server.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	if err := config.Save(cfgPath, config.DefaultConfig(tmp)); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"service", "discover", "example-service", "--config", cfgPath, "--issuer", server.URL, "--client-id", "example-client", "--scope", "https://service.example.com", "--flow", "authorization-code", "--set-current"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("discover service: %v\n%s", err, out.String())
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	service, ok := findTokenService(loaded, "example-service")
	if !ok {
		t.Fatal("expected discovered service")
	}
	if service.AuthorizationEndpoint != server.URL+"/authorize" || service.TokenEndpoint != server.URL+"/token" || service.DeviceEndpoint != server.URL+"/device" {
		t.Fatalf("unexpected discovered endpoints: %+v", service)
	}
	if service.ClientID != "example-client" || service.Scope != "https://service.example.com" || loaded.CurrentService != "example-service" {
		t.Fatalf("unexpected discovered service: %+v", service)
	}
}

func TestAuthServiceImportYAMLFragment(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	handoffPath := filepath.Join(tmp, "oci-context-token-services.yml")
	if err := os.WriteFile(handoffPath, []byte(`
token_services:
  - name: obp
    type: oauth
    flow: authorization-code
    issuer: https://idcs-example.identity.oraclecloud.com
    token_endpoint: https://idcs-example.identity.oraclecloud.com/oauth2/v1/token
    authorization_endpoint: https://idcs-example.identity.oraclecloud.com/oauth2/v1/authorize
    client_id: example-obp-cli-user
    scope: https://example-oabcs.blockchain.ocp.oraclecloud.com:7443/restproxy
    redirect_url: http://127.0.0.1:8180/callback
  - name: obp-jwt-service
    type: oauth
    flow: jwt-client-credentials
    issuer: https://idcs-example.identity.oraclecloud.com
    token_endpoint: https://idcs-example.identity.oraclecloud.com/oauth2/v1/token
    client_id: example-obp-service-jwt
    scope: https://example-oabcs.blockchain.ocp.oraclecloud.com:7443/restproxy
    private_key_file_env: EXAMPLE_OBP_SERVICE_JWT_PRIVATE_KEY_FILE
    key_id: example-obp-service-jwt-cert
    jwt_audience: https://identity.oraclecloud.com/
`), 0o644); err != nil {
		t.Fatalf("write handoff: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"auth", "service", "import", "--config", cfgPath, "--file", handoffPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("import: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "updated: obp") || !strings.Contains(out.String(), "added: obp-jwt-service") {
		t.Fatalf("unexpected import output:\n%s", out.String())
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	obp, ok := findTokenService(loaded, "obp")
	if !ok {
		t.Fatal("expected obp token service")
	}
	if obp.Flow != "authorization-code" || obp.ClientID != "example-obp-cli-user" {
		t.Fatalf("unexpected obp token service: %+v", obp)
	}
	jwt, ok := findTokenService(loaded, "obp-jwt-service")
	if !ok {
		t.Fatal("expected obp-jwt-service token service")
	}
	if jwt.Flow != "jwt-client-credentials" || jwt.PrivateKeyFileEnv != "EXAMPLE_OBP_SERVICE_JWT_PRIVATE_KEY_FILE" || jwt.JWTAudience != "https://identity.oraclecloud.com/" {
		t.Fatalf("unexpected jwt token service: %+v", jwt)
	}
}

func TestAuthServiceImportOCIIDMJSONHandoffDryRun(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	handoffPath := filepath.Join(tmp, "oci-context.handoff.json")
	if err := os.WriteFile(handoffPath, []byte(`{
  "schemaVersion": "oci-idm.handoff.oci-context.v1",
  "tokenServices": [
    {
      "name": "obp-jwt-service",
      "type": "oauth",
      "flow": "jwt-client-credentials",
      "issuer": "https://idcs-example.identity.oraclecloud.com",
      "tokenEndpoint": "https://idcs-example.identity.oraclecloud.com/oauth2/v1/token",
      "clientId": "example-obp-service-jwt",
      "scope": "https://example-oabcs.blockchain.ocp.oraclecloud.com:7443/restproxy",
      "privateKeyFileEnv": "EXAMPLE_OBP_SERVICE_JWT_PRIVATE_KEY_FILE",
      "keyId": "example-obp-service-jwt-cert",
      "jwtAudience": "https://identity.oraclecloud.com/"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write handoff: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"auth", "service", "import", "--config", cfgPath, "--file", handoffPath, "--dry-run", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("import dry-run: %v\n%s", err, out.String())
	}
	var result authServiceImportResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, out.String())
	}
	if !result.DryRun || len(result.Added) != 1 || result.Added[0] != "obp-jwt-service" {
		t.Fatalf("unexpected dry-run result: %+v", result)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if _, ok := findTokenService(loaded, "obp-jwt-service"); ok {
		t.Fatal("dry-run should not persist imported token service")
	}
}

func TestServiceImportRejectsUnsupportedExportContract(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	if err := config.Save(cfgPath, config.DefaultConfig(tmp)); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(`{"apiVersion":"example.invalid/v2","kind":"OCIContextTokenServiceExport","tokenServices":[{"name":"example","type":"oauth","flow":"authorization-code","issuer":"https://example.identity.oraclecloud.com","clientId":"example-client","scope":"https://service.example.com"}]}`))
	cmd.SetArgs([]string{"service", "import", "--config", cfgPath})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported token-service export apiVersion") {
		t.Fatalf("expected contract error, got %v\n%s", err, out.String())
	}
}

func TestServiceVerifyHandoffDetectsRedirectDrift(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	cfg.CurrentService = "example-service"
	cfg.TokenServices = append(cfg.TokenServices, config.TokenService{
		Name: "example-service", Type: "oauth", Flow: "authorization-code",
		Issuer: "https://example.identity.oraclecloud.com", ClientID: "example-client",
		Scope: "https://service.example.com", RedirectURL: "http://127.0.0.1:8180/callback",
		AuthorizationEndpoint: "https://example.identity.oraclecloud.com/oauth2/v1/authorize",
		TokenEndpoint:         "https://example.identity.oraclecloud.com/oauth2/v1/token", OfflineAccess: true,
	})
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	handoffPath := filepath.Join(tmp, "oci-context.handoff.json")
	if err := os.WriteFile(handoffPath, []byte(`{
  "currentService": "example-service",
  "tokenServices": [{
    "name": "example-service", "type": "oauth", "flow": "authorization-code",
    "issuer": "https://example.identity.oraclecloud.com", "clientId": "example-client",
    "scope": "https://service.example.com", "redirectUrl": "http://127.0.0.1:8180/callback",
    "authorizationEndpoint": "https://example.identity.oraclecloud.com/oauth2/v1/authorize",
    "tokenEndpoint": "https://example.identity.oraclecloud.com/oauth2/v1/token", "offlineAccess": true
  }]
}`), 0o600); err != nil {
		t.Fatalf("write handoff: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"service", "verify", "--config", cfgPath, "--file", handoffPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify matching handoff: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "verified: example-service") {
		t.Fatalf("unexpected verify output: %s", out.String())
	}

	cfg.TokenServices[len(cfg.TokenServices)-1].RedirectURL = "http://127.0.0.1:8282/callback"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save drifted config: %v", err)
	}
	cmd = newRootCmd()
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"service", "verify", "--config", cfgPath, "--file", handoffPath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected redirect drift to fail verification")
	}
	if !strings.Contains(out.String(), "mismatch: redirect_url") {
		t.Fatalf("unexpected drift output: %s", out.String())
	}
}

func TestServiceSyncPreviewsThenAppliesReviewedHandoff(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	if err := config.Save(cfgPath, config.DefaultConfig(tmp)); err != nil {
		t.Fatal(err)
	}
	handoffPath := filepath.Join(tmp, "handoff.yml")
	if err := os.WriteFile(handoffPath, []byte(`current_service: example-service
token_services:
  - name: example-service
    type: oauth
    flow: authorization-code
    issuer: https://example.identity.oraclecloud.com
    client_id: example-client
    scope: https://service.example.com
    redirect_url: http://127.0.0.1:8180/callback
    authorization_endpoint: https://example.identity.oraclecloud.com/oauth2/v1/authorize
    token_endpoint: https://example.identity.oraclecloud.com/oauth2/v1/token
    offline_access: true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"service"}, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}
	if out := run("sync", "--config", cfgPath, "--file", handoffPath, "--set-current"); !strings.Contains(out, "added: example-service") {
		t.Fatalf("missing preview: %s", out)
	}
	before, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findTokenService(before, "example-service"); ok {
		t.Fatal("preview wrote config")
	}
	run("sync", "--config", cfgPath, "--file", handoffPath, "--set-current", "--apply")
	after, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentService != "example-service" {
		t.Fatalf("wrong current service: %q", after.CurrentService)
	}
	run("verify", "--config", cfgPath, "--file", handoffPath)
	inlinePath := filepath.Join(tmp, "inline.yml")
	data, err := os.ReadFile(handoffPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inlinePath, append(data, []byte("    client_secret: forbidden\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	cmd.SetArgs([]string{"service", "sync", "--config", cfgPath, "--file", inlinePath, "--apply"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "inline credentials") {
		t.Fatalf("expected inline credential rejection, got %v", err)
	}
}

func TestHandoffAcceptPreviewsThenAppliesAndPrintsSafeNextSteps(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	if err := config.Save(cfgPath, config.DefaultConfig(tmp)); err != nil {
		t.Fatal(err)
	}
	handoffPath := filepath.Join(tmp, "handoff.yml")
	if err := os.WriteFile(handoffPath, []byte(`current_service: example-service
token_services:
  - name: example-service
    type: oauth
    flow: authorization-code
    issuer: https://example.identity.oraclecloud.com
    client_id: example-client
    scope: https://service.example.com
`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"handoff", "accept", "--config", cfgPath, "--file", handoffPath, "--set-current"}, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}
	if out := run(); !strings.Contains(out, "Would import") || !strings.Contains(out, "re-run with --apply") {
		t.Fatalf("unexpected preview:\n%s", out)
	}
	if _, ok := findTokenService(mustLoadConfig(t, cfgPath), "example-service"); ok {
		t.Fatal("preview wrote config")
	}
	if out := run("--apply"); !strings.Contains(out, "auth token --service \"example-service\"") || !strings.Contains(out, "whoami --service \"example-service\"") {
		t.Fatalf("missing next steps:\n%s", out)
	}
}

func TestServiceImportPreviewsThenAppliesFromStdin(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	if err := config.Save(cfgPath, config.DefaultConfig(tmp)); err != nil {
		t.Fatal(err)
	}
	handoff := `{
  "schemaVersion": "oci-idm.handoff.oci-context.v1",
  "currentService": "example-service",
  "tokenServices": [{
    "name": "example-service",
    "type": "oauth",
    "flow": "authorization-code",
    "issuer": "https://example.identity.oraclecloud.com",
    "clientId": "example-client",
    "scope": "https://service.example.com"
  }]
}`
	run := func(args ...string) string {
		t.Helper()
		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(strings.NewReader(handoff))
		cmd.SetArgs(append([]string{"service", "import", "--config", cfgPath, "--set-current"}, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}
	if out := run(); !strings.Contains(out, "Would import") || !strings.Contains(out, "re-run with --apply") {
		t.Fatalf("unexpected preview:\n%s", out)
	}
	if _, ok := findTokenService(mustLoadConfig(t, cfgPath), "example-service"); ok {
		t.Fatal("preview wrote config")
	}
	run("--apply")
	cfg := mustLoadConfig(t, cfgPath)
	if cfg.CurrentService != "example-service" {
		t.Fatalf("current service = %q", cfg.CurrentService)
	}
}

func mustLoadConfig(t *testing.T, path string) config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestServiceAddFromStdinSetsCurrentService(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(`{
  "schemaVersion": "oci-idm.handoff.oci-context.v1",
  "currentService": "hebe-obp-user",
  "tokenServices": [
    {
      "name": "hebe-obp-user",
      "type": "oauth",
      "flow": "authorization-code",
      "issuer": "https://idcs-example.identity.oraclecloud.com",
      "authorizationEndpoint": "https://idcs-example.identity.oraclecloud.com/oauth2/v1/authorize",
      "tokenEndpoint": "https://idcs-example.identity.oraclecloud.com/oauth2/v1/token",
      "clientId": "hebe-obp-user",
      "scope": "https://example-oabcs.blockchain.ocp.oraclecloud.com:7443/restproxy",
      "redirectUrl": "http://127.0.0.1:8180/callback"
    }
  ]
}`))
	cmd.SetArgs([]string{"service", "add", "--config", cfgPath, "--set-current"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("service add: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "added: hebe-obp-user") || !strings.Contains(out.String(), "current_service: hebe-obp-user") {
		t.Fatalf("unexpected service add output:\n%s", out.String())
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.CurrentService != "hebe-obp-user" {
		t.Fatalf("expected current service hebe-obp-user, got %q", loaded.CurrentService)
	}
	service, ok := findTokenService(loaded, "hebe-obp-user")
	if !ok {
		t.Fatal("expected imported token service")
	}
	if service.ClientID != "hebe-obp-user" || service.Flow != "authorization-code" {
		t.Fatalf("unexpected imported service: %+v", service)
	}
}

func TestAuthServiceListRedactsSecrets(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	cfg.TokenServices = append(cfg.TokenServices, config.TokenService{
		Name:         "secret-service",
		Type:         "oauth",
		Flow:         "client-credentials",
		ClientID:     "client",
		ClientSecret: "do-not-print",
		Scope:        "scope",
	})
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"auth", "service", "list", "--config", cfgPath, "-o", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "do-not-print") {
		t.Fatalf("list output leaked secret:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "client_secret_configured") {
		t.Fatalf("expected secret configured marker:\n%s", out.String())
	}
}

func TestServiceGetDefaultsToCurrentAndRedactsSecrets(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	cfg.CurrentService = "hebe-obp-user"
	cfg.TokenServices = append(cfg.TokenServices, config.TokenService{
		Name:                  "hebe-obp-user",
		Type:                  "oauth",
		Flow:                  "authorization-code",
		AuthorizationEndpoint: "https://idcs.example/oauth2/v1/authorize",
		TokenEndpoint:         "https://idcs.example/oauth2/v1/token",
		ClientID:              "client-id",
		ClientSecret:          "do-not-print",
		Scope:                 "https://obp.example/restproxy",
		RedirectURL:           "http://127.0.0.1:8180/callback",
	})
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"service", "get", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("get: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "do-not-print") {
		t.Fatalf("get output leaked secret:\n%s", out.String())
	}
	var view authServiceView
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out.String())
	}
	if view.Name != "hebe-obp-user" || view.Flow != "authorization-code" || !view.ClientSecretConfigured {
		t.Fatalf("unexpected service view: %+v", view)
	}
	if view.Credential.Command != "oci-context" || strings.Join(view.Credential.Args, " ") != "auth token --service hebe-obp-user --no-login --format raw" {
		t.Fatalf("unexpected credential command: %+v", view.Credential)
	}
	if view.InteractiveCredential == nil || view.InteractiveCredential.Command != "oci-context" || strings.Join(view.InteractiveCredential.Args, " ") != "auth token --service hebe-obp-user --format raw" {
		t.Fatalf("unexpected interactive credential command: %+v", view.InteractiveCredential)
	}
}

func TestServiceGetRequiresASelection(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	if err := config.Save(cfgPath, config.DefaultConfig(tmp)); err != nil {
		t.Fatalf("save config: %v", err)
	}
	cmd := newRootCmd()
	cmd.SetArgs([]string{"service", "get", "--config", cfgPath})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "no current token service") {
		t.Fatalf("expected missing current service error, got %v", err)
	}
}
