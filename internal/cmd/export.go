package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var format string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export current context as env or json",
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
			ctx, err := cfg.GetContext(cfg.CurrentContext)
			if err != nil {
				return err
			}

			switch format {
			case "env", "":
				lines := []string{
					fmt.Sprintf("export OCI_CLI_PROFILE=%s", ctx.Profile),
					fmt.Sprintf("export OCI_TENANCY_OCID=%s", ctx.TenancyOCID),
					fmt.Sprintf("export OCI_COMPARTMENT_OCID=%s", ctx.CompartmentOCID),
				}
				if ctx.Region != "" {
					lines = append(lines, fmt.Sprintf("export OCI_REGION=%s", ctx.Region))
				}
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(ctx); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported format: %s", format)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVarP(&format, "format", "f", "env", "Output format: env|json")
	return cmd
}
