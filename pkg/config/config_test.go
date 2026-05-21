package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig() Config {
	return Config{
		Options: Options{
			OCIConfigPath:  "/tmp/oci",
			SocketPath:     "/tmp/daemon.sock",
			DefaultProfile: "DEFAULT",
			DaemonContexts: []string{"dev"},
		},
		Contexts: []Context{{
			Name:            "dev",
			Profile:         "DEFAULT",
			AuthMethod:      AuthMethodSecurityToken,
			TenancyOCID:     "ocid1.tenancy.oc1..aaaa",
			CompartmentOCID: "ocid1.compartment.oc1..bbbb",
			Region:          "us-ashburn-1",
			User:            "ocid1.user.oc1..cccc",
			Notes:           "test",
		}},
		CurrentContext: "dev",
	}
}

func TestSaveYAMLUsesYAMLEncoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := Save(path, testConfig()); err != nil {
		t.Fatalf("save yaml: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read yaml: %v", err)
	}
	if !strings.Contains(string(b), "current_context: dev") {
		t.Fatalf("expected yaml output, got %s", string(b))
	}
}

func TestSaveJSONUsesJSONEncodingAndLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, testConfig()); err != nil {
		t.Fatalf("save json: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read json: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("expected json output: %v\n%s", err, string(b))
	}
	if _, ok := raw["current_context"]; !ok {
		t.Fatalf("expected current_context json key, got %v", raw)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load saved json: %v", err)
	}
	if loaded.CurrentContext != "dev" || len(loaded.Contexts) != 1 || loaded.Contexts[0].AuthMethod != AuthMethodSecurityToken {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
}
