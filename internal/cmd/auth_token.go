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
	Issuer      string `json:"issuer"`
	ClientID    string `json:"client_id"`
	Scope       string `json:"scope"`
	UpdatedAt   string `json:"updated_at"`
}

type obpDiscoveryDocument struct {
	TokenEndpoint               string `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

type obpDeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type obpTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

var httpClientForAuthToken = &http.Client{Timeout: 15 * time.Second}

func newAuthTokenCmd(resolvePath authResolvePathFunc, loadTarget authLoadTargetFunc) *cobra.Command {
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
			if strings.TrimSpace(audience) != "obp" {
				return fmt.Errorf("unsupported token audience %q", audience)
			}

			request := resolveOBPTokenRequest(issuer, clientID, scope, tokenEndpoint, deviceEndpoint)
			cachePath, err := authTokenCachePath(cfg, audience, request.Issuer, request.ClientID, request.Scope)
			if err != nil {
				return err
			}
			entry, err := readAuthTokenCache(cachePath)
			if err == nil && authTokenUsable(entry) {
				return printAuthToken(cmd, entry, format, ctx)
			}
			if noLogin || commandNoInteractive(cmd) {
				return fmt.Errorf("no cached %s token is available; run `oci-context auth token --audience %s` in an interactive shell", audience, audience)
			}
			entry, err = runOBPDeviceLogin(cmd, request, !noBrowser)
			if err != nil {
				return err
			}
			if err := writeAuthTokenCache(cachePath, entry); err != nil {
				return err
			}
			return printAuthToken(cmd, entry, format, ctx)
		},
	}
	cmd.Flags().StringVar(&audience, "audience", "obp", "Token audience: obp")
	cmd.Flags().StringVar(&issuer, "issuer", "", "OBP OAuth issuer URL")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OBP OAuth public client id")
	cmd.Flags().StringVar(&scope, "scope", "", "OBP OAuth scope")
	cmd.Flags().StringVar(&tokenEndpoint, "token-endpoint", "", "OAuth token endpoint")
	cmd.Flags().StringVar(&deviceEndpoint, "device-endpoint", "", "OAuth device authorization endpoint")
	cmd.Flags().StringVar(&format, "format", "raw", "Output format: raw|json")
	cmd.Flags().BoolVar(&noLogin, "no-login", false, "Fail instead of starting a device login")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open the verification URL in a browser")
	return cmd
}

type obpTokenRequest struct {
	Issuer         string
	ClientID       string
	Scope          string
	TokenEndpoint  string
	DeviceEndpoint string
}

func resolveOBPTokenRequest(issuer, clientID, scope, tokenEndpoint, deviceEndpoint string) obpTokenRequest {
	return obpTokenRequest{
		Issuer: firstNonEmpty(
			issuer,
			os.Getenv("OCHAIN_OBP_AUTH_ISSUER"),
			os.Getenv("OBP_OAUTH2_ISSUER"),
			os.Getenv("OBP_OAUTH2_IDCS_ISSUER"),
		),
		ClientID: firstNonEmpty(clientID, os.Getenv("OCHAIN_OBP_AUTH_CLIENT_ID"), "obp"),
		Scope: firstNonEmpty(
			scope,
			os.Getenv("OCHAIN_OBP_AUTH_SCOPE"),
			os.Getenv("OCHAIN_OBP_PLATFORM"),
			os.Getenv("OBP_PLATFORM"),
		),
		TokenEndpoint: firstNonEmpty(
			tokenEndpoint,
			os.Getenv("OCHAIN_OBP_AUTH_TOKEN_ENDPOINT"),
			os.Getenv("OBP_OAUTH2_TOKEN_ENDPOINT"),
		),
		DeviceEndpoint: firstNonEmpty(deviceEndpoint, os.Getenv("OCHAIN_OBP_AUTH_DEVICE_ENDPOINT")),
	}
}

func runOBPDeviceLogin(cmd *cobra.Command, request obpTokenRequest, openBrowser bool) (authTokenCacheEntry, error) {
	if strings.TrimSpace(request.Issuer) == "" {
		return authTokenCacheEntry{}, fmt.Errorf("OBP OAuth issuer is required")
	}
	if strings.TrimSpace(request.Scope) == "" {
		return authTokenCacheEntry{}, fmt.Errorf("OBP OAuth scope is required")
	}
	if request.TokenEndpoint == "" || request.DeviceEndpoint == "" {
		discovery, err := fetchOBPDiscovery(request.Issuer)
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

	device, err := startOBPDeviceAuthorization(request)
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
	token, err := pollOBPDeviceToken(request, device)
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	now := time.Now()
	entry := authTokenCacheEntry{
		AccessToken: token.AccessToken,
		TokenType:   firstNonEmpty(token.TokenType, "Bearer"),
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

func fetchOBPDiscovery(issuer string) (obpDiscoveryDocument, error) {
	endpoint := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	resp, err := httpClientForAuthToken.Get(endpoint)
	if err != nil {
		return obpDiscoveryDocument{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return obpDiscoveryDocument{}, fmt.Errorf("issuer discovery failed with HTTP %d", resp.StatusCode)
	}
	var doc obpDiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return obpDiscoveryDocument{}, err
	}
	return doc, nil
}

func startOBPDeviceAuthorization(request obpTokenRequest) (obpDeviceAuthorizationResponse, error) {
	form := url.Values{
		"client_id": {request.ClientID},
		"scope":     {request.Scope},
	}
	resp, err := httpClientForAuthToken.PostForm(request.DeviceEndpoint, form)
	if err != nil {
		return obpDeviceAuthorizationResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body obpTokenResponse
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return obpDeviceAuthorizationResponse{}, fmt.Errorf("device authorization failed: %s", firstNonEmpty(body.ErrorDesc, body.Error, resp.Status))
	}
	var device obpDeviceAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&device); err != nil {
		return obpDeviceAuthorizationResponse{}, err
	}
	return device, nil
}

func pollOBPDeviceToken(request obpTokenRequest, device obpDeviceAuthorizationResponse) (obpTokenResponse, error) {
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
			return obpTokenResponse{}, err
		}
		var token obpTokenResponse
		err = json.NewDecoder(resp.Body).Decode(&token)
		resp.Body.Close()
		if err != nil {
			return obpTokenResponse{}, err
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
			return obpTokenResponse{}, fmt.Errorf("device authorization failed: %s", token.Error)
		default:
			return obpTokenResponse{}, fmt.Errorf("token request failed: %s", firstNonEmpty(token.ErrorDesc, token.Error, resp.Status))
		}
	}
	return obpTokenResponse{}, fmt.Errorf("device authorization timed out")
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
			Audience    string `json:"audience"`
			Issuer      string `json:"issuer"`
			ClientID    string `json:"client_id"`
			Scope       string `json:"scope"`
			Context     string `json:"context,omitempty"`
			Profile     string `json:"profile,omitempty"`
		}{
			AccessToken: entry.AccessToken,
			TokenType:   entry.TokenType,
			ExpiresAt:   entry.ExpiresAt,
			Audience:    "obp",
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

func authTokenCachePath(cfg config.Config, audience, issuer, clientID, scope string) (string, error) {
	base := filepath.Dir(cfg.Options.SocketPath)
	if base == "." || base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".oci-context")
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{audience, issuer, clientID, scope}, "\x00")))
	return filepath.Join(base, "tokens", audience+"-"+hex.EncodeToString(sum[:])[:16]+".json"), nil
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
