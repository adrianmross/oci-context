package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
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
	cmd.AddCommand(newDaemonServeCmd())
	cmd.AddCommand(newDaemonAuthStatusCmd())
	cmd.AddCommand(newDaemonMonitorCmd())
	cmd.AddCommand(newDaemonLaunchdCmd())
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
