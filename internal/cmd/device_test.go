package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileBlockOmitsSessionMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	contents := "[lehighcap26]\ntenancy=tenancy\nregion=us-ashburn-1\nkey_file=/tmp/private.pem\nsecurity_token_file=/tmp/token\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, keyPath, err := profileBlock(path, "lehighcap26", false)
	if err != nil {
		t.Fatal(err)
	}
	if keyPath != "" || strings.Contains(profile, "key_file") || strings.Contains(profile, "security_token") {
		t.Fatalf("session material leaked: profile=%q key=%q", profile, keyPath)
	}
}

func TestProfileBlockIncludesPortableKeyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	contents := "[lehighcap26]\ntenancy=tenancy\nregion=us-ashburn-1\nkey_file=/tmp/private.pem\nsecurity_token_file=/tmp/token\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, keyPath, err := profileBlock(path, "lehighcap26", true)
	if err != nil {
		t.Fatal(err)
	}
	if keyPath != "/tmp/private.pem" || !strings.Contains(profile, "key_file=~/.oci/sessions/lehighcap26/oci_api_key.pem") || strings.Contains(profile, "security_token") {
		t.Fatalf("unexpected portable profile=%q key=%q", profile, keyPath)
	}
}
