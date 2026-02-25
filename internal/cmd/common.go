package cmd

import (
	"os"
	"path/filepath"
)

// resolveConfigPath returns the config path based on flags and project discovery.
// Priority:
//  1. explicit --config
//  2. if global flag set -> ~/.oci-context/config.yml
//  3. project-local configs (in order):
//     ./.oci-context.yml, ./.oci-context.json,
//     ./.oci-context/config.yml, ./.oci-context/config.json,
//     ./oci-context.yml, ./oci-context.json,
//     ./oci-context/config.yml, ./oci-context/config.json
//  4. fallback to ~/.oci-context/config.yml
func resolveConfigPath(cfg string, global bool) (string, error) {
	if cfg != "" {
		return cfg, nil
	}

	// global override
	if global {
		return globalConfigPath()
	}

	// project discovery (cwd)
	if wd, err := os.Getwd(); err == nil {
		candidates := []string{
			".oci-context.yml",
			".oci-context.json",
			filepath.Join(".oci-context", "config.yml"),
			filepath.Join(".oci-context", "config.json"),
			"oci-context.yml",
			"oci-context.json",
			filepath.Join("oci-context", "config.yml"),
			filepath.Join("oci-context", "config.json"),
		}
		for _, rel := range candidates {
			p := filepath.Join(wd, rel)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, nil
			}
		}
	}

	return globalConfigPath()
}

func globalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".oci-context", "config.yml"), nil
}
