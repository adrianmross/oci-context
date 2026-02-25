package cmd

import (
	"fmt"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func newCurrentCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool

	cmd := &cobra.Command{
		Use:   "current",
		Short: "Show the current context name",
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
			fmt.Fprintln(cmd.OutOrStdout(), ctx.Name)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	return cmd
}

// abbrevOCID shortens an OCID for display.
func abbrevOCID(s string) string {
	if len(s) <= 16 {
		return s
	}
	return fmt.Sprintf("%s…%s", s[:6], s[len(s)-6:])
}
