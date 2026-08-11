package cmd

import (
	"fmt"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func newUseCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool

	cmd := &cobra.Command{
		Use:   "use <name>",
		Short: "Switch current context",
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
			if _, err := cfg.GetContext(name); err != nil {
				return err
			}
			cfg.CurrentContext = name
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if err := syncOCIDefaultsForCurrent(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), `Run: eval "$(oci-context export --format env)"`)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	return cmd
}
