package cmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

type authResolvePathFunc func(*cobra.Command) (string, error)
type authLoadTargetFunc func(string) (config.Config, config.Context, error)

type authTokenCacheEntry struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Service      string `json:"service"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	Scope        string `json:"scope"`
	UpdatedAt    string `json:"updated_at"`
}

type oauthDiscoveryDocument struct {
	AuthorizationEndpoint       string `json:"authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

type oauthDeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

var httpClientForAuthToken = &http.Client{Timeout: 15 * time.Second}

func newAuthTokenCmd(resolvePath authResolvePathFunc, loadTarget authLoadTargetFunc) *cobra.Command {
	var service string
	var audience string
	var issuer string
	var clientID string
	var clientSecret string
	var scope string
	var authorizationEndpoint string
	var tokenEndpoint string
	var deviceEndpoint string
	var redirectURL string
	var flow string
	var format string
	var noLogin bool
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "token",
		Short: "Emit an access token for command handoff",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}

			request, err := resolveTokenServiceRequest(cfg, tokenServiceOptions{
				Service:               service,
				Audience:              audience,
				Issuer:                issuer,
				ClientID:              clientID,
				ClientSecret:          clientSecret,
				Scope:                 scope,
				AuthorizationEndpoint: authorizationEndpoint,
				TokenEndpoint:         tokenEndpoint,
				DeviceEndpoint:        deviceEndpoint,
				RedirectURL:           redirectURL,
				Flow:                  flow,
			})
			if err != nil {
				return err
			}
			cachePath, err := authTokenCachePath(cfg, request.Service, request.Issuer, request.ClientID, request.Scope)
			if err != nil {
				return err
			}
			entry, err := readAuthTokenCache(cachePath)
			if err == nil && authTokenUsable(entry) {
				return printAuthToken(cmd, entry, format, ctx)
			}
			if err == nil && strings.TrimSpace(entry.RefreshToken) != "" {
				refreshed, refreshErr := refreshOAuthToken(request, entry.RefreshToken)
				if refreshErr == nil {
					oldRefreshToken := entry.RefreshToken
					entry = cacheEntryFromToken(request, refreshed)
					entry.RefreshToken = firstNonEmpty(refreshed.RefreshToken, oldRefreshToken)
					if err := writeAuthTokenCache(cachePath, entry); err != nil {
						return err
					}
					return printAuthToken(cmd, entry, format, ctx)
				}
			}
			if noLogin || commandNoInteractive(cmd) {
				return fmt.Errorf("no cached %s token is available; run `oci-context auth token --service %s` in an interactive shell", request.Service, request.Service)
			}
			entry, err = runOAuthLogin(cmd, request, !noBrowser)
			if err != nil {
				return err
			}
			if err := writeAuthTokenCache(cachePath, entry); err != nil {
				return err
			}
			return printAuthToken(cmd, entry, format, ctx)
		},
	}
	cmd.Flags().StringVar(&service, "service", "obp", "Token service name")
	cmd.Flags().StringVar(&audience, "audience", "", "Deprecated alias for --service")
	cmd.Flags().StringVar(&issuer, "issuer", "", "OAuth issuer URL")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth public client id")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth client secret for confidential clients; prefer env-backed config over this flag")
	cmd.Flags().StringVar(&scope, "scope", "", "OAuth scope")
	cmd.Flags().StringVar(&authorizationEndpoint, "authorization-endpoint", "", "OAuth authorization endpoint")
	cmd.Flags().StringVar(&tokenEndpoint, "token-endpoint", "", "OAuth token endpoint")
	cmd.Flags().StringVar(&deviceEndpoint, "device-endpoint", "", "OAuth device authorization endpoint")
	cmd.Flags().StringVar(&redirectURL, "redirect-url", "", "OAuth authorization-code loopback redirect URL")
	cmd.Flags().StringVar(&flow, "flow", "auto", "OAuth interactive flow: auto|authorization-code|device")
	cmd.Flags().StringVar(&format, "format", "raw", "Output format: raw|json")
	cmd.Flags().BoolVar(&noLogin, "no-login", false, "Fail instead of starting an interactive login")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open the verification URL in a browser")
	return cmd
}

