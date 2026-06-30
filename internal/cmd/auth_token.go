package cmd

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
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
	var assertion string
	var assertionFile string
	var assertionCommand string
	var clientAssertion string
	var clientAssertionFile string
	var clientAssertionCommand string
	var subjectToken string
	var subjectTokenFile string
	var subjectTokenCommand string
	var subjectTokenType string
	var requestedTokenType string
	var privateKeyFile string
	var keyID string
	var jwtIssuer string
	var jwtSubject string
	var jwtAudience string
	var jwtExpiresIn time.Duration
	var format string
	var noLogin bool
	var noBrowser bool
	var noCache bool
	var offlineAccess bool

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

			serviceName := service
			if serviceName == "" && audience == "" {
				serviceName = cfg.CurrentService
			}
			return runAuthToken(cmd, cfg, ctx, tokenServiceOptions{
				Service:                serviceName,
				Audience:               audience,
				Issuer:                 issuer,
				ClientID:               clientID,
				ClientSecret:           clientSecret,
				Scope:                  scope,
				AuthorizationEndpoint:  authorizationEndpoint,
				TokenEndpoint:          tokenEndpoint,
				DeviceEndpoint:         deviceEndpoint,
				RedirectURL:            redirectURL,
				Flow:                   flow,
				Assertion:              assertion,
				AssertionFile:          assertionFile,
				AssertionCommand:       assertionCommand,
				ClientAssertion:        clientAssertion,
				ClientAssertionFile:    clientAssertionFile,
				ClientAssertionCommand: clientAssertionCommand,
				SubjectToken:           subjectToken,
				SubjectTokenFile:       subjectTokenFile,
				SubjectTokenCommand:    subjectTokenCommand,
				SubjectTokenType:       subjectTokenType,
				RequestedTokenType:     requestedTokenType,
				PrivateKeyFile:         privateKeyFile,
				KeyID:                  keyID,
				JWTIssuer:              jwtIssuer,
				JWTSubject:             jwtSubject,
				JWTAudience:            jwtAudience,
				JWTExpiresIn:           jwtExpiresIn,
				OfflineAccess:          offlineAccess,
			}, authTokenRunOptions{
				Format:    format,
				NoLogin:   noLogin,
				NoBrowser: noBrowser,
				NoCache:   noCache,
			})
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "Token service name (default current_service else obp)")
	cmd.Flags().StringVar(&audience, "audience", "", "Deprecated alias for --service")
	cmd.Flags().StringVar(&issuer, "issuer", "", "OAuth issuer URL")
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth public client id")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth client secret for confidential clients; prefer env-backed config over this flag")
	cmd.Flags().StringVar(&scope, "scope", "", "OAuth scope")
	cmd.Flags().StringVar(&authorizationEndpoint, "authorization-endpoint", "", "OAuth authorization endpoint")
	cmd.Flags().StringVar(&tokenEndpoint, "token-endpoint", "", "OAuth token endpoint")
	cmd.Flags().StringVar(&deviceEndpoint, "device-endpoint", "", "OAuth device authorization endpoint")
	cmd.Flags().StringVar(&redirectURL, "redirect-url", "", "OAuth authorization-code loopback redirect URL")
	cmd.Flags().StringVar(&flow, "flow", "auto", "OAuth flow: auto|authorization-code|device|client-credentials|jwt-client-credentials|jwt-bearer|token-exchange")
	cmd.Flags().StringVar(&assertion, "assertion", "", "JWT bearer assertion value; prefer --assertion-file or --assertion-command")
	cmd.Flags().StringVar(&assertionFile, "assertion-file", "", "File containing a JWT bearer assertion")
	cmd.Flags().StringVar(&assertionCommand, "assertion-command", "", "Command that prints a JWT bearer assertion")
	cmd.Flags().StringVar(&clientAssertion, "client-assertion", "", "JWT client assertion value; prefer --client-assertion-file or --client-assertion-command")
	cmd.Flags().StringVar(&clientAssertionFile, "client-assertion-file", "", "File containing a JWT client assertion")
	cmd.Flags().StringVar(&clientAssertionCommand, "client-assertion-command", "", "Command that prints a JWT client assertion")
	cmd.Flags().StringVar(&subjectToken, "subject-token", "", "OAuth token-exchange subject token value")
	cmd.Flags().StringVar(&subjectTokenFile, "subject-token-file", "", "File containing an OAuth token-exchange subject token")
	cmd.Flags().StringVar(&subjectTokenCommand, "subject-token-command", "", "Command that prints an OAuth token-exchange subject token")
	cmd.Flags().StringVar(&subjectTokenType, "subject-token-type", "", "OAuth token-exchange subject token type")
	cmd.Flags().StringVar(&requestedTokenType, "requested-token-type", "", "OAuth token-exchange requested token type")
	cmd.Flags().StringVar(&privateKeyFile, "private-key-file", "", "PEM private key file for local JWT signing")
	cmd.Flags().StringVar(&keyID, "key-id", "", "JWT key id header for local signing")
	cmd.Flags().StringVar(&jwtIssuer, "jwt-issuer", "", "JWT issuer claim for local signing")
	cmd.Flags().StringVar(&jwtSubject, "jwt-subject", "", "JWT subject claim for local signing")
	cmd.Flags().StringVar(&jwtAudience, "jwt-audience", "", "JWT audience claim for local signing; defaults to token endpoint")
	cmd.Flags().DurationVar(&jwtExpiresIn, "jwt-expires-in", 5*time.Minute, "JWT lifetime for local signing")
	cmd.Flags().StringVar(&format, "format", "raw", "Output format: raw|json")
	cmd.Flags().BoolVar(&noLogin, "no-login", false, "Fail instead of starting an interactive login")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open the verification URL in a browser")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Bypass cached OAuth tokens and do not update the token cache")
	cmd.Flags().BoolVar(&offlineAccess, "offline-access", false, "Request an OAuth refresh token with the offline_access scope")
	return cmd
}

