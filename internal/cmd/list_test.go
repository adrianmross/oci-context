package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
	"gopkg.in/yaml.v3"
)

func TestListOutputs(t *testing.T) {
	baseCfg := config.Config{
		Options: config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{
			{
				Name:            "dev",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..aaaa",
				CompartmentOCID: "ocid1.compartment.oc1..bbbb",
				Region:          "us-phoenix-1",
				User:            "ocid1.user.oc1..cccc",
				Notes:           "dev-notes",
			},
			{
				Name:            "prod",
				Profile:         "PROD",
				TenancyOCID:     "ocid1.tenancy.oc1..zzzz",
				CompartmentOCID: "ocid1.compartment.oc1..yyyy",
				Region:          "us-ashburn-1",
				User:            "ocid1.user.oc1..xxxx",
				Notes:           "",
			},
		},
		CurrentContext: "dev",
	}

	tests := []struct {
		name      string
		mutate    func(c config.Config) config.Config
		args      []string
		assert    func(t *testing.T, got string, err error)
		assertErr string
	}{
		{
			name:   "default human output",
			mutate: func(c config.Config) config.Config { return c },
			args:   []string{"list"},
			assert: func(t *testing.T, got string, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				want := strings.Join([]string{
					"* dev (profile=DEFAULT region=us-phoenix-1)",
					"  prod (profile=PROD region=us-ashburn-1)",
					"",
				}, "\n")
				if got != want {
					t.Fatalf("output mismatch\nwant:\n%q\ngot:\n%q", want, got)
				}
			},
		},
		{
			name:   "verbose human output",
			mutate: func(c config.Config) config.Config { return c },
			args:   []string{"list", "-v"},
			assert: func(t *testing.T, got string, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				want := strings.Join([]string{
					"* dev (profile=DEFAULT region=us-phoenix-1 tenancy=ocid1.tenancy.oc1..aaaa compartment=ocid1.compartment.oc1..bbbb user=ocid1.user.oc1..cccc)",
					"  prod (profile=PROD region=us-ashburn-1 tenancy=ocid1.tenancy.oc1..zzzz compartment=ocid1.compartment.oc1..yyyy user=ocid1.user.oc1..xxxx)",
					"",
				}, "\n")
				if got != want {
					t.Fatalf("output mismatch\nwant:\n%q\ngot:\n%q", want, got)
				}
			},
		},
		{
			name:   "plain output",
			mutate: func(c config.Config) config.Config { return c },
			args:   []string{"list", "-o", "plain"},
			assert: func(t *testing.T, got string, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				want := strings.Join([]string{
					"context=dev* profile=DEFAULT region=us-phoenix-1 tenancy=ocid1.tenancy.oc1..aaaa compartment=ocid1.compartment.oc1..bbbb user=ocid1.user.oc1..cccc notes=dev-notes",
					"context=prod profile=PROD region=us-ashburn-1 tenancy=ocid1.tenancy.oc1..zzzz compartment=ocid1.compartment.oc1..yyyy user=ocid1.user.oc1..xxxx notes=",
					"",
				}, "\n")
				if got != want {
					t.Fatalf("output mismatch\nwant:\n%q\ngot:\n%q", want, got)
				}
			},
		},
		{
			name:   "json output",
			mutate: func(c config.Config) config.Config { return c },
			args:   []string{"list", "-o", "json"},
			assert: func(t *testing.T, got string, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				var out []config.Context
				if err := json.Unmarshal([]byte(got), &out); err != nil {
					t.Fatalf("unmarshal json: %v", err)
				}
				want := baseCfg.Contexts
				if len(out) != len(want) {
					t.Fatalf("expected %d contexts, got %d", len(want), len(out))
				}
				for i := range want {
					if out[i] != want[i] {
						t.Fatalf("context %d mismatch: want %+v got %+v", i, want[i], out[i])
					}
				}
			},
		},
		{
			name:   "yaml output",
			mutate: func(c config.Config) config.Config { return c },
			args:   []string{"list", "-o", "yaml"},
			assert: func(t *testing.T, got string, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				var out []config.Context
				if err := yaml.Unmarshal([]byte(got), &out); err != nil {
					t.Fatalf("unmarshal yaml: %v", err)
				}
				want := baseCfg.Contexts
				if len(out) != len(want) {
					t.Fatalf("expected %d contexts, got %d", len(want), len(out))
				}
				for i := range want {
					if out[i] != want[i] {
						t.Fatalf("context %d mismatch: want %+v got %+v", i, want[i], out[i])
					}
				}
			},
		},
		{
			name:      "unsupported output",
			mutate:    func(c config.Config) config.Config { return c },
			args:      []string{"list", "-o", "xml"},
			assertErr: "unsupported output format: xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.mutate(baseCfg)
			tmp := t.TempDir()
			cfgPath := tmp + "/config.yml"
			if err := config.Save(cfgPath, cfg); err != nil {
				t.Fatalf("save config: %v", err)
			}

			cmd := newListCmd()
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(append(tt.args, "--config", cfgPath))
			err := cmd.Execute()

			if tt.assertErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.assertErr) {
					t.Fatalf("expected error %q, got %v", tt.assertErr, err)
				}
				return
			}

			if tt.assert == nil {
				t.Fatalf("assert function must be provided for test %q", tt.name)
			}
			tt.assert(t, buf.String(), err)
		})
	}
}
