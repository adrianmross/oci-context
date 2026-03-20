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
