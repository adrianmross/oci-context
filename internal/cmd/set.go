package cmd

import (
	"fmt"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func newSetCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var region, profile, tenancy, compartment, user, notes string

	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Update fields of a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			useGlobal, err := cmd.Flags().GetBool("global")
			if err != nil {
				return err
			}
			name := args[0]
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			ctx, err := cfg.GetContext(name)
			if err != nil {
				return err
			}
			if region != "" {
				ctx.Region = region
			}
			if profile != "" {
				ctx.Profile = profile
			}
			if tenancy != "" {
				ctx.TenancyOCID = tenancy
			}
			if compartment != "" {
				ctx.CompartmentOCID = compartment
			}
			if user != "" {
				ctx.User = user
			}
			if notes != "" {
				ctx.Notes = notes
			}
			if err := cfg.UpsertContext(ctx); err != nil {
				return err
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated context %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVarP(&region, "region", "r", "", "OCI region")
	cmd.Flags().StringVarP(&profile, "profile", "p", "", "OCI CLI profile")
	cmd.Flags().StringVarP(&tenancy, "tenancy", "t", "", "Tenancy OCID")
	cmd.Flags().StringVarP(&compartment, "compartment", "m", "", "Compartment OCID")
	cmd.Flags().StringVarP(&user, "user", "u", "", "User hint")
	cmd.Flags().StringVarP(&notes, "notes", "N", "", "Notes")

	return cmd
}