type tokenServiceOptions struct {
	Service               string
	Audience              string
	Issuer                string
	ClientID              string
	ClientSecret          string
	Scope                 string
	AuthorizationEndpoint string
	TokenEndpoint         string
	DeviceEndpoint        string
	RedirectURL           string
	Flow                  string
}

type tokenServiceRequest struct {
	Service               string
	Type                  string
	Issuer                string
	ClientID              string
	ClientSecret          string
	Scope                 string
	AuthorizationEndpoint string
	TokenEndpoint         string
	DeviceEndpoint        string
	RedirectURL           string
	Flow                  string
}

func resolveTokenServiceRequest(cfg config.Config, opts tokenServiceOptions) (tokenServiceRequest, error) {
	serviceName := firstNonEmpty(opts.Service, opts.Audience, "obp")
	serviceCfg, found := findTokenService(cfg, serviceName)
	if !found {
		serviceCfg, found = defaultTokenService(serviceName)
	}
	if !found && serviceName != "obp" && opts.Issuer == "" && opts.TokenEndpoint == "" {
		return tokenServiceRequest{}, fmt.Errorf("token service %q is not configured", serviceName)
	}
	serviceType := firstNonEmpty(serviceCfg.Type, config.TokenServiceTypeOAuth)
	if serviceType != config.TokenServiceTypeOAuth && serviceType != config.TokenServiceTypeOAuthDevice {
		return tokenServiceRequest{}, fmt.Errorf("token service %q has unsupported type %q", serviceName, serviceType)
	}
	request := tokenServiceRequest{
		Service: serviceName,
		Type:    serviceType,
		Issuer: firstNonEmpty(
			opts.Issuer,
			envValue(serviceCfg.IssuerEnv),
			envValues(serviceCfg.IssuerEnvs...),
			serviceCfg.Issuer,
		),
		ClientID: firstNonEmpty(
			opts.ClientID,
			envValue(serviceCfg.ClientIDEnv),
			envValues(serviceCfg.ClientIDEnvs...),
			serviceCfg.ClientID,
		),
		ClientSecret: firstNonEmpty(
			opts.ClientSecret,
			envValue(serviceCfg.ClientSecretEnv),
			envValues(serviceCfg.ClientSecretEnvs...),
			serviceCfg.ClientSecret,
		),
		Scope: firstNonEmpty(
			opts.Scope,
			envValue(serviceCfg.ScopeEnv),
			envValues(serviceCfg.ScopeEnvs...),
			serviceCfg.Scope,
		),
		AuthorizationEndpoint: firstNonEmpty(
			opts.AuthorizationEndpoint,
			envValue(serviceCfg.AuthorizationEndpointEnv),
			envValues(serviceCfg.AuthorizationEndpointEnvs...),
			serviceCfg.AuthorizationEndpoint,
		),
		TokenEndpoint: firstNonEmpty(
			opts.TokenEndpoint,
			envValue(serviceCfg.TokenEndpointEnv),
			envValues(serviceCfg.TokenEndpointEnvs...),
			serviceCfg.TokenEndpoint,
		),
		DeviceEndpoint: firstNonEmpty(
			opts.DeviceEndpoint,
			envValue(serviceCfg.DeviceEnv),
			envValues(serviceCfg.DeviceEnvs...),
			serviceCfg.DeviceEndpoint,
		),
		RedirectURL: firstNonEmpty(
			opts.RedirectURL,
			envValue(serviceCfg.RedirectURLEnv),
			envValues(serviceCfg.RedirectURLEnvs...),
			serviceCfg.RedirectURL,
		),
		Flow: firstNonEmpty(opts.Flow, serviceCfg.Flow, "auto"),
	}
	if request.ClientID == "" {
		return tokenServiceRequest{}, fmt.Errorf("token service %q client_id is required", serviceName)
	}
	return request, nil
}

