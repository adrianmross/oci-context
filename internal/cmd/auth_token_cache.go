package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/adrianmross/oci-context/pkg/config"
)

const authTokenKeychainService = "oci-context.oauth-token.v1"

// Use the same cache namespace as the file backend, including its configuration directory.
func authTokenCacheKey(cfg config.Config, service, issuer, clientID, scope string) (string, error) {
	path, err := authTokenCachePath(cfg, service, issuer, clientID, scope)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(path))
	return hex.EncodeToString(digest[:]), nil
}

func readConfiguredAuthTokenCache(cfg config.Config, service, issuer, clientID, scope string) (authTokenCacheEntry, error) {
	switch config.NormalizeOAuthTokenCache(cfg.Options.OAuthTokenCache) {
	case config.OAuthTokenCacheFile:
		path, err := authTokenCachePath(cfg, service, issuer, clientID, scope)
		if err != nil {
			return authTokenCacheEntry{}, err
		}
		return readAuthTokenCache(path)
	case config.OAuthTokenCacheKeychain:
		if !authTokenKeychainAvailable() {
			return authTokenCacheEntry{}, fmt.Errorf("OAuth Keychain cache requires a macOS build with cgo enabled")
		}
		key, err := authTokenCacheKey(cfg, service, issuer, clientID, scope)
		if err != nil {
			return authTokenCacheEntry{}, err
		}
		data, err := readAuthTokenKeychain(authTokenKeychainService, key)
		if err != nil {
			return authTokenCacheEntry{}, err
		}
		return decodeAuthTokenCache(data)
	default:
		return authTokenCacheEntry{}, fmt.Errorf("unsupported OAuth token cache %q", cfg.Options.OAuthTokenCache)
	}
}

func writeConfiguredAuthTokenCache(cfg config.Config, entry authTokenCacheEntry) error {
	switch config.NormalizeOAuthTokenCache(cfg.Options.OAuthTokenCache) {
	case config.OAuthTokenCacheFile:
		path, err := authTokenCachePath(cfg, entry.Service, entry.Issuer, entry.ClientID, entry.Scope)
		if err != nil {
			return err
		}
		return writeAuthTokenCache(path, entry)
	case config.OAuthTokenCacheKeychain:
		if !authTokenKeychainAvailable() {
			return fmt.Errorf("OAuth Keychain cache requires a macOS build with cgo enabled")
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		key, err := authTokenCacheKey(cfg, entry.Service, entry.Issuer, entry.ClientID, entry.Scope)
		if err != nil {
			return err
		}
		return writeAuthTokenKeychain(authTokenKeychainService, key, data)
	default:
		return fmt.Errorf("unsupported OAuth token cache %q", cfg.Options.OAuthTokenCache)
	}
}

func decodeAuthTokenCache(data []byte) (authTokenCacheEntry, error) {
	var entry authTokenCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return authTokenCacheEntry{}, err
	}
	if entry.AccessToken == "" {
		return authTokenCacheEntry{}, fmt.Errorf("cached token is empty")
	}
	return entry, nil
}
