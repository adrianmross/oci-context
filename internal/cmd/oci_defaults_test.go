package cmd

import (
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestRenderOCIRCLDefaults(t *testing.T) {
	ctx := config.Context{
		Profile:         "DEFAULT",
		Region:          "us-phoenix-1",
		CompartmentOCID: "ocid1.compartment.oc1..abc",
	}
	got := renderOCIRCLDefaults(ctx)

	for _, want := range []string{
		"[OCI_CLI_SETTINGS]",
		"default_profile=DEFAULT",
		"[DEFAULT]",
		"region=us-phoenix-1",
		"compartment-id=ocid1.compartment.oc1..abc",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rc content to contain %q, got:\n%s", want, got)
		}
	}
}

func TestOciEnvExportLines(t *testing.T) {
	cfg := config.Config{
		Options: config.Options{OCIConfigPath: "/Users/test/.oci/config"},
	}
	lines := ociEnvExportLines(cfg, "/Users/test/.oci-context/oci_cli_rc")
	got := strings.Join(lines, "\n")

	if !strings.Contains(got, "OCI_CLI_RC_FILE=/Users/test/.oci-context/oci_cli_rc") {
		t.Fatalf("missing OCI_CLI_RC_FILE export in %q", got)
	}
	if !strings.Contains(got, "OCI_CLI_CONFIG_FILE=/Users/test/.oci/config") {
		t.Fatalf("missing OCI_CLI_CONFIG_FILE export in %q", got)
	}
}
