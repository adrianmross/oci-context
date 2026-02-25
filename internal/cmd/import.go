package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/adrianmross/oci-context/pkg/ocicfg"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var ociCfgPath string
	var overwrite bool

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import contexts from OCI CLI config profiles",
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

			if ociCfgPath == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				ociCfgPath = filepath.Join(home, ".oci", "config")
			}

			profiles, err := ocicfg.LoadProfiles(ociCfgPath)
			if err != nil {
				return err
			}

			imported := 0
			skipped := 0
			for name, p := range profiles {
				ctx := config.Context{
					Name:            name,
					Profile:         name,
					TenancyOCID:     p.Tenancy,
					CompartmentOCID: p.Tenancy, // default to root compartment
					Region:          p.Region,
					User:            p.User,
					Notes:           "imported from OCI CLI config",
				}
				if err := ctx.Validate(); err != nil {
					return fmt.Errorf("profile %s invalid: %w", name, err)
				}
				if !overwrite {
					// if exists, skip
					if _, err := cfg.GetContext(name); err == nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "skip: %s (exists)\n", name)
						skipped++
						continue
					}
				}
				if err := cfg.UpsertContext(ctx); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "import: %s (profile)\n", name)
				imported++
			}

			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Imported %d profiles (skipped %d) from %s\n", imported, skipped, ociCfgPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to oci-context config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVarP(&ociCfgPath, "oci-config", "o", "", "Path to OCI CLI config (default ~/.oci/config)")
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "w", false, "Overwrite existing contexts with same name")
	return cmd
}
