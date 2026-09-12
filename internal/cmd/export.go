package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

type exportContextView struct {
	config.Context
	CurrentService string `json:"current_service,omitempty"`
}

func newExportCmd() *cobra.Command {
	var cfgPath string
	var useGlobal, archive, includePrivateKey bool
	var output, file, format string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export current context as env, json, or device archive",
		RunE: func(cmd *cobra.Command, args []string) error {
			useGlobal, err := cmd.Flags().GetBool("global")
			if err != nil {
				return err
			}
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if cfg.CurrentContext == "" {
				return fmt.Errorf("no current context set")
			}
			if format != "" {
				output = format
			}
			if archive || output == "archive" {
				if file == "" {
					file = cfg.CurrentContext + ".ocix.tar.gz"
				}
				return runDeviceExport(cmd, cfgPath, useGlobal, "", file, includePrivateKey, true)
			}
			ctx, err := cfg.GetContext(cfg.CurrentContext)
			if err != nil {
				return err
			}

			switch output {
			case "env", "":
				lines := []string{}
				if ctx.Profile != "" {
					lines = append(lines, fmt.Sprintf("export OCI_CLI_PROFILE=%s", ctx.Profile))
				}
				if ctx.Region != "" {
					lines = append(lines, fmt.Sprintf("export OCI_CLI_REGION=%s", ctx.Region))
				}
				if cfg.Options.OCIConfigPath != "" {
					lines = append(lines, fmt.Sprintf("export OCI_CLI_CONFIG_FILE=%s", cfg.Options.OCIConfigPath))
				}
				lines = append(lines,
					fmt.Sprintf("export OCI_TENANCY_OCID=%s", ctx.TenancyOCID),
					fmt.Sprintf("export OCI_COMPARTMENT_OCID=%s", ctx.CompartmentOCID),
				)
				if ctx.Region != "" {
					lines = append(lines, fmt.Sprintf("export OCI_REGION=%s", ctx.Region))
				}
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
			case "oci-env":
				if err := syncOCIDefaultsForCurrent(cfg); err != nil {
					return err
				}
				rcPath, err := managedOCIRCPath()
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(ociEnvExportLines(cfg, rcPath), "\n"))
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(exportContextView{
					Context:        ctx,
					CurrentService: cfg.CurrentService,
				}); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported format: %s", output)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVarP(&output, "output", "o", "env", "Output format: env|json|oci-env|archive")
	cmd.Flags().StringVar(&format, "format", "", "Deprecated alias for --output")
	cmd.Flags().BoolVar(&archive, "archive", false, "Export a portable device archive")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Output file for device archive")
	cmd.Flags().BoolVar(&includePrivateKey, "include-private-key", false, "Include the API private key in a device archive")
	return cmd
}
