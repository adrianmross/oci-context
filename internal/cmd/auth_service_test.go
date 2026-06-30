package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

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
