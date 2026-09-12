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

func TestMergeOCIProfileReplacesOnlySelectedProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	contents := "[one]\ntenancy=old\nregion=us-phoenix-1\n\n[two]\ntenancy=keep\nregion=us-ashburn-1\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mergeOCIProfile(path, "one", "[one]\ntenancy=new\nregion=us-ashburn-1\n", true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "tenancy=new") || !strings.Contains(string(got), "[two]\ntenancy=keep") || strings.Contains(string(got), "tenancy=old") {
		t.Fatalf("unexpected merged config: %s", got)
	}
}

func TestDeviceArchiveRoundTrip(t *testing.T) {
	source := t.TempDir()
	for name, contents := range map[string]string{"context.json": "{}\n", "oci-profile": "[one]\n", "README.md": "readme\n"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := archiveDeviceBundle(source, archive); err != nil {
		t.Fatal(err)
	}
	destination, cleanup, err := openDeviceBundle(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for name := range map[string]bool{"context.json": true, "oci-profile": true, "README.md": true} {
		if _, err := os.Stat(filepath.Join(destination, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}
