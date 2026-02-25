package cmd

import (
	"fmt"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var cfgPath string
	var ctx config.Context

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update a context",
		RunE: func(cmd *cobra.Command, args []string) error {
			useGlobal, err := cmd.Flags().GetBool("global")
			if err != nil {
				return err
			}
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			if err := ctx.Validate(); err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if err := cfg.UpsertContext(ctx); err != nil {
				return err
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added/updated context %s\n", ctx.Name)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVarP(&ctx.Name, "name", "n", "", "Context name")
	cmd.Flags().StringVarP(&ctx.Profile, "profile", "p", "", "OCI CLI profile")
	cmd.Flags().StringVarP(&ctx.TenancyOCID, "tenancy", "t", "", "Tenancy OCID")
	cmd.Flags().StringVarP(&ctx.CompartmentOCID, "compartment", "m", "", "Compartment OCID")
	cmd.Flags().StringVarP(&ctx.Region, "region", "r", "", "OCI region")
	cmd.Flags().StringVarP(&ctx.User, "user", "u", "", "User hint")
	cmd.Flags().StringVarP(&ctx.Notes, "notes", "N", "", "Notes")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("profile")
	_ = cmd.MarkFlagRequired("tenancy")
	_ = cmd.MarkFlagRequired("compartment")

	return cmd
}
