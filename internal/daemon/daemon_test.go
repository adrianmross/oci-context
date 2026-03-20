package daemon

import (
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestBuildValidateOCIArgsOmitsCompartmentFlag(t *testing.T) {
	ctx := config.Context{
		Profile:         "DEFAULT",
		AuthMethod:      config.AuthMethodSecurityToken,
		TenancyOCID:     "ocid1.tenancy.oc1..aaaa",
		CompartmentOCID: "ocid1.compartment.oc1..bbbb",
		Region:          "us-ashburn-1",
	}
	args := buildValidateOCIArgs(ctx, "/Users/test/.oci/config")

	contains := func(flag string) bool {
		for _, arg := range args {
			if arg == flag {
				return true
			}
		}
		return false
	}

	if contains("--compartment-id") || contains("-c") {
		t.Fatalf("validate args must not contain compartment flag: %v", args)
	}
	if !contains("--tenancy-id") || !contains("--profile") || !contains("--auth") || !contains("--region") {
		t.Fatalf("expected tenancy/profile/auth/region flags in validate args: %v", args)
	}
}

func TestBuildRefreshOCIArgs(t *testing.T) {
	ctx := config.Context{
		Profile:    "DEFAULT",
		Region:     "us-phoenix-1",
		AuthMethod: config.AuthMethodSecurityToken,
	}
	args := buildRefreshOCIArgs(ctx, "/Users/test/.oci/config")
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	for _, want := range []string{"session", "refresh", "--profile", "DEFAULT", "--config-file", "/Users/test/.oci/config", "--region", "us-phoenix-1"} {
		if !containsArg(args, want) {
			t.Fatalf("expected refresh args to contain %q, got %v", want, args)
		}
	}
	_ = joined
}

func TestFindHomeRegion(t *testing.T) {
	payload := []byte(`{"data":[{"is-home-region":false,"region-key":"PHX","region-name":"us-phoenix-1","status":"READY"},{"is-home-region":true,"region-key":"IAD","region-name":"us-ashburn-1","status":"READY"}]}`)
	region, err := findHomeRegion(payload)
	if err != nil {
		t.Fatalf("expected home region, got error %v", err)
	}
	if region.RegionName != "us-ashburn-1" || region.RegionKey != "IAD" || !region.IsHomeRegion {
		t.Fatalf("unexpected home region parsed: %+v", region)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
