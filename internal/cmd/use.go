package cmd

import (
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
			return config.Save(path, cfg)
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	return cmd
}
