package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
)

func managedOCIRCPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".oci-context", "oci_cli_rc"), nil
}

func syncOCIDefaultsForCurrent(cfg config.Config) error {
	if cfg.CurrentContext == "" {
		return nil
	}
	ctx, err := cfg.GetContext(cfg.CurrentContext)
	if err != nil {
		return err
	}
	rcPath, err := managedOCIRCPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(rcPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(rcPath, []byte(renderOCIRCLDefaults(ctx)), 0o600)
}

func renderOCIRCLDefaults(ctx config.Context) string {
	lines := []string{
		"[OCI_CLI_SETTINGS]",
	}
	if ctx.Profile != "" {
		lines = append(lines, "default_profile="+ctx.Profile)
	}
	lines = append(lines, "", "[DEFAULT]")
	if ctx.Region != "" {
		lines = append(lines, "region="+ctx.Region)
	}
	if ctx.CompartmentOCID != "" {
		lines = append(lines, "compartment-id="+ctx.CompartmentOCID)
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func ociEnvExportLines(cfg config.Config, rcPath string) []string {
	lines := []string{
		fmt.Sprintf("export OCI_CLI_RC_FILE=%s", rcPath),
	}
	if cfg.Options.OCIConfigPath != "" {
		lines = append(lines, fmt.Sprintf("export OCI_CLI_CONFIG_FILE=%s", cfg.Options.OCIConfigPath))
	}
	return lines
}
