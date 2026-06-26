package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	Service     string `json:"service"`
	Issuer      string `json:"issuer"`
	ClientID    string `json:"client_id"`
	Scope       string `json:"scope"`
	UpdatedAt   string `json:"updated_at"`
}

type oauthDiscoveryDocument struct {
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
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

var httpClientForAuthToken = &http.Client{Timeout: 15 * time.Second}

func newAuthTokenCmd(resolvePath authResolvePathFunc, loadTarget authLoadTargetFunc) *cobra.Command {
	var service string
	var audience string
	var issuer string
	var clientID string
	var scope string
	var tokenEndpoint string
	var deviceEndpoint string
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
				Service:        service,
				Audience:       audience,
				Issuer:         issuer,
				ClientID:       clientID,
				Scope:          scope,
				TokenEndpoint:  tokenEndpoint,
				DeviceEndpoint: deviceEndpoint,
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
			if noLogin || commandNoInteractive(cmd) {
				return fmt.Errorf("no cached %s token is available; run `oci-context auth token --service %s` in an interactive shell", request.Service, request.Service)
			}
			entry, err = runOAuthDeviceLogin(cmd, request, !noBrowser)
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
	cmd.Flags().StringVar(&scope, "scope", "", "OAuth scope")
	cmd.Flags().StringVar(&tokenEndpoint, "token-endpoint", "", "OAuth token endpoint")
	cmd.Flags().StringVar(&deviceEndpoint, "device-endpoint", "", "OAuth device authorization endpoint")
	cmd.Flags().StringVar(&format, "format", "raw", "Output format: raw|json")
	cmd.Flags().BoolVar(&noLogin, "no-login", false, "Fail instead of starting a device login")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open the verification URL in a browser")
	return cmd
}

type tokenServiceOptions struct {
	Service        string
	Audience       string
	Issuer         string
	ClientID       string
	Scope          string
	TokenEndpoint  string
	DeviceEndpoint string
}

type tokenServiceRequest struct {
	Service        string
	Type           string
	Issuer         string
	ClientID       string
	Scope          string
	TokenEndpoint  string
	DeviceEndpoint string
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
	serviceType := firstNonEmpty(serviceCfg.Type, config.TokenServiceTypeOAuthDevice)
	if serviceType != config.TokenServiceTypeOAuthDevice {
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
		Scope: firstNonEmpty(
			opts.Scope,
			envValue(serviceCfg.ScopeEnv),
			envValues(serviceCfg.ScopeEnvs...),
			serviceCfg.Scope,
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

func defaultTokenService(name string) (config.TokenService, bool) {
	for _, service := range config.DefaultTokenServices() {
		if service.Name == name {
			return service, true
		}
	}
	return config.TokenService{}, false
}

func runOAuthDeviceLogin(cmd *cobra.Command, request tokenServiceRequest, openBrowser bool) (authTokenCacheEntry, error) {
	if strings.TrimSpace(request.Issuer) == "" {
		return authTokenCacheEntry{}, fmt.Errorf("token service %q issuer is required", request.Service)
	}
	if strings.TrimSpace(request.Scope) == "" {
		return authTokenCacheEntry{}, fmt.Errorf("token service %q scope is required", request.Service)
	}
	if request.TokenEndpoint == "" || request.DeviceEndpoint == "" {
		discovery, err := fetchOAuthDiscovery(request.Issuer)
		if err != nil {
			return authTokenCacheEntry{}, err
		}
		if request.TokenEndpoint == "" {
			request.TokenEndpoint = discovery.TokenEndpoint
		}
		if request.DeviceEndpoint == "" {
			request.DeviceEndpoint = discovery.DeviceAuthorizationEndpoint
		}
	}
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
	now := time.Now()
	entry := authTokenCacheEntry{
		AccessToken: token.AccessToken,
		TokenType:   firstNonEmpty(token.TokenType, "Bearer"),
		Service:     request.Service,
		Issuer:      request.Issuer,
		ClientID:    request.ClientID,
		Scope:       request.Scope,
		UpdatedAt:   now.Format(time.RFC3339),
	}
	if token.ExpiresIn > 0 {
		entry.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	return entry, nil
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
		resp, err := httpClientForAuthToken.PostForm(request.TokenEndpoint, form)
		if err != nil {
			return oauthTokenResponse{}, err
		}
		var token oauthTokenResponse
		err = json.NewDecoder(resp.Body).Decode(&token)
		resp.Body.Close()
		if err != nil {
			return oauthTokenResponse{}, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && token.AccessToken != "" {
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
			return oauthTokenResponse{}, fmt.Errorf("token request failed: %s", firstNonEmpty(token.ErrorDesc, token.Error, resp.Status))
		}
	}
	return oauthTokenResponse{}, fmt.Errorf("device authorization timed out")
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
