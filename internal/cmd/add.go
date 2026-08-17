package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/adrianmross/oci-context/pkg/ocicfg"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var cfgPath string
	var ctx config.Context

	cmd := &cobra.Command{
		Use:     "create [name]",
		Aliases: []string{"add"},
		Short:   "Create or update a context from an OCI profile",
		Args:    cobra.MaximumNArgs(1),
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
			if len(args) == 1 {
				if ctx.Name != "" && ctx.Name != args[0] {
					return fmt.Errorf("context name provided twice: %s and %s", ctx.Name, args[0])
				}
				ctx.Name = args[0]
			}
			if err := fillContextFromProfile(&ctx, cfg); err != nil {
				return err
			}
			if err := ctx.Validate(); err != nil {
				return err
			}
			if err := cfg.UpsertContext(ctx); err != nil {
				return err
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if err := syncOCIDefaultsForCurrent(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created/updated context %s\n", ctx.Name)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVarP(&ctx.Name, "name", "n", "", "Context name")
	cmd.Flags().StringVarP(&ctx.Profile, "profile", "p", "", "OCI CLI profile (default: options.default_profile, then DEFAULT)")
	cmd.Flags().StringVarP(&ctx.AuthMethod, "auth-method", "a", config.AuthMethodAPIKey, "OCI auth method (api_key|security_token|instance_principal|resource_principal|instance_obo_user|oke_workload_identity)")
	cmd.Flags().StringVarP(&ctx.TenancyOCID, "tenancy", "t", "", "Tenancy OCID")
	cmd.Flags().StringVarP(&ctx.CompartmentOCID, "compartment", "m", "", "Compartment OCID")
	cmd.Flags().StringVarP(&ctx.Region, "region", "r", "", "OCI region")
	cmd.Flags().StringVarP(&ctx.User, "user", "u", "", "User hint")
	cmd.Flags().StringVarP(&ctx.Notes, "notes", "N", "", "Notes")

	return cmd
}

func fillContextFromProfile(ctx *config.Context, cfg config.Config) error {
	if ctx.Profile == "" || ctx.TenancyOCID == "" || ctx.Region == "" || ctx.User == "" {
		ociConfigPath := cfg.Options.OCIConfigPath
		if ociConfigPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			ociConfigPath = filepath.Join(home, ".oci", "config")
		}
		profiles, err := ocicfg.LoadProfiles(ociConfigPath)
		if err != nil {
			return fmt.Errorf("load OCI profiles: %w", err)
		}
		profileName := ctx.Profile
		if profileName == "" {
			profileName = cfg.Options.DefaultProfile
		}
		if profileName == "" {
			profileName = "DEFAULT"
		}
		profile, ok := profiles[profileName]
		if !ok && cfg.CurrentContext != "" {
			if current, currentErr := cfg.GetContext(cfg.CurrentContext); currentErr == nil && current.Profile != "" {
				profileName = current.Profile
				profile, ok = profiles[profileName]
			}
		}
		if !ok {
			return fmt.Errorf("OCI profile %q not found in %s", profileName, ociConfigPath)
		}
		ctx.Profile = profileName
		if ctx.TenancyOCID == "" {
			ctx.TenancyOCID = profile.Tenancy
		}
		if ctx.Region == "" {
			ctx.Region = profile.Region
		}
		if ctx.User == "" {
			ctx.User = profile.User
		}
	}
	if ctx.CompartmentOCID == "" {
		ctx.CompartmentOCID = ctx.TenancyOCID
	}
	ctx.AuthMethod = config.NormalizeAuthMethod(ctx.AuthMethod)
	ctx.Profile = strings.TrimSpace(ctx.Profile)
	return nil
}