func findTokenService(cfg config.Config, name string) (config.TokenService, bool) {
	for _, service := range cfg.TokenServices {
		if service.Name == name {
			return service, true
		}
	}
	return config.TokenService{}, false
}

func runOAuthLogin(cmd *cobra.Command, request tokenServiceRequest, openBrowser bool) (authTokenCacheEntry, error) {
	if strings.TrimSpace(request.Issuer) == "" {
		return authTokenCacheEntry{}, fmt.Errorf("token service %q issuer is required", request.Service)
	}
	if strings.TrimSpace(request.Scope) == "" {
		return authTokenCacheEntry{}, fmt.Errorf("token service %q scope is required", request.Service)
	}
	if err := resolveOAuthLoginEndpoints(&request); err != nil {
		return authTokenCacheEntry{}, err
	}

	flow := normalizeOAuthFlow(request.Flow)
	if flow == "auto" {
		switch {
		case request.AuthorizationEndpoint != "":
			flow = "authorization-code"
		case request.DeviceEndpoint != "":
			flow = "device"
		default:
			return authTokenCacheEntry{}, fmt.Errorf("issuer discovery did not provide authorization or device endpoints")
		}
	}

	switch flow {
	case "authorization-code":
		return runOAuthAuthorizationCodeLogin(cmd, request, openBrowser)
	case "device":
		return runOAuthDeviceLogin(cmd, request, openBrowser)
	default:
		return authTokenCacheEntry{}, fmt.Errorf("unsupported OAuth flow %q", request.Flow)
	}
}

func resolveOAuthLoginEndpoints(request *tokenServiceRequest) error {
	if request.TokenEndpoint != "" &&
		(request.AuthorizationEndpoint != "" || request.DeviceEndpoint != "") {
		return nil
	}
	discovery, err := fetchOAuthDiscovery(request.Issuer)
	if err != nil {
		return err
	}
	if request.AuthorizationEndpoint == "" {
		request.AuthorizationEndpoint = discovery.AuthorizationEndpoint
	}
	if request.TokenEndpoint == "" {
		request.TokenEndpoint = discovery.TokenEndpoint
	}
	if request.DeviceEndpoint == "" {
		request.DeviceEndpoint = discovery.DeviceAuthorizationEndpoint
	}
	return nil
}

func normalizeOAuthFlow(flow string) string {
	switch strings.ToLower(strings.TrimSpace(flow)) {
	case "", "auto":
		return "auto"
	case "authorization_code", "authorization-code", "auth-code", "code":
		return "authorization-code"
	case "device", "device-code", "device_code":
		return "device"
	default:
		return strings.ToLower(strings.TrimSpace(flow))
	}
}

func defaultTokenService(name string) (config.TokenService, bool) {
	for _, service := range config.DefaultTokenServices() {
		if service.Name == name {
			return service, true
		}
	}
	return config.TokenService{}, false
}

func runOAuthDeviceLogin(cmd *cobra.Command, request tokenServiceRequest, openBrowser bool) (authTokenCacheEntry, error) {
	if request.TokenEndpoint == "" || request.DeviceEndpoint == "" {
		return authTokenCacheEntry{}, fmt.Errorf("issuer discovery did not provide device and token endpoints")
	}

	device, err := startOAuthDeviceAuthorization(request)
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	verification := firstNonEmpty(device.VerificationURIComplete, device.VerificationURI)
	if verification != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Open %s\n", verification)
		if openBrowser {
			openURLBestEffort(verification)
		}
	}
	if device.UserCode != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Enter code %s\n", device.UserCode)
	}
	token, err := pollOAuthDeviceToken(request, device)
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	return cacheEntryFromToken(request, token), nil
}

