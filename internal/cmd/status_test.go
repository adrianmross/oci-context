package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/adrianmross/oci-context/pkg/oci"
)

// copyConfig deep-copies a config for mutation in tests.
func copyConfig(c config.Config) config.Config {
	out := c
	out.Contexts = make([]config.Context, len(c.Contexts))
	copy(out.Contexts, c.Contexts)
	return out
}

// stubIdentity returns fixed identity details for tests.
func stubIdentity() func() {
	original := fetchIdentity
	fetchIdentity = func(_ctx context.Context, _path, _profile, region, tenancyOCID, compartmentOCID, userOCID string) (oci.IdentityDetails, error) {
		return oci.IdentityDetails{
			TenancyName:     "Tenancy Friendly",
			TenancyOCID:     tenancyOCID,
			CompartmentName: "Compartment Friendly",
			CompartmentOCID: compartmentOCID,
			UserName:        "User Friendly",
			UserOCID:        userOCID,
			Region:          region,
		}, nil
	}
	return func() { fetchIdentity = original }
}

// stubIdentityError forces an error.
func stubIdentityError(err error) func() {
	original := fetchIdentity
	fetchIdentity = func(_ctx context.Context, _path, _profile, _region, _tenancyOCID, _compartmentOCID, _userOCID string) (oci.IdentityDetails, error) {
		return oci.IdentityDetails{}, err
	}
	return func() { fetchIdentity = original }
}

func TestStatusOutputs(t *testing.T) {
	restore := stubIdentity()
	defer restore()

	baseCfg := config.Config{
		Options: config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{{
			Name:            "dev",
			Profile:         "DEFAULT",
			TenancyOCID:     "ocid1.tenancy.oc1..aaaa",
			CompartmentOCID: "ocid1.compartment.oc1..bbbb",
			Region:          "us-phoenix-1",
			User:            "ocid1.user.oc1..cccc",
		}},
		CurrentContext: "dev",
	}

	tests := []struct {
		name      string
		mutateCfg func(c config.Config) config.Config
		args      []string
		want      string
		wantErr   string
	}{
		{
			name: "default multiline with profile omitted when same as context",
			mutateCfg: func(c config.Config) config.Config {
				c.Contexts[0].Profile = "dev" // context == profile
				c.Contexts[0].User = "ocid1.user.oc1..userX"
				return c
			},
			args: []string{"status"},
			want: strings.Join([]string{
				"context: dev",
				"tenancy: Tenancy Friendly (ocid1.tenancy.oc1..aaaa)",
				"compartment: Compartment Friendly (ocid1.compartment.oc1..bbbb)",
				"user: User Friendly (ocid1.user.oc1..userX)",
				"region: us-phoenix-1",
				"",
			}, "\n"),
		},
		{
			name:      "default multiline with profile shown",
			mutateCfg: func(c config.Config) config.Config { return c },
			args:      []string{"status"},
			want: strings.Join([]string{
				"context: dev",
				"profile: DEFAULT",
				"tenancy: Tenancy Friendly (ocid1.tenancy.oc1..aaaa)",
				"compartment: Compartment Friendly (ocid1.compartment.oc1..bbbb)",
				"user: User Friendly (ocid1.user.oc1..cccc)",
				"region: us-phoenix-1",
				"",
			}, "\n"),
		},
		{
			name:      "plain OCIDs only (-p)",
			mutateCfg: func(c config.Config) config.Config { return c },
			args:      []string{"status", "-p"},
			want:      "context=dev profile=DEFAULT tenancy=ocid1.tenancy.oc1..aaaa compartment=ocid1.compartment.oc1..bbbb user=ocid1.user.oc1..cccc region=us-phoenix-1\n",
		},
		{
			name:      "plain friendly single-line (-o plain)",
			mutateCfg: func(c config.Config) config.Config { return c },
			args:      []string{"status", "-o", "plain"},
			want:      "context=dev profile=DEFAULT tenancy=Tenancy Friendly (" + abbrevOCID("ocid1.tenancy.oc1..aaaa") + ") compartment=Compartment Friendly (" + abbrevOCID("ocid1.compartment.oc1..bbbb") + ") user=User Friendly (" + abbrevOCID("ocid1.user.oc1..cccc") + ") region=us-phoenix-1\n",
		},
		{
			name:      "json output",
			mutateCfg: func(c config.Config) config.Config { return c },
			args:      []string{"status", "-o", "json"},
			want:      "{\n  \"compartment\": \"Compartment Friendly\",\n  \"compartment_id\": \"ocid1.compartment.oc1..bbbb\",\n  \"context\": \"dev\",\n  \"profile\": \"DEFAULT\",\n  \"region\": \"us-phoenix-1\",\n  \"tenancy\": \"Tenancy Friendly\",\n  \"tenancy_id\": \"ocid1.tenancy.oc1..aaaa\",\n  \"user\": \"User Friendly\",\n  \"user_id\": \"ocid1.user.oc1..cccc\"\n}\n",
		},
		{
			name:      "yaml output",
			mutateCfg: func(c config.Config) config.Config { return c },
			args:      []string{"status", "-o", "yaml"},
			want: strings.Join([]string{
				"compartment: Compartment Friendly",
				"compartment_id: ocid1.compartment.oc1..bbbb",
				"context: dev",
				"profile: DEFAULT",
				"region: us-phoenix-1",
				"tenancy: Tenancy Friendly",
				"tenancy_id: ocid1.tenancy.oc1..aaaa",
				"user: User Friendly",
				"user_id: ocid1.user.oc1..cccc",
				"",
			}, "\n"),
		},
		{
			name:      "unsupported output format",
			mutateCfg: func(c config.Config) config.Config { return c },
			args:      []string{"status", "-o", "xml"},
			wantErr:   "unsupported output format: xml",
		},
		{
			name: "no current context set",
			mutateCfg: func(c config.Config) config.Config {
				c.CurrentContext = ""
				return c
			},
			args:    []string{"status"},
			wantErr: "no current context set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.mutateCfg(copyConfig(baseCfg))
			// Write temp config file
			tmp := t.TempDir()
			cfgPath := tmp + "/config.yml"
			if err := config.Save(cfgPath, cfg); err != nil {
				t.Fatalf("save config: %v", err)
			}

			cmd := newStatusCmd()
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(append(tt.args, "--config", cfgPath))
			err := cmd.Execute()

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			got := buf.String()
			if got != tt.want {
				t.Fatalf("output mismatch\nwant:\n%q\ngot:\n%q", tt.want, got)
			}
		})
	}
}

func TestStatusIdentityError(t *testing.T) {
	restore := stubIdentityError(errors.New("boom"))
	defer restore()

	cfg := config.Config{
		Options: config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{{
			Name:            "dev",
			Profile:         "DEFAULT",
			TenancyOCID:     "ocid1.tenancy.oc1..aaaa",
			CompartmentOCID: "ocid1.compartment.oc1..bbbb",
			Region:          "us-phoenix-1",
			User:            "ocid1.user.oc1..cccc",
		}},
		CurrentContext: "dev",
	}
	tmp := t.TempDir()
	cfgPath := tmp + "/config.yml"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd := newStatusCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"status", "--config", cfgPath})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected identity error, got %v", err)
	}
}
