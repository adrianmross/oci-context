package cmd

import (
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestBuildAuthValidateOCIArgsOmitsCompartmentFlag(t *testing.T) {
	ctx := config.Context{
		Profile:         "DEFAULT",
		AuthMethod:      config.AuthMethodAPIKey,
		TenancyOCID:     "ocid1.tenancy.oc1..aaaa",
		CompartmentOCID: "ocid1.compartment.oc1..bbbb",
		Region:          "us-sanjose-1",
	}
	args := buildAuthValidateOCIArgs(ctx, "/Users/test/.oci/config")

	contains := func(flag string) bool {
		for i := range args {
			if args[i] == flag {
				return true
			}
		}
		return false
	}

	if contains("--compartment-id") || contains("-c") {
		t.Fatalf("validate args must not contain compartment flag: %v", args)
	}
	if !contains("--tenancy-id") {
		t.Fatalf("expected tenancy-id flag in validate args: %v", args)
	}
	if !contains("--profile") || !contains("--config-file") || !contains("--auth") || !contains("--region") {
		t.Fatalf("expected profile/config/auth/region defaults in validate args: %v", args)
	}
}