func fetchOAuthDiscovery(issuer string) (oauthDiscoveryDocument, error) {
	endpoint := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	resp, err := httpClientForAuthToken.Get(endpoint)
	if err != nil {
		return oauthDiscoveryDocument{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return oauthDiscoveryDocument{}, fmt.Errorf("issuer discovery failed with HTTP %d", resp.StatusCode)
	}
	var doc oauthDiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return oauthDiscoveryDocument{}, err
	}
	return doc, nil
}

type oauthAuthorizationCallback struct {
	Code  string
	State string
	Error string
}

func runOAuthAuthorizationCodeLogin(cmd *cobra.Command, request tokenServiceRequest, openBrowser bool) (authTokenCacheEntry, error) {
	if request.AuthorizationEndpoint == "" || request.TokenEndpoint == "" {
		return authTokenCacheEntry{}, fmt.Errorf("issuer discovery did not provide authorization and token endpoints")
	}
	redirectURL, server, callbacks, err := startAuthorizationCodeCallbackServer(request.RedirectURL)
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	defer server.Shutdown(context.Background())

	state, err := randomURLSafeString(32)
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	verifier, err := randomURLSafeString(64)
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	challenge := pkceChallenge(verifier)
	authURL, err := buildAuthorizationURL(request, redirectURL, state, challenge)
	if err != nil {
		return authTokenCacheEntry{}, err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Open %s\n", authURL)
	if openBrowser {
		openURLBestEffort(authURL)
	}

	var callback oauthAuthorizationCallback
	select {
	case callback = <-callbacks:
	case <-time.After(5 * time.Minute):
		return authTokenCacheEntry{}, fmt.Errorf("authorization code login timed out")
	}
	if callback.Error != "" {
		return authTokenCacheEntry{}, fmt.Errorf("authorization failed: %s", callback.Error)
	}
	if callback.State != state {
		return authTokenCacheEntry{}, fmt.Errorf("authorization callback state did not match")
	}
	if callback.Code == "" {
		return authTokenCacheEntry{}, fmt.Errorf("authorization callback did not include a code")
	}

	token, err := exchangeAuthorizationCode(request, callback.Code, redirectURL, verifier)
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	return cacheEntryFromToken(request, token), nil
}

func startAuthorizationCodeCallbackServer(rawRedirectURL string) (string, *http.Server, <-chan oauthAuthorizationCallback, error) {
	redirectURL, listenAddress, callbackPath, err := resolveLoopbackRedirect(rawRedirectURL)
	if err != nil {
		return "", nil, nil, err
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return "", nil, nil, err
	}
	if rawRedirectURL == "" {
		redirectURL = "http://" + listener.Addr().String() + callbackPath
	}
	callbacks := make(chan oauthAuthorizationCallback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		callbacks <- oauthAuthorizationCallback{
			Code:  query.Get("code"),
			State: query.Get("state"),
			Error: firstNonEmpty(query.Get("error_description"), query.Get("error")),
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "Authorization received. You can return to oci-context.")
	})
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	return redirectURL, server, callbacks, nil
}

func resolveLoopbackRedirect(rawRedirectURL string) (redirectURL string, listenAddress string, callbackPath string, err error) {
	if rawRedirectURL == "" {
		return "", "127.0.0.1:0", "/callback", nil
	}
	if isCloudGateCallback(rawRedirectURL) {
		return "", "", "", cloudGateCallbackError()
	}
	parsed, err := url.Parse(rawRedirectURL)
	if err != nil {
		return "", "", "", err
	}
	if parsed.Scheme != "http" {
		return "", "", "", cloudGateCallbackError()
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" {
		return "", "", "", fmt.Errorf("authorization-code redirect URL must use localhost or 127.0.0.1 so oci-context can receive the authorization code")
	}
	port := parsed.Port()
	if port == "" {
		return "", "", "", fmt.Errorf("authorization-code redirect URL must include a port")
	}
	callbackPath = parsed.EscapedPath()
	if callbackPath == "" {
		callbackPath = "/callback"
		parsed.Path = callbackPath
	}
	return parsed.String(), net.JoinHostPort(host, port), callbackPath, nil
}

