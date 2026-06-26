package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"gopkg.in/yaml.v3"
)

// Config represents the persisted state for oci-context.
type Config struct {
	Options        Options        `yaml:"options" json:"options"`
	Contexts       []Context      `yaml:"contexts" json:"contexts"`
	TokenServices  []TokenService `yaml:"token_services,omitempty" json:"token_services,omitempty"`
	CurrentContext string         `yaml:"current_context" json:"current_context"`
}

// Options holds global settings.
type Options struct {
	OCIConfigPath  string   `yaml:"oci_config_path" json:"oci_config_path"`
	SocketPath     string   `yaml:"socket_path" json:"socket_path"`
	DefaultProfile string   `yaml:"default_profile" json:"default_profile"`
	DaemonContexts []string `yaml:"daemon_contexts,omitempty" json:"daemon_contexts,omitempty"`
}

// Context describes a selectable OCI context.
type Context struct {
	Name            string `yaml:"name" json:"name"`
	Profile         string `yaml:"profile" json:"profile"`
	AuthMethod      string `yaml:"auth_method,omitempty" json:"auth_method,omitempty"`
	TenancyOCID     string `yaml:"tenancy_ocid" json:"tenancy_ocid"`
	CompartmentOCID string `yaml:"compartment_ocid" json:"compartment_ocid"`
	Region          string `yaml:"region" json:"region"`
	User            string `yaml:"user" json:"user"`
	Notes           string `yaml:"notes" json:"notes"`
}

// TokenService describes a named token provider for command handoffs.
type TokenService struct {
	Name                      string   `yaml:"name" json:"name"`
	Type                      string   `yaml:"type,omitempty" json:"type,omitempty"`
	Issuer                    string   `yaml:"issuer,omitempty" json:"issuer,omitempty"`
	IssuerEnv                 string   `yaml:"issuer_env,omitempty" json:"issuer_env,omitempty"`
	IssuerEnvs                []string `yaml:"issuer_envs,omitempty" json:"issuer_envs,omitempty"`
	ClientID                  string   `yaml:"client_id,omitempty" json:"client_id,omitempty"`
	ClientIDEnv               string   `yaml:"client_id_env,omitempty" json:"client_id_env,omitempty"`
	ClientIDEnvs              []string `yaml:"client_id_envs,omitempty" json:"client_id_envs,omitempty"`
	ClientSecret              string   `yaml:"client_secret,omitempty" json:"client_secret,omitempty"`
	ClientSecretEnv           string   `yaml:"client_secret_env,omitempty" json:"client_secret_env,omitempty"`
	ClientSecretEnvs          []string `yaml:"client_secret_envs,omitempty" json:"client_secret_envs,omitempty"`
	Scope                     string   `yaml:"scope,omitempty" json:"scope,omitempty"`
	ScopeEnv                  string   `yaml:"scope_env,omitempty" json:"scope_env,omitempty"`
	ScopeEnvs                 []string `yaml:"scope_envs,omitempty" json:"scope_envs,omitempty"`
	AuthorizationEndpoint     string   `yaml:"authorization_endpoint,omitempty" json:"authorization_endpoint,omitempty"`
	AuthorizationEndpointEnv  string   `yaml:"authorization_endpoint_env,omitempty" json:"authorization_endpoint_env,omitempty"`
	AuthorizationEndpointEnvs []string `yaml:"authorization_endpoint_envs,omitempty" json:"authorization_endpoint_envs,omitempty"`
	TokenEndpoint             string   `yaml:"token_endpoint,omitempty" json:"token_endpoint,omitempty"`
	TokenEndpointEnv          string   `yaml:"token_endpoint_env,omitempty" json:"token_endpoint_env,omitempty"`
	TokenEndpointEnvs         []string `yaml:"token_endpoint_envs,omitempty" json:"token_endpoint_envs,omitempty"`
	DeviceEndpoint            string   `yaml:"device_endpoint,omitempty" json:"device_endpoint,omitempty"`
	DeviceEnv                 string   `yaml:"device_endpoint_env,omitempty" json:"device_endpoint_env,omitempty"`
	DeviceEnvs                []string `yaml:"device_endpoint_envs,omitempty" json:"device_endpoint_envs,omitempty"`
	RedirectURL               string   `yaml:"redirect_url,omitempty" json:"redirect_url,omitempty"`
	RedirectURLEnv            string   `yaml:"redirect_url_env,omitempty" json:"redirect_url_env,omitempty"`
	RedirectURLEnvs           []string `yaml:"redirect_url_envs,omitempty" json:"redirect_url_envs,omitempty"`
	Flow                      string   `yaml:"flow,omitempty" json:"flow,omitempty"`
}

const (
	TokenServiceTypeOAuth       = "oauth"
	TokenServiceTypeOAuthDevice = "oauth_device"
)

var (
	ErrContextNotFound = errors.New("context not found")
	ErrDuplicateName   = errors.New("context name already exists")
)

const (
	AuthMethodAPIKey            = "api_key"
	AuthMethodSecurityToken     = "security_token"
	AuthMethodInstancePrincipal = "instance_principal"
	AuthMethodResourcePrincipal = "resource_principal"
	AuthMethodInstanceOBOUser   = "instance_obo_user"
	AuthMethodOKEWorkload       = "oke_workload_identity"
)

