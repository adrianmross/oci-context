package cmd

import (
	"fmt"

	"github.com/adrianmross/oci-context/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the oci-context daemon",
	}
	cmd.AddCommand(newDaemonServeCmd())
	return cmd
}

func newDaemonServeCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the oci-context daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := daemon.EnsureConfig(cfgPath)
			if err != nil {
				return err
			}
			svc, err := daemon.NewService(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Starting daemon with config %s\n", path)
			return svc.Serve()
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	return cmd
}