type authTokenRunOptions struct {
	Format      string
	NoLogin     bool
	NoBrowser   bool
	NoCache     bool
	LoginStatus bool
}

type tokenServiceOptions struct {
	Service                string
	Audience               string
	Issuer                 string
	ClientID               string
	ClientSecret           string
	Scope                  string
	AuthorizationEndpoint  string
	TokenEndpoint          string
	DeviceEndpoint         string
	RedirectURL            string
	Flow                   string
	Assertion              string
	AssertionFile          string
	AssertionCommand       string
	ClientAssertion        string
	ClientAssertionFile    string
	ClientAssertionCommand string
	SubjectToken           string
	SubjectTokenFile       string
	SubjectTokenCommand    string
	SubjectTokenType       string
	RequestedTokenType     string
	PrivateKeyFile         string
	KeyID                  string
	JWTIssuer              string
	JWTSubject             string
	JWTAudience            string
	JWTExpiresIn           time.Duration
	OfflineAccess          bool
}

func runAuthToken(cmd *cobra.Command, cfg config.Config, ctx config.Context, opts tokenServiceOptions, runOpts authTokenRunOptions) error {
	request, err := resolveTokenServiceRequest(cfg, opts)
	if err != nil {
		return err
	}
	cachePath, err := authTokenCachePath(cfg, request.Service, request.Issuer, request.ClientID, request.Scope)
	if err != nil {
		return err
	}
	var entry authTokenCacheEntry
	if !runOpts.NoCache {
		entry, err = readAuthTokenCache(cachePath)
		if err == nil && authTokenUsable(entry) {
			return finishAuthTokenRun(cmd, entry, runOpts, ctx, "Token service %s is already logged in\n")
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
				return finishAuthTokenRun(cmd, entry, runOpts, ctx, "Refreshed token service %s\n")
			}
		}
	}
	if isNonInteractiveOAuthFlow(normalizeOAuthFlow(request.Flow)) {
		entry, err = runOAuthNonInteractiveTokenFlow(request)
		if err != nil {
			return err
		}
		if !runOpts.NoCache {
			if err := writeAuthTokenCache(cachePath, entry); err != nil {
				return err
			}
		}
		return finishAuthTokenRun(cmd, entry, runOpts, ctx, "Logged in token service %s\n")
	}
	if runOpts.NoLogin || commandNoInteractive(cmd) {
		return fmt.Errorf("no cached %s token is available; run `oci-context auth token --service %s` in an interactive shell", request.Service, request.Service)
	}
	entry, err = runOAuthLogin(cmd, request, !runOpts.NoBrowser)
	if err != nil {
		return err
	}
	if !runOpts.NoCache {
		if err := writeAuthTokenCache(cachePath, entry); err != nil {
			return err
		}
	}
	return finishAuthTokenRun(cmd, entry, runOpts, ctx, "Logged in token service %s\n")
}

