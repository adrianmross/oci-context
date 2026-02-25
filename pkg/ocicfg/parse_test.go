package ocicfg

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadProfiles_SuccessAndDefaults(t *testing.T) {
	config := `
[DEFAULT]
user=ocid1.user.oc1..user123
tenancy=ocid1.tenancy.oc1..ten123
region=us-ashburn-1

[SECOND]
tenancy=ocid1.tenancy.oc1..ten456
region=us-phoenix-1
`
	path := writeTempConfig(t, config)

	profiles, err := LoadProfiles(path)
	if err != nil {
		t.Fatalf("LoadProfiles returned error: %v", err)
	}

	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	def := profiles["DEFAULT"]
	if def.User != "ocid1.user.oc1..user123" || def.Tenancy != "ocid1.tenancy.oc1..ten123" || def.Region != "us-ashburn-1" {
		t.Fatalf("DEFAULT profile mismatch: %+v", def)
	}

	sec := profiles["SECOND"]
	// user is optional; should default to tenancy when missing
	if sec.User != "ocid1.tenancy.oc1..ten456" {
		t.Fatalf("SECOND user default mismatch: %s", sec.User)
	}
	if sec.Tenancy != "ocid1.tenancy.oc1..ten456" || sec.Region != "us-phoenix-1" {
		t.Fatalf("SECOND profile mismatch: %+v", sec)
	}
}

func TestLoadProfiles_Errors(t *testing.T) {
	configMissingTenancy := `
[BAD]
region=us-ashburn-1
`
	path := writeTempConfig(t, configMissingTenancy)
	if _, err := LoadProfiles(path); err == nil || err.Error() != "profile BAD missing tenancy" {
		t.Fatalf("expected missing tenancy error, got %v", err)
	}

	configMissingRegion := `
[BAD]
tenancy=ocid1.tenancy.oc1..ten123
`
	path = writeTempConfig(t, configMissingRegion)
	if _, err := LoadProfiles(path); err == nil || err.Error() != "profile BAD missing region" {
		t.Fatalf("expected missing region error, got %v", err)
	}
}
