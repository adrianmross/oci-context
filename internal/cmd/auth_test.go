package cmd

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

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

func TestResolveLoopbackRedirectRejectsCloudGatePlaceholder(t *testing.T) {
	_, _, _, err := resolveLoopbackRedirect("https://%hostid%/cloudgate/v1/oauth2/callback")
	if err == nil {
		t.Fatal("expected CloudGate callback error")
	}
	if !strings.Contains(err.Error(), "CloudGate") {
		t.Fatalf("expected CloudGate-specific error, got %v", err)
	}
}

func TestResolveLoopbackRedirectRejectsHTTPSCallback(t *testing.T) {
	_, _, _, err := resolveLoopbackRedirect("https://example.com/oauth/callback")
	if err == nil {
		t.Fatal("expected non-loopback callback error")
	}
	if !strings.Contains(err.Error(), "http loopback URL") {
		t.Fatalf("expected loopback-specific error, got %v", err)
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

func TestAuthTokenOAuthAuthorizationCodeServiceCachesAndRefreshes(t *testing.T) {
	var authorizationRedirect string
	var tokenRequests int
	var refreshRequests int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			fmt.Fprintf(w, `{"authorization_endpoint":"%s/authorize","token_endpoint":"%s/token"}`, server.URL, server.URL)
		case "/authorize":
			query := r.URL.Query()
			if query.Get("response_type") != "code" ||
				query.Get("client_id") != "obp-native" ||
				query.Get("scope") != "https://obp.example.com/restproxy" ||
				query.Get("code_challenge_method") != "S256" ||
				query.Get("code_challenge") == "" {
				t.Fatalf("unexpected authorization query: %v", query)
			}
			authorizationRedirect = query.Get("redirect_uri")
			redirect, err := url.Parse(authorizationRedirect)
			if err != nil {
				t.Fatalf("parse redirect: %v", err)
			}
			values := redirect.Query()
			values.Set("code", "auth-code-1")
			values.Set("state", query.Get("state"))
			redirect.RawQuery = values.Encode()
			http.Redirect(w, r, redirect.String(), http.StatusFound)
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				tokenRequests++
				if r.Form.Get("code") != "auth-code-1" ||
					r.Form.Get("client_id") != "obp-native" ||
					r.Form.Get("redirect_uri") != authorizationRedirect ||
					r.Form.Get("code_verifier") == "" {
					t.Fatalf("unexpected authorization-code token form: %v", r.Form)
				}
				fmt.Fprint(w, `{"access_token":"obp-user-token","refresh_token":"refresh-1","token_type":"Bearer","expires_in":1}`)
			case "refresh_token":
				refreshRequests++
				if r.Form.Get("refresh_token") != "refresh-1" || r.Form.Get("client_id") != "obp-native" {
					t.Fatalf("unexpected refresh token form: %v", r.Form)
				}
				fmt.Fprint(w, `{"access_token":"obp-refreshed-token","token_type":"Bearer","expires_in":3600}`)
			default:
				t.Fatalf("unexpected token grant: %v", r.Form)
			}
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
			Flow:     "authorization-code",
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
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	authURL := waitForAuthURL(t, &errOut)
	resp, err := http.Get(authURL)
	if err != nil {
		t.Fatalf("follow authorization redirect: %v", err)
	}
	_ = resp.Body.Close()

	if err := <-errCh; err != nil {
		t.Fatalf("auth token: %v\nstdout=%s\nstderr=%s", err, out.String(), errOut.String())
	}
	var got struct {
		AccessToken string `json:"access_token"`
		Service     string `json:"service"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal token json: %v\n%s", err, out.String())
	}
	if got.AccessToken != "obp-user-token" || got.Service != "chaincode-obp" {
		t.Fatalf("unexpected token payload: %+v", got)
	}

	time.Sleep(1100 * time.Millisecond)
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
		t.Fatalf("cached refresh auth token: %v\nstderr=%s", err, errOut.String())
	}
	if out.String() != "obp-refreshed-token\n" {
		t.Fatalf("expected refreshed raw token, got %q", out.String())
	}
	if tokenRequests != 1 || refreshRequests != 1 {
		t.Fatalf("expected one code token request and one refresh, got code=%d refresh=%d", tokenRequests, refreshRequests)
	}
}

func TestAuthTokenJWTClientCredentialsUsesClientAssertion(t *testing.T) {
	var tokenRequests int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		tokenRequests++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" ||
			r.Form.Get("client_id") != "obp-sa" ||
			r.Form.Get("scope") != "https://obp.example.com/restproxy" ||
			r.Form.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" ||
			r.Form.Get("client_assertion") != "client.jwt.assertion" {
			t.Fatalf("unexpected client-credentials form: %v", r.Form)
		}
		fmt.Fprint(w, `{"access_token":"service-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()

	cfgPath := writeAuthTokenTestConfig(t)
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"auth", "token",
		"--config", cfgPath,
		"--service", "obp",
		"--flow", "jwt-client-credentials",
		"--token-endpoint", server.URL + "/token",
		"--client-id", "obp-sa",
		"--scope", "https://obp.example.com/restproxy",
		"--client-assertion", "client.jwt.assertion",
		"--no-login",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth token: %v", err)
	}
	if out.String() != "service-token\n" {
		t.Fatalf("expected service token, got %q", out.String())
	}
	if tokenRequests != 1 {
		t.Fatalf("expected one token request, got %d", tokenRequests)
	}
}

func TestAuthTokenJWTClientCredentialsCanSignClientAssertion(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPath := t.TempDir() + "/client.key"
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		assertion := r.Form.Get("client_assertion")
		header, claims := decodeUnsignedJWT(t, assertion)
		if header["alg"] != "RS256" || header["kid"] != "kid-1" {
			t.Fatalf("unexpected assertion header: %v", header)
		}
		if claims["iss"] != "obp-sa" ||
			claims["sub"] != "obp-sa" ||
			claims["aud"] != server.URL+"/token" {
			t.Fatalf("unexpected assertion claims: %v", claims)
		}
		fmt.Fprint(w, `{"access_token":"signed-service-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()

	cfgPath := writeAuthTokenTestConfig(t)
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"auth", "token",
		"--config", cfgPath,
		"--service", "obp",
		"--flow", "jwt-client-credentials",
		"--token-endpoint", server.URL + "/token",
		"--client-id", "obp-sa",
		"--scope", "https://obp.example.com/restproxy",
		"--private-key-file", keyPath,
		"--key-id", "kid-1",
		"--no-login",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth token: %v", err)
	}
	if out.String() != "signed-service-token\n" {
		t.Fatalf("expected signed service token, got %q", out.String())
	}
}

func TestAuthTokenJWTBearerUsesUserAssertion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" ||
			r.Form.Get("client_id") != "obp-user-rep" ||
			r.Form.Get("scope") != "https://obp.example.com/restproxy" ||
			r.Form.Get("assertion") != "user.jwt.assertion" {
			t.Fatalf("unexpected jwt-bearer form: %v", r.Form)
		}
		fmt.Fprint(w, `{"access_token":"user-rep-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()

	cfgPath := writeAuthTokenTestConfig(t)
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"auth", "token",
		"--config", cfgPath,
		"--service", "obp",
		"--flow", "jwt-bearer",
		"--token-endpoint", server.URL + "/token",
		"--client-id", "obp-user-rep",
		"--scope", "https://obp.example.com/restproxy",
		"--assertion", "user.jwt.assertion",
		"--no-login",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth token: %v", err)
	}
	if out.String() != "user-rep-token\n" {
		t.Fatalf("expected user representation token, got %q", out.String())
	}
}

func TestAuthTokenTokenExchangeUsesSubjectTokenCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:token-exchange" ||
			r.Form.Get("client_id") != "obp-workload" ||
			r.Form.Get("scope") != "https://obp.example.com/restproxy" ||
			r.Form.Get("subject_token_type") != "urn:ietf:params:oauth:token-type:jwt" ||
			r.Form.Get("requested_token_type") != "urn:ietf:params:oauth:token-type:access_token" ||
			r.Form.Get("subject_token") != "workload.jwt.token" {
			t.Fatalf("unexpected token-exchange form: %v", r.Form)
		}
		fmt.Fprint(w, `{"access_token":"workload-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()

	cfgPath := writeAuthTokenTestConfig(t)
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"auth", "token",
		"--config", cfgPath,
		"--service", "obp",
		"--flow", "token-exchange",
		"--token-endpoint", server.URL + "/token",
		"--client-id", "obp-workload",
		"--scope", "https://obp.example.com/restproxy",
		"--subject-token-command", "printf workload.jwt.token",
		"--requested-token-type", "urn:ietf:params:oauth:token-type:access_token",
		"--no-login",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth token: %v", err)
	}
	if out.String() != "workload-token\n" {
		t.Fatalf("expected workload token, got %q", out.String())
	}
}

func writeAuthTokenTestConfig(t *testing.T) string {
	t.Helper()
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
	cfgPath := t.TempDir() + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return cfgPath
}

func decodeUnsignedJWT(t *testing.T, token string) (map[string]string, map[string]any) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected three JWT parts, got %q", token)
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode JWT header: %v", err)
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT claims: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal JWT header: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}
	return header, claims
}

func waitForAuthURL(t *testing.T, errOut *bytes.Buffer) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(errOut.String(), "\n") {
			if strings.HasPrefix(line, "Open ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "Open "))
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for auth URL in stderr: %q", errOut.String())
	return ""
}

func TestAuthTokenDefaultOBPServiceUsesGenericEnvBindings(t *testing.T) {
	t.Setenv("OCHAIN_OBP_AUTH_ISSUER", "https://issuer.example.com")
	t.Setenv("OCHAIN_OBP_AUTH_CLIENT_ID", "obp-native")
	t.Setenv("OCHAIN_OBP_AUTH_SCOPE", "https://obp.example.com/restproxy")
	t.Setenv("OCHAIN_OBP_AUTH_TOKEN_ENDPOINT", "https://issuer.example.com/token")
	t.Setenv("OCHAIN_OBP_AUTH_DEVICE_ENDPOINT", "https://issuer.example.com/device")

	request, err := resolveTokenServiceRequest(config.Config{}, tokenServiceOptions{Service: "obp"})
	if err != nil {
		t.Fatalf("resolve token service: %v", err)
	}
	if request.Service != "obp" || request.Type != config.TokenServiceTypeOAuth {
		t.Fatalf("unexpected service metadata: %+v", request)
	}
	if request.Issuer != "https://issuer.example.com" ||
		request.ClientID != "obp-native" ||
		request.Scope != "https://obp.example.com/restproxy" ||
		request.TokenEndpoint != "https://issuer.example.com/token" ||
		request.DeviceEndpoint != "https://issuer.example.com/device" {
		t.Fatalf("unexpected request: %+v", request)
	}
}

func TestAuthTokenConfiguredServiceUsesGenericEnvLists(t *testing.T) {
	t.Setenv("CHAINCODE_ISSUER", "https://issuer.example.com")
	t.Setenv("CHAINCODE_SCOPE", "https://obp.example.com/restproxy")

	cfg := config.Config{
		TokenServices: []config.TokenService{{
			Name:       "chaincode",
			Type:       config.TokenServiceTypeOAuthDevice,
			IssuerEnvs: []string{"CHAINCODE_ISSUER"},
			ClientID:   "chaincode-client",
			ScopeEnvs:  []string{"CHAINCODE_SCOPE"},
		}},
	}

	request, err := resolveTokenServiceRequest(cfg, tokenServiceOptions{Service: "chaincode"})
	if err != nil {
		t.Fatalf("resolve token service: %v", err)
	}
	if request.Issuer != "https://issuer.example.com" ||
		request.ClientID != "chaincode-client" ||
		request.Scope != "https://obp.example.com/restproxy" {
		t.Fatalf("unexpected request: %+v", request)
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