func finishAuthTokenRun(cmd *cobra.Command, entry authTokenCacheEntry, runOpts authTokenRunOptions, ctx config.Context, statusFormat string) error {
	if runOpts.LoginStatus {
		fmt.Fprintf(cmd.OutOrStdout(), statusFormat, entry.Service)
		return nil
	}
	format := firstNonEmpty(runOpts.Format, "raw")
	return printAuthToken(cmd, entry, format, ctx)
}

type tokenServiceRequest struct {
	Service                string
	Type                   string
	Issuer                 string
	ClientID               string
	ClientSecret           string
	Scope                  string
	AuthorizationEndpoint  string
	TokenEndpoint          string
	DeviceEndpoint         string
	RedirectURL            string
	Flow                   string
	Assertion              string
	AssertionFile          string
	AssertionCommand       string
	ClientAssertion        string
	ClientAssertionFile    string
	ClientAssertionCommand string
	SubjectToken           string
	SubjectTokenFile       string
	SubjectTokenCommand    string
	SubjectTokenType       string
	RequestedTokenType     string
	PrivateKeyFile         string
	KeyID                  string
	JWTIssuer              string
	JWTSubject             string
	JWTAudience            string
	JWTExpiresIn           time.Duration
	OfflineAccess          bool
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
		Flow:          firstNonEmpty(opts.Flow, serviceCfg.Flow, "auto"),
		OfflineAccess: opts.OfflineAccess || serviceCfg.OfflineAccess,
		Assertion: firstNonEmpty(
			opts.Assertion,
			envValue(serviceCfg.AssertionEnv),
			envValues(serviceCfg.AssertionEnvs...),
			serviceCfg.Assertion,
		),
		AssertionFile: firstNonEmpty(
			opts.AssertionFile,
			envValue(serviceCfg.AssertionFileEnv),
			serviceCfg.AssertionFile,
		),
		AssertionCommand: firstNonEmpty(
			opts.AssertionCommand,
			envValue(serviceCfg.AssertionCommandEnv),
			serviceCfg.AssertionCommand,
		),
		ClientAssertion: firstNonEmpty(
			opts.ClientAssertion,
			envValue(serviceCfg.ClientAssertionEnv),
			serviceCfg.ClientAssertion,
		),
		ClientAssertionFile: firstNonEmpty(
			opts.ClientAssertionFile,
			envValue(serviceCfg.ClientAssertionFileEnv),
			serviceCfg.ClientAssertionFile,
		),
		ClientAssertionCommand: firstNonEmpty(
			opts.ClientAssertionCommand,
			envValue(serviceCfg.ClientAssertionCommandEnv),
			serviceCfg.ClientAssertionCommand,
		),
		SubjectToken: firstNonEmpty(
			opts.SubjectToken,
			envValue(serviceCfg.SubjectTokenEnv),
			serviceCfg.SubjectToken,
		),
		SubjectTokenFile: firstNonEmpty(
			opts.SubjectTokenFile,
			envValue(serviceCfg.SubjectTokenFileEnv),
			serviceCfg.SubjectTokenFile,
		),
		SubjectTokenCommand: firstNonEmpty(
			opts.SubjectTokenCommand,
			envValue(serviceCfg.SubjectTokenCommandEnv),
			serviceCfg.SubjectTokenCommand,
		),
		SubjectTokenType:   firstNonEmpty(opts.SubjectTokenType, serviceCfg.SubjectTokenType),
		RequestedTokenType: firstNonEmpty(opts.RequestedTokenType, serviceCfg.RequestedTokenType),
		PrivateKeyFile: firstNonEmpty(
			opts.PrivateKeyFile,
			envValue(serviceCfg.PrivateKeyFileEnv),
			serviceCfg.PrivateKeyFile,
		),
		KeyID: firstNonEmpty(
			opts.KeyID,
			envValue(serviceCfg.KeyIDEnv),
			serviceCfg.KeyID,
		),
		JWTIssuer: firstNonEmpty(
			opts.JWTIssuer,
			envValue(serviceCfg.JWTIssuerEnv),
			serviceCfg.JWTIssuer,
		),
		JWTSubject: firstNonEmpty(
			opts.JWTSubject,
			envValue(serviceCfg.JWTSubjectEnv),
			serviceCfg.JWTSubject,
		),
		JWTAudience: firstNonEmpty(
			opts.JWTAudience,
			envValue(serviceCfg.JWTAudienceEnv),
			serviceCfg.JWTAudience,
		),
		JWTExpiresIn: opts.JWTExpiresIn,
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
	case "client_credentials", "client-credentials", "client":
		return "client-credentials"
	case "jwt_client_credentials", "jwt-client-credentials", "client-credentials-jwt", "client_assertion", "client-assertion":
		return "jwt-client-credentials"
	case "jwt_bearer", "jwt-bearer", "assertion", "user-jwt", "user_jwt":
		return "jwt-bearer"
	case "token_exchange", "token-exchange", "workload", "workload-identity", "workload_identity", "federated-workload", "federated_workload":
		return "token-exchange"
	default:
		return strings.ToLower(strings.TrimSpace(flow))
	}
}

