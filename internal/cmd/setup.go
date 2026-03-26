package cmd

import (
	"fmt"

	"github.com/adrianmross/oci-context/internal/daemon"
	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var contextName string
	var noDaemon bool
	var noAuth bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap oci-context (config + daemon + auth)",
		RunE: func(cmd *cobra.Command, args []string) error {
			daemonVerbose = verbose
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			path, err = daemon.EnsureConfig(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Config ready: %s\n", path)

			if !noDaemon {
				if err := runDaemonInstallDefault(cmd.OutOrStdout(), path); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Skipping daemon setup (--no-daemon).")
			}

			if !noAuth {
				if err := runSetupAuth(cmd, path, contextName); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Skipping auth setup (--no-auth).")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context for auth setup (default current)")
	cmd.Flags().BoolVar(&noDaemon, "no-daemon", false, "Skip daemon setup")
	cmd.Flags().BoolVar(&noAuth, "no-auth", false, "Skip auth setup")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print underlying system commands as they run")
	return cmd
}

func runSetupAuth(cmd *cobra.Command, cfgPath, contextName string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	target := contextName
	if target == "" {
		target = cfg.CurrentContext
	}
	if target == "" {
		return fmt.Errorf("no current context set; run `oci-context use <context>` or pass --context")
	}
	ctx, err := cfg.GetContext(target)
	if err != nil {
		return err
	}
	method := config.NormalizeAuthMethod(ctx.AuthMethod)
	fmt.Fprintf(cmd.OutOrStdout(), "Running auth setup for context=%s method=%s\n", ctx.Name, method)
	switch method {
	case config.AuthMethodAPIKey, config.AuthMethodSecurityToken:
		return runOCI(cmd, []string{"setup", "config", "--profile-name", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath})
	case config.AuthMethodInstancePrincipal:
		return runOCI(cmd, []string{"setup", "instance-principal"})
	default:
		return fmt.Errorf("no setup flow for auth method %s", method)
	}
}
