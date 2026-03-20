package cmd

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type tuiPrefs struct {
	VerboseContexts     bool `yaml:"verbose_contexts"`
	VerboseTenancies    bool `yaml:"verbose_tenancies"`
	VerboseCompartments bool `yaml:"verbose_compartments"`
	VerboseRegions      bool `yaml:"verbose_regions"`
}

func defaultTUIPrefs() tuiPrefs {
	return tuiPrefs{
		VerboseContexts:     true,
		VerboseTenancies:    true,
		VerboseCompartments: false,
		VerboseRegions:      false,
	}
}

func tuiPrefsPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "oci-context", "tui.yml"), nil
}

func loadTUIPrefs() (tuiPrefs, string, error) {
	path, err := tuiPrefsPath()
	if err != nil {
		return defaultTUIPrefs(), "", err
	}

	prefs := defaultTUIPrefs()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return prefs, path, nil
		}
		return prefs, path, err
	}
	if err := yaml.Unmarshal(data, &prefs); err != nil {
		return defaultTUIPrefs(), path, err
	}
	return prefs, path, nil
}

func saveTUIPrefs(path string, prefs tuiPrefs) error {
	if path == "" {
		var err error
		path, err = tuiPrefsPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&prefs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
