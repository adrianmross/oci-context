//go:build !darwin || !cgo

package cmd

import "fmt"

func authTokenKeychainAvailable() bool { return false }

func readAuthTokenKeychain(_, _ string) ([]byte, error) {
	return nil, fmt.Errorf("OAuth Keychain cache requires a macOS build with cgo enabled")
}

func writeAuthTokenKeychain(_, _ string, _ []byte) error {
	return fmt.Errorf("OAuth Keychain cache requires a macOS build with cgo enabled")
}
