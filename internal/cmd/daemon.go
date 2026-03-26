package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adrianmross/oci-context/internal/daemon"
	"github.com/adrianmross/oci-context/pkg/config"
	ipcmsg "github.com/adrianmross/oci-context/pkg/ipc"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the oci-context daemon",
	}
	cmd.AddCommand(newDaemonInstallCmd())
	cmd.AddCommand(newDaemonServeCmd())
	cmd.AddCommand(newDaemonAuthStatusCmd())
	cmd.AddCommand(newDaemonNudgeCmd())
	cmd.AddCommand(newDaemonMonitorCmd())
	cmd.AddCommand(newDaemonLaunchdCmd())
	cmd.AddCommand(newDaemonSleepwatcherCmd())
	cmd.AddCommand(newDaemonHammerspoonCmd())
	return cmd
}

func newDaemonInstallCmd() *cobra.Command {
	var cfgPath string
	var label string
	var binaryPath string
	var outPath string
	var stdoutPath string
	var stderrPath string
	var autoRefresh bool
	var validateInterval time.Duration
	var refreshInterval time.Duration
	var loadNow bool
	var kickstart bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install/refresh launchd daemon service and restart it",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("daemon install is only supported on macOS")
			}

			path, err := daemon.EnsureConfig(cfgPath)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			if binaryPath == "" {
				if bin, lookErr := exec.LookPath("oci-context"); lookErr == nil {
					binaryPath = bin
				}
			}
			if binaryPath == "" {
				exe, exeErr := os.Executable()
				if exeErr == nil {
					binaryPath = exe
				}
			}
			if binaryPath == "" {
				return fmt.Errorf("could not resolve oci-context binary path; pass --binary")
			}
			if stdoutPath == "" {
				stdoutPath = filepath.Join(home, ".oci-context", "daemon.out.log")
			}
			if stderrPath == "" {
				stderrPath = filepath.Join(home, ".oci-context", "daemon.err.log")
			}
			if outPath == "" {
				outPath = filepath.Join(home, "Library", "LaunchAgents", label+".plist")
			}

			plist := renderLaunchdPlist(label, binaryPath, path, autoRefresh, validateInterval, refreshInterval, stdoutPath, stderrPath)
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o755); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(stderrPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(outPath, []byte(plist), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote launchd plist: %s\n", outPath)

			if !loadNow {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"Load with:\nlaunchctl unload %s 2>/dev/null || true\nlaunchctl load %s\nlaunchctl start %s\n",
					outPath,
					outPath,
					label,
				)
				return nil
			}

			_ = exec.Command("launchctl", "unload", outPath).Run()
			if out, err := exec.Command("launchctl", "load", outPath).CombinedOutput(); err != nil {
				return fmt.Errorf("launchctl load failed: %v: %s", err, strings.TrimSpace(string(out)))
			}
			if out, err := exec.Command("launchctl", "start", label).CombinedOutput(); err != nil {
				return fmt.Errorf("launchctl start failed: %v: %s", err, strings.TrimSpace(string(out)))
			}

			if kickstart {
				target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
				if out, err := exec.Command("launchctl", "kickstart", "-k", target).CombinedOutput(); err != nil {
					return fmt.Errorf("launchctl kickstart failed: %v: %s", err, strings.TrimSpace(string(out)))
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Loaded and restarted launchd job: %s\n", target)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Loaded and started launchd job: %s\n", label)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&label, "label", "com.adrianmross.oci-context.daemon", "launchd label")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Absolute path to oci-context binary")
	cmd.Flags().StringVar(&outPath, "out", "", "Output plist path (default ~/Library/LaunchAgents/<label>.plist)")
	cmd.Flags().StringVar(&stdoutPath, "stdout-log", "", "stdout log path")
	cmd.Flags().StringVar(&stderrPath, "stderr-log", "", "stderr log path")
	cmd.Flags().BoolVar(&autoRefresh, "auto-refresh", true, "Enable daemon auth validate/refresh loop")
	cmd.Flags().DurationVar(&validateInterval, "validate-interval", 5*time.Minute, "How often to validate auth")
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 15*time.Minute, "How often to refresh security-token auth")
	cmd.Flags().BoolVar(&loadNow, "load", true, "Load and start launchd agent after writing files")
	cmd.Flags().BoolVar(&kickstart, "kickstart", true, "Kickstart launchd job after load/start to force a process restart")
	return cmd
}