func isNonInteractiveOAuthFlow(flow string) bool {
	switch flow {
	case "client-credentials", "jwt-client-credentials", "jwt-bearer", "token-exchange":
		return true
	default:
		return false
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

func runOAuthNonInteractiveTokenFlow(request tokenServiceRequest) (authTokenCacheEntry, error) {
	if strings.TrimSpace(request.Scope) == "" {
		return authTokenCacheEntry{}, fmt.Errorf("token service %q scope is required", request.Service)
	}
	if err := resolveOAuthTokenEndpoint(&request); err != nil {
		return authTokenCacheEntry{}, err
	}

	var token oauthTokenResponse
	var err error
	switch normalizeOAuthFlow(request.Flow) {
	case "client-credentials":
		token, err = exchangeClientCredentials(request, "")
	case "jwt-client-credentials":
		assertion, assertionErr := resolveClientAssertion(request)
		if assertionErr != nil {
			return authTokenCacheEntry{}, assertionErr
		}
		token, err = exchangeClientCredentials(request, assertion)
		if shouldRetryIDCSClientAssertionAudience(request, err) {
			retryRequest := request
			retryRequest.JWTAudience = "https://identity.oraclecloud.com/"
			assertion, assertionErr = resolveClientAssertion(retryRequest)
			if assertionErr != nil {
				return authTokenCacheEntry{}, assertionErr
			}
			token, err = exchangeClientCredentials(retryRequest, assertion)
		}
	case "jwt-bearer":
		assertion, assertionErr := resolveBearerAssertion(request)
		if assertionErr != nil {
			return authTokenCacheEntry{}, assertionErr
		}
		token, err = exchangeJWTBearerAssertion(request, assertion)
	case "token-exchange":
		subjectToken, tokenErr := resolveSubjectToken(request)
		if tokenErr != nil {
			return authTokenCacheEntry{}, tokenErr
		}
		token, err = exchangeSubjectToken(request, subjectToken)
	default:
		return authTokenCacheEntry{}, fmt.Errorf("unsupported non-interactive OAuth flow %q", request.Flow)
	}
	if err != nil {
		return authTokenCacheEntry{}, err
	}
	return cacheEntryFromToken(request, token), nil
}

func shouldRetryIDCSClientAssertionAudience(request tokenServiceRequest, err error) bool {
	if err == nil {
		return false
	}
	if strings.TrimSpace(request.JWTAudience) != "" {
		return false
	}
	if strings.TrimSpace(request.PrivateKeyFile) == "" || hasClientAssertionMaterial(request) {
		return false
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid audience in client assertion") {
		return false
	}
	return isOCIIdentityDomainURL(request.TokenEndpoint) || isOCIIdentityDomainURL(request.Issuer)
}

func hasClientAssertionMaterial(request tokenServiceRequest) bool {
	return strings.TrimSpace(request.ClientAssertion) != "" ||
		strings.TrimSpace(request.ClientAssertionFile) != "" ||
		strings.TrimSpace(request.ClientAssertionCommand) != ""
}

func isOCIIdentityDomainURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return strings.HasSuffix(host, ".identity.oraclecloud.com")
}

func resolveOAuthTokenEndpoint(request *tokenServiceRequest) error {
	if request.TokenEndpoint != "" {
		return nil
	}
	if strings.TrimSpace(request.Issuer) == "" {
		return fmt.Errorf("token service %q issuer or token endpoint is required", request.Service)
	}
	discovery, err := fetchOAuthDiscovery(request.Issuer)
	if err != nil {
		return err
	}
	if discovery.TokenEndpoint == "" {
		return fmt.Errorf("issuer discovery did not provide token endpoint")
	}
	request.TokenEndpoint = discovery.TokenEndpoint
	return nil
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
	query.Set("scope", authorizationCodeScope(request.Scope, request.OfflineAccess))
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func authorizationCodeScope(scope string, offlineAccess bool) string {
	fields := strings.Fields(scope)
	if !offlineAccess {
		return strings.Join(fields, " ")
	}
	for _, field := range fields {
		if field == "offline_access" {
			return strings.Join(fields, " ")
		}
	}
	return strings.TrimSpace(strings.Join(append(fields, "offline_access"), " "))
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

func exchangeClientCredentials(request tokenServiceRequest, clientAssertion string) (oauthTokenResponse, error) {
	form := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {request.ClientID},
		"scope":      {request.Scope},
	}
	if clientAssertion != "" {
		form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
		form.Set("client_assertion", clientAssertion)
	} else if request.ClientSecret != "" {
		form.Set("client_secret", request.ClientSecret)
	}
	return postOAuthTokenForm(request.TokenEndpoint, form)
}

func exchangeJWTBearerAssertion(request tokenServiceRequest, assertion string) (oauthTokenResponse, error) {
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
		"client_id":  {request.ClientID},
		"scope":      {request.Scope},
	}
	if request.ClientSecret != "" {
		form.Set("client_secret", request.ClientSecret)
	}
	return postOAuthTokenForm(request.TokenEndpoint, form)
}

func exchangeSubjectToken(request tokenServiceRequest, subjectToken string) (oauthTokenResponse, error) {
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"client_id":  {request.ClientID},
		"scope":      {request.Scope},
		"subject_token_type": {firstNonEmpty(
			request.SubjectTokenType,
			"urn:ietf:params:oauth:token-type:jwt",
		)},
		"subject_token": {subjectToken},
	}
	if request.RequestedTokenType != "" {
		form.Set("requested_token_type", request.RequestedTokenType)
	}
	if request.ClientSecret != "" {
		form.Set("client_secret", request.ClientSecret)
	}
	return postOAuthTokenForm(request.TokenEndpoint, form)
}