func buildAuthorizationURL(request tokenServiceRequest, redirectURL string, state string, challenge string) (string, error) {
	parsed, err := url.Parse(request.AuthorizationEndpoint)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", request.ClientID)
	query.Set("redirect_uri", redirectURL)
	query.Set("scope", request.Scope)
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func isCloudGateCallback(rawRedirectURL string) bool {
	normalized := strings.ToLower(strings.TrimSpace(rawRedirectURL))
	return strings.Contains(normalized, "/cloudgate/v1/oauth2/callback")
}

func cloudGateCallbackError() error {
	return fmt.Errorf("authorization-code redirect URL must be an http loopback URL that oci-context can receive; service callbacks such as https://<host>/cloudgate/v1/oauth2/callback are handled by CloudGate and cannot complete CLI token handoff")
}

func exchangeAuthorizationCode(request tokenServiceRequest, code string, redirectURL string, verifier string) (oauthTokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURL},
		"client_id":     {request.ClientID},
		"code_verifier": {verifier},
	}
	if request.ClientSecret != "" {
		form.Set("client_secret", request.ClientSecret)
	}
	return postOAuthTokenForm(request.TokenEndpoint, form)
}

func startOAuthDeviceAuthorization(request tokenServiceRequest) (oauthDeviceAuthorizationResponse, error) {
	form := url.Values{
		"client_id": {request.ClientID},
		"scope":     {request.Scope},
	}
	resp, err := httpClientForAuthToken.PostForm(request.DeviceEndpoint, form)
	if err != nil {
		return oauthDeviceAuthorizationResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body oauthTokenResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return oauthDeviceAuthorizationResponse{}, fmt.Errorf("device authorization failed: %s", firstNonEmpty(body.ErrorDesc, body.Error, resp.Status))
	}
	var device oauthDeviceAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
		return oauthDeviceAuthorizationResponse{}, err
	}
	return device, nil
}

func pollOAuthDeviceToken(request tokenServiceRequest, device oauthDeviceAuthorizationResponse) (oauthTokenResponse, error) {
	interval := time.Duration(device.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	if device.ExpiresIn <= 0 {
		deadline = time.Now().Add(10 * time.Minute)
	}
	for time.Now().Before(deadline) {
		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {device.DeviceCode},
			"client_id":   {request.ClientID},
		}
		token, status, err := postOAuthTokenFormStatus(request.TokenEndpoint, form)
		if err != nil {
			return oauthTokenResponse{}, err
		}
		if status >= 200 && status < 300 && token.AccessToken != "" {
			return token, nil
		}
		switch token.Error {
		case "authorization_pending":
			time.Sleep(interval)
		case "slow_down":
			interval += 5 * time.Second
			time.Sleep(interval)
		case "access_denied", "expired_token":
			return oauthTokenResponse{}, fmt.Errorf("device authorization failed: %s", token.Error)
		default:
			return oauthTokenResponse{}, fmt.Errorf("token request failed: %s", firstNonEmpty(token.ErrorDesc, token.Error, fmt.Sprintf("HTTP %d", status)))
		}
	}
	return oauthTokenResponse{}, fmt.Errorf("device authorization timed out")
}

func refreshOAuthToken(request tokenServiceRequest, refreshToken string) (oauthTokenResponse, error) {
	if request.TokenEndpoint == "" {
		if err := resolveOAuthLoginEndpoints(&request); err != nil {
			return oauthTokenResponse{}, err
		}
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {request.ClientID},
	}
	if request.ClientSecret != "" {
		form.Set("client_secret", request.ClientSecret)
	}
	return postOAuthTokenForm(request.TokenEndpoint, form)
}

