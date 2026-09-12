package cmd

import (
	"encoding/json"
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
	var overwrite, archive bool
	var file string

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import contexts or a device bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			if archive || (file != "" && looksLikeDeviceBundle(file)) {
				if file == "" {
					return fmt.Errorf("--archive requires --file")
				}
				return runDeviceImport(cmd, cfgPath, true, file, overwrite)
			}
			if file != "" && looksLikeJSONContext(file) {
				return runJSONContextImport(cmd, cfgPath, useGlobal, file, overwrite)
			}
			ociCfgPath := file
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
					AuthMethod:      config.AuthMethodAPIKey,
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
	cmd.Flags().StringVarP(&file, "file", "f", "", "OCI CLI config or device bundle to import")
	cmd.Flags().BoolVar(&archive, "archive", false, "Import --file as a device bundle")
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "w", false, "Overwrite existing contexts with same name")
	return cmd
}

func runJSONContextImport(parent *cobra.Command, cfgPath string, useGlobal bool, input string, overwrite bool) error {
	path, err := resolveConfigPath(cfgPath, useGlobal)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read context JSON: %w", err)
	}
	var view exportContextView
	if err := json.Unmarshal(data, &view); err != nil {
		return fmt.Errorf("parse context JSON: %w", err)
	}
	if view.Name == "" {
		var bundle deviceBundle
		if err := json.Unmarshal(data, &bundle); err == nil && bundle.Context.Name != "" {
			view.Context = bundle.Context
		}
	}
	if err := view.Context.Validate(); err != nil {
		return fmt.Errorf("invalid context JSON: %w", err)
	}
	if !overwrite {
		if _, err := cfg.GetContext(view.Name); err == nil {
			return fmt.Errorf("context %s already exists; use --overwrite", view.Name)
		}
	}
	if err := cfg.UpsertContext(view.Context); err != nil {
		return err
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(parent.OutOrStdout(), "Imported context %s from %s\n", view.Name, input)
	return nil
}

func looksLikeJSONContext(input string) bool {
	info, err := os.Stat(input)
	if err != nil || info.IsDir() {
		return false
	}
	b, err := os.ReadFile(input)
	if err != nil {
		return false
	}
	return len(b) > 0 && b[0] == '{' || filepath.Ext(input) == ".json"
}
