package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
)

func TestAuthCapabilityForMethod(t *testing.T) {
	sec := authCapabilityForMethod(config.AuthMethodSecurityToken)
	if !sec.CanLogin || !sec.CanRefresh || !sec.CanValidate || !sec.CanSetup {
		t.Fatalf("expected full security_token capabilities, got %+v", sec)
	}

	api := authCapabilityForMethod(config.AuthMethodAPIKey)
	if api.CanRefresh || api.CanLogin || !api.CanSetup || !api.CanValidate {
		t.Fatalf("unexpected api_key capabilities: %+v", api)
	}

	rp := authCapabilityForMethod(config.AuthMethodResourcePrincipal)
	if rp.CanLogin || rp.CanRefresh || rp.CanSetup || !rp.CanValidate {
		t.Fatalf("unexpected resource_principal capabilities: %+v", rp)
	}
}

func TestAuthMethodsCommandOutput(t *testing.T) {
	cmd := newAuthCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"methods"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"api_key",
		"security_token",
		"instance_principal",
		"resource_principal",
		"instance_obo_user",
		"oke_workload_identity",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected methods output to contain %q, got %q", want, got)
		}
	}
}

func TestFindHomeRegion(t *testing.T) {
	payload := []byte(`{
  "data": [
    {"is-home-region": false, "region-key": "PHX", "region-name": "us-phoenix-1", "status": "READY"},
    {"is-home-region": true, "region-key": "IAD", "region-name": "us-ashburn-1", "status": "READY"}
  ]
}`)
	region, err := findHomeRegion(payload)
	if err != nil {
		t.Fatalf("expected home region, got error %v", err)
	}
	if region.RegionName != "us-ashburn-1" || region.RegionKey != "IAD" || !region.IsHomeRegion {
		t.Fatalf("unexpected home region parsed: %+v", region)
	}
}

func TestFindHomeRegionReturnsErrorWhenMissing(t *testing.T) {
	payload := []byte(`{"data":[{"is-home-region":false,"region-key":"PHX","region-name":"us-phoenix-1","status":"READY"}]}`)
	_, err := findHomeRegion(payload)
	if err == nil {
		t.Fatalf("expected error when home region is missing")
	}
}
