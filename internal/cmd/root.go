package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func buildVersionString() string {
	parts := []string{version}
	if commit != "" && commit != "none" {
		parts = append(parts, "commit="+commit)
	}
	if date != "" && date != "unknown" {
		parts = append(parts, "built="+date)
	}
	return strings.Join(parts, " ")
}

func newRootCmd() *cobra.Command {
	var showVersion bool

	cmd := &cobra.Command{
		Use:           "oci-context",
		Short:         "Manage OCI contexts (profile, tenancy, compartment, region)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), buildVersionString())
				return err
			}
			return cmd.Help()
		},
	}

	cmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version and exit")

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
