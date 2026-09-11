package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestPromoteAPIKeyUploadsWithSessionAndSwitchesContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	contents := `options:
  oci_config_path: /tmp/oci-config
contexts:
  - name: target
    profile: target-api
    auth_method: security_token
    tenancy_ocid: ocid1.tenancy.oc1..tenancy
    compartment_ocid: ocid1.tenancy.oc1..tenancy
    region: us-ashburn-1
    user: ocid1.user.oc1..user
current_context: ""
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	var got []string
	old := runOCIForAuth
	runOCIForAuth = func(_ *cobra.Command, args []string) error {
		got = append([]string(nil), args...)
		return nil
	}
	t.Cleanup(func() { runOCIForAuth = old })

	cmd := newRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"auth", "promote-api-key", "--config", path, "--context", "target",
		"--from-profile", "session", "--public-key-file", "/tmp/key.pub",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	want := []string{"iam", "user", "api-key", "upload", "--user-id", "ocid1.user.oc1..user", "--key-file", "/tmp/key.pub", "--profile", "session", "--auth", "security_token", "--region", "us-ashburn-1"}
	if len(got) != len(want) {
		t.Fatalf("args=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args=%v, want %v", got, want)
		}
	}
}