func ValidAuthMethods() []string {
	return []string{
		AuthMethodAPIKey,
		AuthMethodSecurityToken,
		AuthMethodInstancePrincipal,
		AuthMethodResourcePrincipal,
		AuthMethodInstanceOBOUser,
		AuthMethodOKEWorkload,
	}
}

func NormalizeAuthMethod(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return AuthMethodAPIKey
	}
	return v
}

func IsValidAuthMethod(v string) bool {
	v = NormalizeAuthMethod(v)
	for _, m := range ValidAuthMethods() {
		if v == m {
			return true
		}
	}
	return false
}

// DefaultConfig returns the initial config.
func DefaultConfig(home string) Config {
	return Config{
		Options: Options{
			OCIConfigPath:  filepath.Join(home, ".oci", "config"),
			SocketPath:     filepath.Join(home, ".oci-context", "daemon.sock"),
			DefaultProfile: "",
			DaemonContexts: []string{},
		},
		Contexts:       []Context{},
		TokenServices:  DefaultTokenServices(),
		CurrentContext: "",
	}
}

func DefaultTokenServices() []TokenService {
	return []TokenService{DefaultOBPTokenService()}
}

func DefaultOBPTokenService() TokenService {
	return TokenService{
		Name:        "obp",
		Type:        TokenServiceTypeOAuth,
		ClientID:    "obp",
		ClientIDEnv: "OCHAIN_OBP_AUTH_CLIENT_ID",
		ClientSecretEnvs: []string{
			"OCHAIN_OBP_AUTH_CLIENT_SECRET",
			"OBP_OAUTH2_CLIENT_SECRET",
			"OBP_OAUTH2_IDCS_CLIENT_SECRET",
		},
		IssuerEnvs: []string{
			"OCHAIN_OBP_AUTH_ISSUER",
			"OBP_OAUTH2_ISSUER",
			"OBP_OAUTH2_IDCS_ISSUER",
		},
		ScopeEnvs: []string{
			"OCHAIN_OBP_AUTH_SCOPE",
			"OCHAIN_OBP_PLATFORM",
			"OBP_PLATFORM",
		},
		TokenEndpointEnvs: []string{
			"OCHAIN_OBP_AUTH_TOKEN_ENDPOINT",
			"OBP_OAUTH2_TOKEN_ENDPOINT",
		},
		AuthorizationEndpointEnv: "OCHAIN_OBP_AUTH_AUTHORIZATION_ENDPOINT",
		DeviceEnv:                "OCHAIN_OBP_AUTH_DEVICE_ENDPOINT",
		RedirectURLEnv:           "OCHAIN_OBP_AUTH_REDIRECT_URL",
	}
}

// EnsureDefaultConfig creates a default config file if it does not exist.
func EnsureDefaultConfig(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfg := DefaultConfig(home)
	return Save(path, cfg)
}

// Load reads config with a file lock for safety.
func Load(path string) (Config, error) {
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return Config{}, err
	}
	defer lock.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes config with a file lock.
func Save(path string, cfg Config) error {
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()

	var data []byte
	var err error
	if strings.EqualFold(filepath.Ext(path), ".json") {
		data, err = json.MarshalIndent(&cfg, "", "  ")
		if err == nil {
			data = append(data, '\n')
		}
	} else {
		data, err = yaml.Marshal(&cfg)
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

// GetContext finds a context by name.
func (c Config) GetContext(name string) (Context, error) {
	for _, ctx := range c.Contexts {
		if ctx.Name == name {
			return ctx, nil
		}
	}
	return Context{}, ErrContextNotFound
}

// UpsertContext adds or updates a context.
func (c *Config) UpsertContext(ctx Context) error {
	for i, existing := range c.Contexts {
		if existing.Name == ctx.Name {
			c.Contexts[i] = ctx
			return nil
		}
	}
	c.Contexts = append(c.Contexts, ctx)
	if c.CurrentContext == "" {
		c.CurrentContext = ctx.Name
	}
	return nil
}

// DeleteContext removes a context by name.
func (c *Config) DeleteContext(name string) error {
	idx := -1
	for i, ctx := range c.Contexts {
		if ctx.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ErrContextNotFound
	}
	c.Contexts = append(c.Contexts[:idx], c.Contexts[idx+1:]...)
	if c.CurrentContext == name {
		c.CurrentContext = ""
	}
	return nil
}

// Validate minimal required fields.
func (ctx Context) Validate() error {
	if ctx.Name == "" {
		return fmt.Errorf("context name is required")
	}
	if ctx.Profile == "" {
		return fmt.Errorf("context profile is required")
	}
	if ctx.TenancyOCID == "" {
		return fmt.Errorf("context tenancy_ocid is required")
	}
	if ctx.CompartmentOCID == "" {
		return fmt.Errorf("context compartment_ocid is required")
	}
	if !IsValidAuthMethod(ctx.AuthMethod) {
		return fmt.Errorf("context auth_method %q is invalid", ctx.AuthMethod)
	}
	return nil
}
