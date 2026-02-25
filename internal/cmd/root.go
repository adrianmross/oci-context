package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "oci-context",
		Short:         "Manage OCI contexts (profile, tenancy, compartment, region)",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Persistent flags for defaults (not the context values themselves)
	pf := cmd.PersistentFlags()
	pf.String("config", "", "Path to config file (default project .oci-context.yml else $HOME/.oci-context/config.yml)")
	pf.BoolP("global", "g", false, "Force use of global config (~/.oci-context/config.yml)")

	// Subcommands
	cmd.AddCommand(
		newInitCmd(),
		newListCmd(),
		newCurrentCmd(),
		newUseCmd(),
		newAddCmd(),
		newSetCmd(),
		newDeleteCmd(),
		newStatusCmd(),
		newExportCmd(),
		newImportCmd(),
		newDaemonCmd(),
		newTuiCmd(),
	)

	return cmd
}

// Execute runs the CLI.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ExecuteDaemon runs the daemon entrypoint.
func ExecuteDaemon() {
	if err := newDaemonServeCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