func postOAuthTokenForm(endpoint string, form url.Values) (oauthTokenResponse, error) {
	token, status, err := postOAuthTokenFormStatus(endpoint, form)
	if err != nil {
		return oauthTokenResponse{}, err
	}
	if status < 200 || status >= 300 || token.AccessToken == "" {
		return oauthTokenResponse{}, fmt.Errorf("token request failed: %s", firstNonEmpty(token.ErrorDesc, token.Error, fmt.Sprintf("HTTP %d", status)))
	}
	return token, nil
}

func postOAuthTokenFormStatus(endpoint string, form url.Values) (oauthTokenResponse, int, error) {
	resp, err := httpClientForAuthToken.PostForm(endpoint, form)
	if err != nil {
		return oauthTokenResponse{}, 0, err
	}
	defer resp.Body.Close()
	var token oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return oauthTokenResponse{}, resp.StatusCode, err
	}
	return token, resp.StatusCode, nil
}

func cacheEntryFromToken(request tokenServiceRequest, token oauthTokenResponse) authTokenCacheEntry {
	now := time.Now()
	entry := authTokenCacheEntry{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    firstNonEmpty(token.TokenType, "Bearer"),
		Service:      request.Service,
		Issuer:       request.Issuer,
		ClientID:     request.ClientID,
		Scope:        request.Scope,
		UpdatedAt:    now.Format(time.RFC3339),
	}
	if token.ExpiresIn > 0 {
		entry.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	return entry
}

func randomURLSafeString(byteCount int) (string, error) {
	data := make([]byte, byteCount)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func printAuthToken(cmd *cobra.Command, entry authTokenCacheEntry, format string, ctx config.Context) error {
	switch strings.ToLower(format) {
	case "", "raw":
		_, err := fmt.Fprintln(cmd.OutOrStdout(), entry.AccessToken)
		return err
	case "json":
		payload := struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type,omitempty"`
			ExpiresAt   string `json:"expires_at,omitempty"`
			Service     string `json:"service"`
			Issuer      string `json:"issuer"`
			ClientID    string `json:"client_id"`
			Scope       string `json:"scope"`
			Context     string `json:"context,omitempty"`
			Profile     string `json:"profile,omitempty"`
		}{
			AccessToken: entry.AccessToken,
			TokenType:   entry.TokenType,
			ExpiresAt:   entry.ExpiresAt,
			Service:     entry.Service,
			Issuer:      entry.Issuer,
			ClientID:    entry.ClientID,
			Scope:       entry.Scope,
			Context:     ctx.Name,
			Profile:     ctx.Profile,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
}

func authTokenCachePath(cfg config.Config, service, issuer, clientID, scope string) (string, error) {
	base := filepath.Dir(cfg.Options.SocketPath)
	if base == "." || base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".oci-context")
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{service, issuer, clientID, scope}, "\x00")))
	return filepath.Join(base, "tokens", service+"-"+hex.EncodeToString(sum[:])[:16]+".json"), nil
}

func readAuthTokenCache(path string) (authTokenCacheEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	var entry authTokenCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return authTokenCacheEntry{}, err
	}
	if entry.AccessToken == "" {
		return authTokenCacheEntry{}, fmt.Errorf("cached token is empty")
	}
	return entry, nil
}

func writeAuthTokenCache(path string, entry authTokenCacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func authTokenUsable(entry authTokenCacheEntry) bool {
	if entry.AccessToken == "" {
		return false
	}
	if entry.ExpiresAt == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339, entry.ExpiresAt)
	if err != nil {
		return false
	}
	return expiresAt.After(time.Now().Add(30 * time.Second))
}

func openURLBestEffort(rawURL string) {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
		args = []string{rawURL}
	case "windows":
		command = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", rawURL}
	default:
		command = "xdg-open"
		args = []string{rawURL}
	}
	_ = exec.Command(command, args...).Start()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func envValue(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return os.Getenv(name)
}

func envValues(names ...string) string {
	for _, name := range names {
		if value := envValue(name); value != "" {
			return value
		}
	}
	return ""
}