func newDaemonServeCmd() *cobra.Command {
	var cfgPath string
	var autoRefresh bool
	var validateInterval time.Duration
	var refreshInterval time.Duration
	var noRefreshOnValidateError bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the oci-context daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := daemon.EnsureConfig(cfgPath)
			if err != nil {
				return err
			}
			opts := daemon.DefaultServiceOptions()
			opts.AutoRefresh = autoRefresh
			opts.ValidateInterval = validateInterval
			opts.RefreshInterval = refreshInterval
			opts.RefreshOnValidateError = !noRefreshOnValidateError
			svc, err := daemon.NewServiceWithOptions(path, opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"Starting daemon with config %s (auto-refresh=%t validate-interval=%s refresh-interval=%s)\n",
				path,
				opts.AutoRefresh,
				opts.ValidateInterval,
				opts.RefreshInterval,
			)
			return svc.Serve()
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVar(&autoRefresh, "auto-refresh", false, "Enable daemon auth validate/refresh loop")
	cmd.Flags().DurationVar(&validateInterval, "validate-interval", 5*time.Minute, "How often to validate auth")
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 15*time.Minute, "How often to refresh security-token auth")
	cmd.Flags().BoolVar(&noRefreshOnValidateError, "no-refresh-on-validate-error", false, "Do not auto-refresh security-token on validate failure")
	return cmd
}

func newDaemonAuthStatusCmd() *cobra.Command {
	var cfgPath string
	var contextName string

	cmd := &cobra.Command{
		Use:   "auth-status",
		Short: "Show daemon runtime auth status for the current or specified context",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := daemon.EnsureConfig(cfgPath)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			conn, err := ipcmsg.Dial(cfg.Options.SocketPath)
			if err != nil {
				return fmt.Errorf("dial daemon socket %s: %w (is daemon running?)", cfg.Options.SocketPath, err)
			}
			defer conn.Close()
			req := ipcmsg.Request{Method: "auth_status", Name: contextName}
			if err := conn.SendRequest(req); err != nil {
				return err
			}
			var resp struct {
				OK    bool              `json:"ok"`
				Error string            `json:"error,omitempty"`
				Data  daemon.AuthStatus `json:"data,omitempty"`
			}
			if err := conn.ReadResponse(&resp); err != nil {
				return err
			}
			if !resp.OK {
				return errors.New(resp.Error)
			}
			b, err := json.MarshalIndent(resp.Data, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (default current)")
	return cmd
}

func newDaemonMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Manage contexts monitored by daemon auto-refresh",
	}
	cmd.AddCommand(newDaemonMonitorListCmd())
	cmd.AddCommand(newDaemonMonitorAddCmd())
	cmd.AddCommand(newDaemonMonitorRemoveCmd())
	cmd.AddCommand(newDaemonMonitorClearCmd())
	return cmd
}

func newDaemonMonitorListCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contexts configured for daemon monitoring",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadDaemonConfig(cfgPath)
			if err != nil {
				return err
			}
			if len(cfg.Options.DaemonContexts) == 0 {
				if cfg.CurrentContext == "" {
					fmt.Fprintln(cmd.OutOrStdout(), "(none)")
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "(none; daemon falls back to current context: %s)\n", cfg.CurrentContext)
				}
				return nil
			}
			for _, name := range cfg.Options.DaemonContexts {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	return cmd
}

func newDaemonMonitorAddCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "add <context> [context...]",
		Short: "Add one or more contexts to daemon monitoring list",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadDaemonConfig(cfgPath)
			if err != nil {
				return err
			}
			exists := make(map[string]bool, len(cfg.Options.DaemonContexts))
			for _, name := range cfg.Options.DaemonContexts {
				exists[name] = true
			}
			for _, name := range args {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				if _, err := cfg.GetContext(name); err != nil {
					return fmt.Errorf("context %q not found", name)
				}
				if exists[name] {
					continue
				}
				cfg.Options.DaemonContexts = append(cfg.Options.DaemonContexts, name)
				exists[name] = true
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Daemon monitor set: %s\n", strings.Join(cfg.Options.DaemonContexts, ", "))
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	return cmd
}

func newDaemonMonitorRemoveCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "remove <context> [context...]",
		Short: "Remove one or more contexts from daemon monitoring list",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadDaemonConfig(cfgPath)
			if err != nil {
				return err
			}
			remove := make(map[string]bool, len(args))
			for _, name := range args {
				remove[strings.TrimSpace(name)] = true
			}
			next := make([]string, 0, len(cfg.Options.DaemonContexts))
			for _, name := range cfg.Options.DaemonContexts {
				if remove[name] {
					continue
				}
				next = append(next, name)
			}
			cfg.Options.DaemonContexts = next
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if len(next) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon monitor list cleared; daemon will fall back to current context.")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Daemon monitor set: %s\n", strings.Join(next, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	return cmd
}

func newDaemonMonitorClearCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear daemon monitoring list (fallback to current context)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadDaemonConfig(cfgPath)
			if err != nil {
				return err
			}
			cfg.Options.DaemonContexts = nil
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Daemon monitor list cleared.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	return cmd
}

func newDaemonLaunchdCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "launchd",
		Short: "Generate/install launchd configuration for daemon",
	}
	cmd.AddCommand(newDaemonLaunchdGenerateCmd())
	return cmd
}

func newDaemonSleepwatcherCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sleepwatcher",
		Short: "Install wake automation that nudges daemon auth checks",
	}
	cmd.AddCommand(newDaemonSleepwatcherInstallCmd())
	return cmd
}

func newDaemonHammerspoonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hammerspoon",
		Short: "Install Hammerspoon auth notifications and wake automation",
	}
	cmd.AddCommand(newDaemonHammerspoonInstallCmd())
	return cmd
}

