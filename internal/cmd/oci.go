package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func newOCICmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool

	cmd := &cobra.Command{
		Use:   "oci [--] <oci args...>",
		Short: "Run OCI CLI with current context defaults",
		Long:  "Executes the OCI CLI and injects current context defaults for --profile, --region, and --compartment-id when they are not already specified.",
		Args:  cobra.MinimumNArgs(1),
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

			finalArgs := buildOCIArgs(args, ctx, cfg.Options.OCIConfigPath)
			ociCmd := exec.CommandContext(cmd.Context(), "oci", finalArgs...)
			ociCmd.Stdin = cmd.InOrStdin()
			ociCmd.Stdout = cmd.OutOrStdout()
			ociCmd.Stderr = cmd.ErrOrStderr()

			if err := ociCmd.Run(); err != nil {
				if ee, ok := err.(*exec.Error); ok && ee.Err != nil {
					return fmt.Errorf("failed to execute oci CLI (%w): install with `pip install oci-cli` or ensure it is in PATH", ee.Err)
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	return cmd
}

func buildOCIArgs(args []string, ctx config.Context, ociConfigPath string) []string {
	out := make([]string, 0, len(args)+8)
	out = append(out, args...)

	if !hasOCIFlag(args, "--config-file", "") && ociConfigPath != "" {
		out = append(out, "--config-file", ociConfigPath)
	}
	if !hasOCIFlag(args, "--profile", "") && ctx.Profile != "" {
		out = append(out, "--profile", ctx.Profile)
	}
	authMethod := config.NormalizeAuthMethod(ctx.AuthMethod)
	if !hasOCIFlag(args, "--auth", "") && authMethod != "" {
		out = append(out, "--auth", authMethod)
	}
	if !hasOCIFlag(args, "--region", "") && ctx.Region != "" {
		out = append(out, "--region", ctx.Region)
	}
	if !hasOCIFlag(args, "--compartment-id", "-c") && ctx.CompartmentOCID != "" {
		out = append(out, "--compartment-id", ctx.CompartmentOCID)
	}

	return out
}

func hasOCIFlag(args []string, longFlag, shortFlag string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == longFlag || strings.HasPrefix(a, longFlag+"=") {
			return true
		}
		if shortFlag != "" && (a == shortFlag || strings.HasPrefix(a, shortFlag+"=")) {
			return true
		}
	}
	return false
}
