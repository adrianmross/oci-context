package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestAuthCacheUseFileAndStatus(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yml")
	if err := config.Save(path, config.DefaultConfig(tmp)); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"auth", "cache", "use", "file", "--config", path})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := mustLoadConfig(t, path).Options.OAuthTokenCache; got != config.OAuthTokenCacheFile {
		t.Fatalf("cache = %q, want file", got)
	}

	cmd = newRootCmd()
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"auth", "cache", "status", "--config", path, "--output", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"backend":"file"`) {
		t.Fatalf("unexpected status: %s", out.String())
	}
}

func TestAuthTokenCacheKeyDoesNotExposeServiceMetadata(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	key, err := authTokenCacheKey(cfg, "example-service", "https://issuer.example.com", "client", "openid")
	if err != nil {
		t.Fatal(err)
	}
	other, err := authTokenCacheKey(config.DefaultConfig(t.TempDir()), "example-service", "https://issuer.example.com", "client", "openid")
	if err != nil || other == key {
		t.Fatal("expected separate cache namespaces")
	}
	if len(key) != 64 || strings.Contains(key, "example-service") {
		t.Fatalf("unexpected cache key %q", key)
	}
}

func TestAuthCacheUseKeychainRequiresNativeBackend(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yml")
	if err := config.Save(path, config.DefaultConfig(tmp)); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	cmd.SetArgs([]string{"auth", "cache", "use", "keychain", "--config", path})
	err := cmd.Execute()
	if authTokenKeychainAvailable() {
		if err != nil {
			t.Fatal(err)
		}
		if got := mustLoadConfig(t, path).Options.OAuthTokenCache; got != config.OAuthTokenCacheKeychain {
			t.Fatalf("cache = %q, want keychain", got)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "macOS build") {
		t.Fatalf("expected native-backend error, got %v", err)
	}
}
