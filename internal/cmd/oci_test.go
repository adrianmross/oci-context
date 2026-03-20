package cmd

import (
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestBuildOCIArgsAddsContextDefaultsWhenMissing(t *testing.T) {
	ctx := config.Context{
		Profile:         "DEFAULT",
		Region:          "us-phoenix-1",
		CompartmentOCID: "ocid1.compartment.oc1..abc",
	}

	got := buildOCIArgs([]string{"iam", "region", "list"}, ctx, "/Users/me/.oci/config")

	wantParts := []string{
		"iam", "region", "list",
		"--config-file", "/Users/me/.oci/config",
		"--profile", "DEFAULT",
		"--region", "us-phoenix-1",
		"--compartment-id", "ocid1.compartment.oc1..abc",
	}
	assertContainsInOrder(t, got, wantParts)
}

func TestBuildOCIArgsDoesNotOverrideExplicitFlags(t *testing.T) {
	ctx := config.Context{
		Profile:         "DEFAULT",
		Region:          "us-phoenix-1",
		CompartmentOCID: "ocid1.compartment.oc1..abc",
	}

	got := buildOCIArgs(
		[]string{
			"iam", "region", "list",
			"--profile", "OPS",
			"--region=eu-frankfurt-1",
			"-c", "ocid1.compartment.oc1..override",
			"--config-file", "/tmp/alt-oci-config",
		},
		ctx,
		"/Users/me/.oci/config",
	)

	assertContainsInOrder(t, got, []string{
		"iam", "region", "list",
		"--profile", "OPS",
		"--region=eu-frankfurt-1",
		"-c", "ocid1.compartment.oc1..override",
		"--config-file", "/tmp/alt-oci-config",
	})

	for _, forbidden := range []string{"DEFAULT", "us-phoenix-1", "ocid1.compartment.oc1..abc", "/Users/me/.oci/config"} {
		for _, arg := range got {
			if arg == forbidden {
				t.Fatalf("expected explicit flags to win; found injected value %q in %v", forbidden, got)
			}
		}
	}
}

func assertContainsInOrder(t *testing.T, got, want []string) {
	t.Helper()
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Fatalf("expected ordered subsequence %v in %v", want, got)
	}
}
