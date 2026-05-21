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
	var withAuth bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Bootstrap oci-context (config + daemon)",
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

			if withAuth {
				if commandNoInteractive(cmd) {
					return interactiveDisabledError()
				}
				if err := runSetupAuth(cmd, path, contextName); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Skipping auth setup (use --with-auth to enable).")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context for auth setup (default current)")
	cmd.Flags().BoolVar(&noDaemon, "no-daemon", false, "Skip daemon setup")
	cmd.Flags().BoolVar(&withAuth, "with-auth", false, "Also run auth setup for current/selected context")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print underlying system commands as they run")
	cmd.AddCommand(newSetupDaemonCmd())
	return cmd
}

func newSetupDaemonCmd() *cobra.Command {
	var (
		cfgPath        string
		useGlobal      bool
		label          string
		ociContextBin  string
		monitors       []string
		all            bool
		noLaunchd      bool
		noSleepwatcher bool
		noHammerspoon  bool
		recoverNow     bool
		output         string
	)
	cmd := &cobra.Command{
		Use:   "daemon [context...]",
		Short: "Install daemon integrations, monitor contexts, and recover daemon health",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			monitors = append(monitors, args...)
			return runDaemonRepairCmd(cmd, daemonRepairOptions{
				cfgPath:        path,
				label:          label,
				ociContextBin:  ociContextBin,
				monitors:       monitors,
				all:            all,
				noLaunchd:      noLaunchd,
				noSleepwatcher: noSleepwatcher,
				noHammerspoon:  noHammerspoon,
				recoverNow:     recoverNow,
				output:         output,
			})
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVar(&label, "label", daemonLaunchdDefaultLabel, "launchd label")
	cmd.Flags().StringVar(&ociContextBin, "oci-context-bin", "", "Absolute path to oci-context binary for installed integrations")
	cmd.Flags().StringVar(&ociContextBin, "binary", "", "Alias for --oci-context-bin")
	cmd.Flags().StringArrayVar(&monitors, "monitor", nil, "Context to add to daemon monitoring list (repeatable)")
	cmd.Flags().BoolVar(&all, "all", false, "Install launchd, sleepwatcher, and Hammerspoon integrations")
	cmd.Flags().BoolVar(&noLaunchd, "no-launchd", false, "Skip launchd install")
	cmd.Flags().BoolVar(&noSleepwatcher, "no-sleepwatcher", false, "Skip sleepwatcher install")
	cmd.Flags().BoolVar(&noHammerspoon, "no-hammerspoon", false, "Skip Hammerspoon install")
	cmd.Flags().BoolVar(&recoverNow, "recover", true, "Recover/restart daemon after installation")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
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
		return runOCI(cmd, []string{"setup", "config", "--profile", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath})
	case config.AuthMethodInstancePrincipal:
		return runOCI(cmd, []string{"setup", "instance-principal"})
	default:
		return fmt.Errorf("no setup flow for auth method %s", method)
	}
}
