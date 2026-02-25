package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"gopkg.in/yaml.v3"
)

// Config represents the persisted state for oci-context.
type Config struct {
	Options        Options   `yaml:"options"`
	Contexts       []Context `yaml:"contexts"`
	CurrentContext string    `yaml:"current_context"`
}

// Options holds global settings.
type Options struct {
	OCIConfigPath  string `yaml:"oci_config_path"`
	SocketPath     string `yaml:"socket_path"`
	DefaultProfile string `yaml:"default_profile"`
}

// Context describes a selectable OCI context.
type Context struct {
	Name            string `yaml:"name"`
	Profile         string `yaml:"profile"`
	TenancyOCID     string `yaml:"tenancy_ocid"`
	CompartmentOCID string `yaml:"compartment_ocid"`
	Region          string `yaml:"region"`
	User            string `yaml:"user"`
	Notes           string `yaml:"notes"`
}

var (
	ErrContextNotFound = errors.New("context not found")
	ErrDuplicateName   = errors.New("context name already exists")
)

// DefaultConfig returns the initial config.
func DefaultConfig(home string) Config {
	return Config{
		Options: Options{
			OCIConfigPath:  filepath.Join(home, ".oci", "config"),
			SocketPath:     filepath.Join(home, ".oci-context", "daemon.sock"),
			DefaultProfile: "",
		},
		Contexts:       []Context{},
		CurrentContext: "",
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

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return nil
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
	return nil
}
