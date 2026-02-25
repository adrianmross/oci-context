package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize oci-context config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfgPath == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				cfgPath = filepath.Join(home, ".oci-context", "config.yml")
			}
			if err := config.EnsureDefaultConfig(cfgPath); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Initialized config at %s\n", cfgPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	return cmd
}