func resolveBearerAssertion(request tokenServiceRequest) (string, error) {
	assertion, err := resolveSecretMaterial(secretMaterialSource{
		Inline:  request.Assertion,
		File:    request.AssertionFile,
		Command: request.AssertionCommand,
		Label:   "JWT bearer assertion",
	})
	if err != nil {
		return "", err
	}
	if assertion != "" {
		return assertion, nil
	}
	return signBearerAssertion(request)
}

func resolveClientAssertion(request tokenServiceRequest) (string, error) {
	assertion, err := resolveSecretMaterial(secretMaterialSource{
		Inline:  request.ClientAssertion,
		File:    request.ClientAssertionFile,
		Command: request.ClientAssertionCommand,
		Label:   "JWT client assertion",
	})
	if err != nil {
		return "", err
	}
	if assertion != "" {
		return assertion, nil
	}
	return signClientAssertion(request)
}

func resolveSubjectToken(request tokenServiceRequest) (string, error) {
	token, err := resolveSecretMaterial(secretMaterialSource{
		Inline:  request.SubjectToken,
		File:    request.SubjectTokenFile,
		Command: request.SubjectTokenCommand,
		Label:   "subject token",
	})
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("subject token is required for token-exchange flow; set --subject-token-file or --subject-token-command")
	}
	return token, nil
}

type secretMaterialSource struct {
	Inline  string
	File    string
	Command string
	Label   string
}