func newDaemonHammerspoonInstallCmd() *cobra.Command {
	var (
		cfgPath        string
		daemonLabel    string
		wakeupScript   string
		hammerspoonDir string
		ociContextBin  string
		reloadNow      bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install managed Hammerspoon handler and wake script",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("hammerspoon install is only supported on macOS")
			}
			path, err := daemon.EnsureConfig(cfgPath)
			if err != nil {
				return err
			}
			_ = path
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			if ociContextBin == "" {
				if p, lookErr := exec.LookPath("oci-context"); lookErr == nil {
					ociContextBin = p
				}
			}
			if ociContextBin == "" {
				exe, exeErr := os.Executable()
				if exeErr == nil {
					ociContextBin = exe
				}
			}
			if ociContextBin == "" {
				return fmt.Errorf("could not resolve oci-context binary path; pass --oci-context-bin")
			}
			if wakeupScript == "" {
				wakeupScript = filepath.Join(home, ".wakeup")
			}
			if hammerspoonDir == "" {
				hammerspoonDir = filepath.Join(home, ".hammerspoon")
			}

			if err := os.MkdirAll(hammerspoonDir, 0o755); err != nil {
				return err
			}
			modulePath := filepath.Join(hammerspoonDir, "oci_context.lua")
			initPath := filepath.Join(hammerspoonDir, "init.lua")

			if err := os.WriteFile(modulePath, []byte(renderHammerspoonModule()), 0o644); err != nil {
				return err
			}
			if err := ensureHammerspoonInitLoadsModule(initPath); err != nil {
				return err
			}
			if err := os.WriteFile(wakeupScript, []byte(renderWakeupScriptWithHammerspoon(ociContextBin, daemonLabel)), 0o755); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Wrote Hammerspoon module: %s\n", modulePath)
			fmt.Fprintf(cmd.OutOrStdout(), "Updated Hammerspoon init: %s\n", initPath)
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote wake script: %s\n", wakeupScript)

			if reloadNow {
				if out, err := exec.Command("open", "-a", "Hammerspoon").CombinedOutput(); err != nil {
					return fmt.Errorf("failed to launch Hammerspoon: %v: %s", err, strings.TrimSpace(string(out)))
				}
				if out, err := exec.Command("open", "-g", "hammerspoon://reloadConfig").CombinedOutput(); err != nil {
					return fmt.Errorf("failed to reload Hammerspoon config: %v: %s", err, strings.TrimSpace(string(out)))
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Hammerspoon launched and config reloaded.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Reload Hammerspoon config with: open -g 'hammerspoon://reloadConfig'")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&daemonLabel, "daemon-label", "com.adrianmross.oci-context.daemon", "launchd label for oci-context daemon")
	cmd.Flags().StringVar(&wakeupScript, "wakeup-script", "", "Path to write wake hook script (default ~/.wakeup)")
	cmd.Flags().StringVar(&hammerspoonDir, "hammerspoon-dir", "", "Path to Hammerspoon config directory (default ~/.hammerspoon)")
	cmd.Flags().StringVar(&ociContextBin, "oci-context-bin", "", "Absolute path to oci-context binary")
	cmd.Flags().BoolVar(&reloadNow, "reload", true, "Launch Hammerspoon and reload config after writing files")
	return cmd
}

func newNotifyCmd(use, short string) *cobra.Command {
	var (
		cfgPath      string
		contextName  string
		reason       string
		profile      string
		region       string
		tenancyName  string
		nativeNotify bool
		printOnlyURL bool
	)
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("hammerspoon notify is only supported on macOS")
			}
			ctxName := strings.TrimSpace(contextName)
			prof := strings.TrimSpace(profile)
			reg := strings.TrimSpace(region)
			tenancy := strings.TrimSpace(tenancyName)
			if ctxName == "" || prof == "" || reg == "" || tenancy == "" {
				cfg, _, err := loadDaemonConfig(cfgPath)
				if err != nil {
					return err
				}
				if ctxName == "" {
					ctxName = cfg.CurrentContext
				}
				if ctxName != "" {
					ctx, err := cfg.GetContext(ctxName)
					if err != nil {
						return err
					}
					if prof == "" {
						prof = ctx.Profile
					}
					if reg == "" {
						reg = ctx.Region
					}
					if tenancy == "" {
						tenancy = strings.TrimSpace(ctx.TenancyOCID)
					}
				}
			}
			if ctxName == "" {
				ctxName = "current"
			}
			if prof == "" {
				prof = "DEFAULT"
			}
			eventURL := buildHammerspoonAuthNeededURL(prof, ctxName, reg, reason, tenancy)
			if printOnlyURL {
				fmt.Fprintln(cmd.OutOrStdout(), eventURL)
				return nil
			}
			if out, err := exec.Command("open", "-g", eventURL).CombinedOutput(); err != nil {
				return fmt.Errorf("failed to trigger hammerspoon event: %v: %s", err, strings.TrimSpace(string(out)))
			}
			if nativeNotify {
				msg := fmt.Sprintf("Auth check for %s (%s)", ctxName, prof)
				if strings.TrimSpace(reason) != "" {
					msg = fmt.Sprintf("%s\n%s", msg, reason)
				}
				if err := sendNativeAuthNotification(eventURL, msg, "oci-context", "Remote trigger"); err != nil {
					return fmt.Errorf("hammerspoon event sent but native notification failed: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Triggered Hammerspoon notification for context=%s profile=%s region=%s\n", ctxName, prof, reg)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&contextName, "context", "", "Context name (default current)")
	cmd.Flags().StringVar(&reason, "reason", "manual", "Reason text shown in notification")
	cmd.Flags().StringVar(&profile, "profile", "", "Override profile passed to Hammerspoon event")
	cmd.Flags().StringVar(&region, "region", "", "Override region passed to Hammerspoon event")
	cmd.Flags().StringVar(&tenancyName, "tenancy-name", "", "Optional tenancy override passed to session authenticate (default: selected context tenancy_ocid)")
	cmd.Flags().StringVar(&tenancyName, "tenancy", "", "Alias for --tenancy-name")
	cmd.Flags().BoolVar(&nativeNotify, "native-notify", false, "Also send a native macOS notification (terminal-notifier when available, otherwise osascript)")
	cmd.Flags().BoolVar(&printOnlyURL, "print-url", false, "Print event URL instead of opening it")
	return cmd
}

func newDaemonSleepwatcherInstallCmd() *cobra.Command {
	var (
		cfgPath         string
		daemonLabel     string
		wakeupScript    string
		sleepwatcherPl  string
		sleepwatcherBin string
		ociContextBin   string
		loadNow         bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Create wake hook script and launchd plist for sleepwatcher",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("sleepwatcher install is only supported on macOS")
			}
			path, err := daemon.EnsureConfig(cfgPath)
			if err != nil {
				return err
			}
			_ = path
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			if sleepwatcherBin == "" {
				if p, lookErr := exec.LookPath("sleepwatcher"); lookErr == nil {
					sleepwatcherBin = p
				}
			}
			if sleepwatcherBin == "" {
				return fmt.Errorf("sleepwatcher binary not found; install with `brew install sleepwatcher` or pass --sleepwatcher-bin")
			}
			if ociContextBin == "" {
				if p, lookErr := exec.LookPath("oci-context"); lookErr == nil {
					ociContextBin = p
				}
			}
			if ociContextBin == "" {
				exe, exeErr := os.Executable()
				if exeErr == nil {
					ociContextBin = exe
				}
			}
			if ociContextBin == "" {
				return fmt.Errorf("could not resolve oci-context binary path; pass --oci-context-bin")
			}
			if wakeupScript == "" {
				wakeupScript = filepath.Join(home, ".wakeup")
			}
			if sleepwatcherPl == "" {
				sleepwatcherPl = filepath.Join(home, "Library", "LaunchAgents", "com.adrianmross.oci-context.sleepwatcher.plist")
			}

			script := renderWakeupScript(ociContextBin, daemonLabel)
			if err := os.WriteFile(wakeupScript, []byte(script), 0o755); err != nil {
				return err
			}
			plist := renderSleepwatcherPlist("com.adrianmross.oci-context.sleepwatcher", sleepwatcherBin, wakeupScript)
			if err := os.MkdirAll(filepath.Dir(sleepwatcherPl), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(sleepwatcherPl, []byte(plist), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote wake script: %s\n", wakeupScript)
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote launchd plist: %s\n", sleepwatcherPl)
			if loadNow {
				_ = exec.Command("launchctl", "unload", sleepwatcherPl).Run()
				if out, err := exec.Command("launchctl", "load", sleepwatcherPl).CombinedOutput(); err != nil {
					return fmt.Errorf("launchctl load failed: %v: %s", err, strings.TrimSpace(string(out)))
				}
				if out, err := exec.Command("launchctl", "start", "com.adrianmross.oci-context.sleepwatcher").CombinedOutput(); err != nil {
					return fmt.Errorf("launchctl start failed: %v: %s", err, strings.TrimSpace(string(out)))
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Loaded and started sleepwatcher launch agent.")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Load with:\nlaunchctl unload %s 2>/dev/null || true\nlaunchctl load %s\nlaunchctl start com.adrianmross.oci-context.sleepwatcher\n", sleepwatcherPl, sleepwatcherPl)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&daemonLabel, "daemon-label", "com.adrianmross.oci-context.daemon", "launchd label for oci-context daemon")
	cmd.Flags().StringVar(&wakeupScript, "wakeup-script", "", "Path to write wake hook script (default ~/.wakeup)")
	cmd.Flags().StringVar(&sleepwatcherPl, "plist", "", "Path to write sleepwatcher launchd plist")
	cmd.Flags().StringVar(&sleepwatcherBin, "sleepwatcher-bin", "", "Absolute path to sleepwatcher binary")
	cmd.Flags().StringVar(&ociContextBin, "oci-context-bin", "", "Absolute path to oci-context binary")
	cmd.Flags().BoolVar(&loadNow, "load", true, "Load and start launchd agent after writing files")
	return cmd
}

func newDaemonLaunchdGenerateCmd() *cobra.Command {
	var cfgPath string
	var label string
	var binaryPath string
	var outPath string
	var stdoutPath string
	var stderrPath string
	var autoRefresh bool
	var validateInterval time.Duration
	var refreshInterval time.Duration

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a launchd plist for running daemon in background",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "darwin" {
				return fmt.Errorf("launchd generation is only supported on macOS")
			}
			path, err := daemon.EnsureConfig(cfgPath)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			if binaryPath == "" {
				if bin, lookErr := exec.LookPath("oci-context"); lookErr == nil {
					binaryPath = bin
				}
			}
			if binaryPath == "" {
				exe, exeErr := os.Executable()
				if exeErr == nil {
					binaryPath = exe
				}
			}
			if binaryPath == "" {
				return fmt.Errorf("could not resolve oci-context binary path; pass --binary")
			}
			if stdoutPath == "" {
				stdoutPath = filepath.Join(home, ".oci-context", "daemon.out.log")
			}
			if stderrPath == "" {
				stderrPath = filepath.Join(home, ".oci-context", "daemon.err.log")
			}
			if outPath == "" {
				outPath = filepath.Join(home, "Library", "LaunchAgents", label+".plist")
			}
			plist := renderLaunchdPlist(label, binaryPath, path, autoRefresh, validateInterval, refreshInterval, stdoutPath, stderrPath)
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(outPath, []byte(plist), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", outPath)
			fmt.Fprintf(cmd.OutOrStdout(), "Load with:\nlaunchctl unload %s 2>/dev/null || true\nlaunchctl load %s\nlaunchctl start %s\n", outPath, outPath, label)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&label, "label", "com.adrianmross.oci-context.daemon", "launchd label")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Absolute path to oci-context binary")
	cmd.Flags().StringVar(&outPath, "out", "", "Output plist path (default ~/Library/LaunchAgents/<label>.plist)")
	cmd.Flags().StringVar(&stdoutPath, "stdout-log", "", "stdout log path")
	cmd.Flags().StringVar(&stderrPath, "stderr-log", "", "stderr log path")
	cmd.Flags().BoolVar(&autoRefresh, "auto-refresh", true, "Enable daemon auth validate/refresh loop")
	cmd.Flags().DurationVar(&validateInterval, "validate-interval", 5*time.Minute, "How often to validate auth")
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 15*time.Minute, "How often to refresh security-token auth")
	return cmd
}

func loadDaemonConfig(cfgPath string) (config.Config, string, error) {
	path, err := daemon.EnsureConfig(cfgPath)
	if err != nil {
		return config.Config{}, "", err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, "", err
	}
	return cfg, path, nil
}

func newDaemonNudgeCmd() *cobra.Command {
	var cfgPath string
	var contextName string
	cmd := &cobra.Command{
		Use:   "nudge",
		Short: "Trigger immediate daemon auth maintenance for monitored or selected context",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := loadDaemonConfig(cfgPath)
			if err != nil {
				return err
			}
			conn, err := ipcmsg.Dial(cfg.Options.SocketPath)
			if err != nil {
				return fmt.Errorf("dial daemon socket %s: %w (is daemon running?)", cfg.Options.SocketPath, err)
			}
			defer conn.Close()
			req := ipcmsg.Request{Method: "auth_nudge", Name: contextName}
			if err := conn.SendRequest(req); err != nil {
				return err
			}
			var resp struct {
				OK    bool        `json:"ok"`
				Error string      `json:"error,omitempty"`
				Data  interface{} `json:"data,omitempty"`
			}
			if err := conn.ReadResponse(&resp); err != nil {
				return err
			}
			if !resp.OK {
				return errors.New(resp.Error)
			}
			b, err := json.MarshalIndent(resp.Data, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (default monitored list)")
	return cmd
}

func renderLaunchdPlist(label, binaryPath, cfgPath string, autoRefresh bool, validateInterval, refreshInterval time.Duration, stdoutPath, stderrPath string) string {
	args := []string{
		xmlEscape(binaryPath),
		"daemon",
		"serve",
		"--config",
		xmlEscape(cfgPath),
	}
	if autoRefresh {
		args = append(args, "--auto-refresh")
	}
	args = append(args, "--validate-interval", validateInterval.String(), "--refresh-interval", refreshInterval.String())
	argXML := make([]string, 0, len(args))
	for _, a := range args {
		argXML = append(argXML, fmt.Sprintf("      <string>%s</string>", xmlEscape(a)))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
%s
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ProcessType</key>
    <string>Background</string>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
  </dict>
</plist>
`, xmlEscape(label), strings.Join(argXML, "\n"), xmlEscape(stdoutPath), xmlEscape(stderrPath))
}

func renderWakeupScript(ociContextBin, daemonLabel string) string {
	return fmt.Sprintf(`#!/bin/zsh
set -euo pipefail
launchctl kickstart -k gui/$(id -u)/%s >/dev/null 2>&1 || true
%s daemon nudge >/dev/null 2>&1 || true
`, daemonLabel, shellQuote(ociContextBin))
}

func ensureHammerspoonInitLoadsModule(initPath string) error {
	const marker1 = `pcall(require, "oci_context")`
	const marker2 = `dofile(hs.configdir .. "/oci_context.lua")`
	const snippet = `
-- oci-context managed block
local ok, modErr = pcall(require, "oci_context")
if not ok then
  hs.notify.new({
    title = "oci-context",
    informativeText = "Hammerspoon load error: " .. tostring(modErr),
    autoWithdraw = true,
  }):send()
end
-- end oci-context managed block
`
	b, err := os.ReadFile(initPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.WriteFile(initPath, []byte(strings.TrimLeft(snippet, "\n")), 0o644)
		}
		return err
	}
	if strings.Contains(string(b), marker1) || strings.Contains(string(b), marker2) {
		return nil
	}
	out := string(b)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += strings.TrimLeft(snippet, "\n")
	return os.WriteFile(initPath, []byte(out), 0o644)
}

func buildHammerspoonAuthNeededURL(profile, contextName, region, reason, tenancyName string) string {
	q := url.Values{}
	q.Set("profile", profile)
	q.Set("context", contextName)
	q.Set("reason", reason)
	if strings.TrimSpace(region) != "" {
		q.Set("region", strings.TrimSpace(region))
	}
	if strings.TrimSpace(tenancyName) != "" {
		q.Set("tenancy_name", strings.TrimSpace(tenancyName))
	}
	return "hammerspoon://oci-auth-needed?" + q.Encode()
}

func buildAppleScriptDisplayNotification(message, title, subtitle string) string {
	escape := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return s
	}
	return fmt.Sprintf(
		`display notification "%s" with title "%s" subtitle "%s"`,
		escape(message),
		escape(title),
		escape(subtitle),
	)
}

func buildTerminalNotifierArgs(eventURL, message, title, subtitle string) []string {
	return []string{
		"-title", title,
		"-subtitle", subtitle,
		"-message", message,
		"-open", eventURL,
	}
}

func sendNativeAuthNotification(eventURL, message, title, subtitle string) error {
	if tnPath, err := exec.LookPath("terminal-notifier"); err == nil {
		args := buildTerminalNotifierArgs(eventURL, message, title, subtitle)
		if out, err := exec.Command(tnPath, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("terminal-notifier failed: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	script := buildAppleScriptDisplayNotification(message, title, subtitle)
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		return fmt.Errorf("osascript failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func renderSleepwatcherPlist(label, sleepwatcherBin, wakeupScript string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
      <string>%s</string>
      <string>-w</string>
      <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ProcessType</key>
    <string>Background</string>
  </dict>
</plist>
`, xmlEscape(label), xmlEscape(sleepwatcherBin), xmlEscape(wakeupScript))
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func xmlEscape(s string) string {
	repl := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return repl.Replace(s)
}
