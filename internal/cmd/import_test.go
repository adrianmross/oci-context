package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func TestJSONContextImport(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	cfg := config.DefaultConfig(tmp)
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(tmp, "lehighcap26.json")
	ctx := config.Context{
		Name:            "lehighcap26",
		Profile:         "lehighcap26-api",
		AuthMethod:      config.AuthMethodAPIKey,
		TenancyOCID:     "ocid1.tenancy.test",
		CompartmentOCID: "ocid1.tenancy.test",
		Region:          "us-ashburn-1",
		User:            "ocid1.user.test",
	}
	data, err := json.Marshal(exportContextView{Context: ctx})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := runJSONContextImport(cmd, cfgPath, false, input, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loaded.GetContext(ctx.Name)
	if err != nil || got.Profile != ctx.Profile {
		t.Fatalf("imported context mismatch: %+v, %v", got, err)
	}
}

func TestLooksLikeDeviceBundleAndJSON(t *testing.T) {
	tmp := t.TempDir()
	jsonPath := filepath.Join(tmp, "context.json")
	if err := os.WriteFile(jsonPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !looksLikeJSONContext(jsonPath) || looksLikeDeviceBundle(jsonPath) {
		t.Fatal("expected JSON context detection")
	}
	archivePath := filepath.Join(tmp, "context.ocix.tar.gz")
	if err := os.WriteFile(archivePath, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !looksLikeDeviceBundle(archivePath) {
		t.Fatal("expected archive detection")
	}
}
