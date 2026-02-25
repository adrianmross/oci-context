package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newListCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var output string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contexts",
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

			switch strings.ToLower(output) {
			case "":
				// Default: human-friendly list
				for _, ctx := range cfg.Contexts {
					marker := " "
					if ctx.Name == cfg.CurrentContext {
						marker = "*"
					}
					if verbose {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s (profile=%s region=%s tenancy=%s compartment=%s user=%s)\n",
							marker,
							ctx.Name,
							ctx.Profile,
							ctx.Region,
							ctx.TenancyOCID,
							ctx.CompartmentOCID,
							ctx.User,
						)
						continue
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s %s (profile=%s region=%s)\n", marker, ctx.Name, ctx.Profile, ctx.Region)
				}
				return nil
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(cfg.Contexts)
			case "yaml", "yml":
				enc := yaml.NewEncoder(cmd.OutOrStdout())
				defer enc.Close()
				return enc.Encode(cfg.Contexts)
			case "plain":
				for _, ctx := range cfg.Contexts {
					marker := ""
					if ctx.Name == cfg.CurrentContext {
						marker = "*"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "context=%s%s profile=%s region=%s tenancy=%s compartment=%s user=%s notes=%s\n",
						ctx.Name,
						marker,
						ctx.Profile,
						ctx.Region,
						ctx.TenancyOCID,
						ctx.CompartmentOCID,
						ctx.User,
						ctx.Notes,
					)
				}
				return nil
			default:
				return fmt.Errorf("unsupported output format: %s", output)
			}
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVarP(&output, "out", "o", "", "Output format: json|yaml|plain (default: human-readable)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed fields in human-readable output")
	return cmd
}