func resolveSecretMaterial(source secretMaterialSource) (string, error) {
	if strings.TrimSpace(source.Inline) != "" {
		return strings.TrimSpace(source.Inline), nil
	}
	if strings.TrimSpace(source.File) != "" {
		data, err := os.ReadFile(source.File)
		if err != nil {
			return "", fmt.Errorf("read %s file: %w", source.Label, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if strings.TrimSpace(source.Command) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/bin/sh", "-c", source.Command).Output()
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s command timed out", source.Label)
		}
		if err != nil {
			return "", fmt.Errorf("%s command failed: %w", source.Label, err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	return "", nil
}

func signClientAssertion(request tokenServiceRequest) (string, error) {
	if request.PrivateKeyFile == "" {
		return "", fmt.Errorf("JWT client assertion is required for jwt-client-credentials flow; set --client-assertion-file, --client-assertion-command, or --private-key-file")
	}
	return signOAuthJWT(request, oauthJWTClaims{
		Issuer:   request.ClientID,
		Subject:  request.ClientID,
		Audience: firstNonEmpty(request.JWTAudience, request.TokenEndpoint),
	})
}

func signBearerAssertion(request tokenServiceRequest) (string, error) {
	if request.PrivateKeyFile == "" {
		return "", fmt.Errorf("JWT bearer assertion is required for jwt-bearer flow; set --assertion-file, --assertion-command, or --private-key-file")
	}
	issuer := firstNonEmpty(request.JWTIssuer, request.ClientID)
	subject := request.JWTSubject
	if subject == "" {
		return "", fmt.Errorf("jwt-bearer local signing requires --jwt-subject")
	}
	return signOAuthJWT(request, oauthJWTClaims{
		Issuer:   issuer,
		Subject:  subject,
		Audience: firstNonEmpty(request.JWTAudience, request.TokenEndpoint),
	})
}

type oauthJWTClaims struct {
	Issuer   string
	Subject  string
	Audience string
}

func signOAuthJWT(request tokenServiceRequest, input oauthJWTClaims) (string, error) {
	if input.Issuer == "" || input.Subject == "" || input.Audience == "" {
		return "", fmt.Errorf("JWT local signing requires issuer, subject, and audience")
	}
	expiresIn := request.JWTExpiresIn
	if expiresIn <= 0 {
		expiresIn = 5 * time.Minute
	}
	key, err := readPrivateKey(request.PrivateKeyFile)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	jti, err := randomURLSafeString(16)
	if err != nil {
		return "", err
	}
	header := map[string]string{
		"typ": "JWT",
		"alg": jwtSigningAlg(key),
	}
	if request.KeyID != "" {
		header["kid"] = request.KeyID
	}
	claims := map[string]any{
		"iss": input.Issuer,
		"sub": input.Subject,
		"aud": input.Audience,
		"iat": now.Unix(),
		"exp": now.Add(expiresIn).Unix(),
		"jti": jti,
	}
	return signJWT(header, claims, key)
}

func readPrivateKey(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("private key file does not contain PEM data")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("private key file must contain a PKCS#8, PKCS#1 RSA, or EC private key")
}

func jwtSigningAlg(key any) string {
	switch key.(type) {
	case *ecdsa.PrivateKey:
		return "ES256"
	default:
		return "RS256"
	}
}

func signJWT(header map[string]string, claims map[string]any, key any) (string, error) {
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	sum := sha256.Sum256([]byte(payload))

	var signature []byte
	switch privateKey := key.(type) {
	case *rsa.PrivateKey:
		signature, err = rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, sum[:])
	case *ecdsa.PrivateKey:
		signature, err = signECDSAJWT(privateKey, sum[:])
	default:
		return "", fmt.Errorf("unsupported private key type %T", key)
	}
	if err != nil {
		return "", err
	}
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func signECDSAJWT(key *ecdsa.PrivateKey, digest []byte) ([]byte, error) {
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("only P-256 EC private keys are supported for JWT signing")
	}
	r, s, err := ecdsa.Sign(rand.Reader, key, digest)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 64)
	writeBigEndianFixed(out[:32], r)
	writeBigEndianFixed(out[32:], s)
	return out, nil
}

func writeBigEndianFixed(dst []byte, v *big.Int) {
	for i := range dst {
		dst[i] = 0
	}
	bytes := v.Bytes()
	copy(dst[len(dst)-len(bytes):], bytes)
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
