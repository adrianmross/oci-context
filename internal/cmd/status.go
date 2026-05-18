package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/adrianmross/oci-context/pkg/oci"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// fetchIdentity is a seam to allow testing without hitting the network.
var fetchIdentity = oci.FetchIdentityDetails

func newStatusCmd() *cobra.Command {
	var useGlobal bool
	var cfgPath string
	var output string
	var plain bool
	var noLookup bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current context details (friendly names)",
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
			resp := map[string]string{
				"context":        ctx.Name,
				"profile":        ctx.Profile,
				"auth_method":    config.NormalizeAuthMethod(ctx.AuthMethod),
				"tenancy":        "",
				"tenancy_id":     ctx.TenancyOCID,
				"compartment":    "",
				"compartment_id": ctx.CompartmentOCID,
				"user":           "",
				"user_id":        ctx.User,
				"region":         ctx.Region,
			}
			if !noLookup {
				ctxTimeout, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
				defer cancel()
				details, err := fetchIdentity(ctxTimeout, cfg.Options.OCIConfigPath, ctx.Profile, ctx.Region, ctx.TenancyOCID, ctx.CompartmentOCID, ctx.User)
				if err != nil {
					return err
				}
				resp["tenancy"] = details.TenancyName
				resp["tenancy_id"] = details.TenancyOCID
				resp["compartment"] = details.CompartmentName
				resp["compartment_id"] = details.CompartmentOCID
				resp["user"] = details.UserName
				resp["user_id"] = details.UserOCID
				resp["region"] = details.Region
			}
			if plain {
				line := fmt.Sprintf(
					"context=%s profile=%s auth=%s tenancy=%s compartment=%s user=%s region=%s",
					resp["context"], resp["profile"], resp["auth_method"], resp["tenancy_id"], resp["compartment_id"], resp["user_id"], resp["region"],
				)
				fmt.Fprintln(cmd.OutOrStdout(), line)
				return nil
			}
			switch strings.ToLower(output) {
			case "":
				// default human-friendly multiline
				fmt.Fprintf(cmd.OutOrStdout(), "context: %s\n", resp["context"])
				if resp["context"] != resp["profile"] {
					fmt.Fprintf(cmd.OutOrStdout(), "profile: %s\n", resp["profile"])
				}
				fmt.Fprintf(cmd.OutOrStdout(), "auth: %s\n", resp["auth_method"])
				printNameAndID := func(label, name, id string) {
					if name == "" {
						fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", label, id)
						return
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s (%s)\n", label, name, id)
				}
				printNameAndID("tenancy", resp["tenancy"], resp["tenancy_id"])
				printNameAndID("compartment", resp["compartment"], resp["compartment_id"])
				printNameAndID("user", resp["user"], resp["user_id"])
				fmt.Fprintf(cmd.OutOrStdout(), "region: %s\n", resp["region"])
				return nil
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			case "yaml", "yml":
				enc := yaml.NewEncoder(cmd.OutOrStdout())
				defer enc.Close()
				return enc.Encode(resp)
			case "plain":
				profilePart := ""
				if resp["context"] != resp["profile"] {
					profilePart = fmt.Sprintf(" profile=%s", resp["profile"])
				}
				line := fmt.Sprintf(
					"context=%s%s auth=%s tenancy=%s compartment=%s user=%s region=%s",
					resp["context"], profilePart,
					resp["auth_method"],
					formatStatusPlainValue(resp["tenancy"], resp["tenancy_id"]),
					formatStatusPlainValue(resp["compartment"], resp["compartment_id"]),
					formatStatusPlainValue(resp["user"], resp["user_id"]),
					resp["region"],
				)
				fmt.Fprintln(cmd.OutOrStdout(), line)
				return nil
			default:
				return fmt.Errorf("unsupported output format: %s", output)
			}
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVarP(&output, "out", "o", "", "Output format: json|yaml|plain (default: human-readable)")
	cmd.Flags().BoolVarP(&plain, "plain", "p", false, "Plain IDs only (OCIDs, no names)")
	cmd.Flags().BoolVar(&noLookup, "cached", false, "Read config/current context only; do not query OCI identity")
	cmd.Flags().BoolVar(&noLookup, "no-lookup", false, "Read config/current context only; do not query OCI identity")
	return cmd
}

func formatStatusPlainValue(name, id string) string {
	if name == "" {
		return id
	}
	return fmt.Sprintf("%s (%s)", name, abbrevOCID(id))
}
