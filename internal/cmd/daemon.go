package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"gopkg.in/yaml.v3"
)

const daemonLaunchdDefaultLabel = "com.adrianmross.oci-context.daemon"

var daemonVerbose bool

type daemonCommandResult struct {
	OK    bool        `json:"ok" yaml:"ok"`
	Error string      `json:"error,omitempty" yaml:"error,omitempty"`
	Data  interface{} `json:"data,omitempty" yaml:"data,omitempty"`
}

type daemonDoctorResult struct {
	Healthy    bool                `json:"healthy" yaml:"healthy"`
	ConfigPath string              `json:"config_path" yaml:"config_path"`
	SocketPath string              `json:"socket_path" yaml:"socket_path"`
	Context    string              `json:"context,omitempty" yaml:"context,omitempty"`
	Launchd    daemonLaunchdStatus `json:"launchd" yaml:"launchd"`
	IPC        daemonIPCStatus     `json:"ipc" yaml:"ipc"`
	AuthStatus *daemon.AuthStatus  `json:"auth_status,omitempty" yaml:"auth_status,omitempty"`
	Issues     []string            `json:"issues,omitempty" yaml:"issues,omitempty"`
	Fixes      []string            `json:"fixes,omitempty" yaml:"fixes,omitempty"`
}

type daemonLaunchdStatus struct {
	Checked bool   `json:"checked" yaml:"checked"`
	Target  string `json:"target,omitempty" yaml:"target,omitempty"`
	State   string `json:"state,omitempty" yaml:"state,omitempty"`
	Error   string `json:"error,omitempty" yaml:"error,omitempty"`
	Detail  string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type daemonIPCStatus struct {
	Available bool   `json:"available" yaml:"available"`
	Error     string `json:"error,omitempty" yaml:"error,omitempty"`
}

type daemonRepairResult struct {
	OK         bool     `json:"ok" yaml:"ok"`
	ConfigPath string   `json:"config_path" yaml:"config_path"`
	Installed  []string `json:"installed,omitempty" yaml:"installed,omitempty"`
	Skipped    []string `json:"skipped,omitempty" yaml:"skipped,omitempty"`
	Monitored  []string `json:"monitored,omitempty" yaml:"monitored,omitempty"`
	Recovered  bool     `json:"recovered" yaml:"recovered"`
}

func printDaemonOutput(cmd *cobra.Command, output string, data interface{}, text func() error) error {
	switch strings.ToLower(output) {
	case "", "text":
		if text != nil {
			return text()
		}
		return nil
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case "yaml", "yml":
		enc := yaml.NewEncoder(cmd.OutOrStdout())
		defer enc.Close()
		return enc.Encode(data)
	default:
		return fmt.Errorf("unsupported output format: %s", output)
	}
}

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the oci-context daemon",
	}
	cmd.PersistentFlags().BoolVarP(&daemonVerbose, "verbose", "v", false, "Print underlying system commands as they run")
	cmd.AddCommand(newDaemonInstallCmd())
	cmd.AddCommand(newDaemonRepairCmd())
	cmd.AddCommand(newDaemonRecoverCmd())
	cmd.AddCommand(newDaemonUpCmd())
	cmd.AddCommand(newDaemonDoctorCmd())
	cmd.AddCommand(newDaemonServeCmd())
	cmd.AddCommand(newDaemonAuthStatusCmd())
	cmd.AddCommand(newDaemonNudgeCmd())
	cmd.AddCommand(newDaemonMonitorCmd())
	cmd.AddCommand(newDaemonLaunchdCmd())
	cmd.AddCommand(newDaemonSleepwatcherCmd())
	cmd.AddCommand(newDaemonHammerspoonCmd())
	return cmd
}

func newDaemonRecoverCmd() *cobra.Command {
	var cfgPath string
	var label string
	var contextName string
	var output string
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Restart launchd daemon and trigger immediate auth maintenance (macOS)",
		Aliases: []string{
			"fix",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonRecoverCmd(cmd, cfgPath, label, contextName, output)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&label, "label", daemonLaunchdDefaultLabel, "launchd label")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name for nudge (default monitored list)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
	return cmd
}

func newDaemonUpCmd() *cobra.Command {
	var cfgPath string
	var label string
	var contextName string
	var output string
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Restart daemon and trigger immediate auth maintenance (macOS)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonRecoverCmd(cmd, cfgPath, label, contextName, output)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&label, "label", daemonLaunchdDefaultLabel, "launchd label")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name for nudge (default monitored list)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
	return cmd
}

func runDaemonRecoverCmd(cmd *cobra.Command, cfgPath, label, contextName, output string) error {
	data, err := runDaemonRecover(cmd.OutOrStdout(), cfgPath, label, contextName)
	if err != nil {
		return err
	}
	result := daemonCommandResult{OK: true, Data: data}
	return printDaemonOutput(cmd, output, result, func() error {
		b, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Launchd daemon restarted and nudged.")
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
		return nil
	})
}

func runDaemonRecover(out io.Writer, cfgPath, label, contextName string) (interface{}, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("daemon recover is only supported on macOS")
	}
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	if cmdOut, err := runCombinedOutput(out, "launchctl", "kickstart", "-k", target); err != nil {
		return nil, fmt.Errorf("launchctl kickstart failed: %v: %s (run `oci-context daemon install` to reinstall/reload service)", err, strings.TrimSpace(string(cmdOut)))
	}

	cfg, _, err := loadDaemonConfig(cfgPath)
	if err != nil {
		return nil, err
	}
	conn, err := dialDaemonSocketWithRetry(out, cfg.Options.SocketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("daemon restarted but socket dial failed: %w", err)
	}
	defer conn.Close()
	req := ipcmsg.Request{Method: "auth_nudge", Name: contextName}
	if err := conn.SendRequest(req); err != nil {
		return nil, err
	}
	var resp daemonCommandResult
	if err := conn.ReadResponse(&resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, errors.New(resp.Error)
	}
	return resp.Data, nil
}

func newDaemonDoctorCmd() *cobra.Command {
	var cfgPath string
	var label string
	var contextName string
	var output string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose daemon health and suggest remediation steps",
		Aliases: []string{
			"check",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := buildDaemonDoctorResult(cmd, cfgPath, label, contextName)
			if err != nil {
				return err
			}
			if err := printDaemonOutput(cmd, output, result, func() error {
				printDaemonDoctorText(cmd, result)
				return nil
			}); err != nil {
				return err
			}
			if !result.Healthy {
				return fmt.Errorf("doctor found %d issue(s)", len(result.Issues))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&label, "label", daemonLaunchdDefaultLabel, "launchd label")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (default current)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
	return cmd
}

func buildDaemonDoctorResult(cmd *cobra.Command, cfgPath, label, contextName string) (daemonDoctorResult, error) {
	cfg, path, err := loadDaemonConfig(cfgPath)
	if err != nil {
		return daemonDoctorResult{}, err
	}
	if contextName == "" {
		contextName = cfg.CurrentContext
	}
	result := daemonDoctorResult{
		ConfigPath: path,
		SocketPath: cfg.Options.SocketPath,
		Context:    contextName,
		Healthy:    true,
	}

	if runtime.GOOS == "darwin" {
		target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
		result.Launchd = daemonLaunchdStatus{Checked: true, Target: target}
		out, err := runCombinedOutput(cmd.OutOrStdout(), "launchctl", "print", target)
		if err != nil {
			result.Healthy = false
			result.Launchd.Error = err.Error()
			result.Launchd.Detail = strings.TrimSpace(string(out))
			result.Issues = append(result.Issues, "launchd daemon is not healthy")
			result.Fixes = append(result.Fixes, "oci-context daemon install launchd")
		} else {
			result.Launchd.State = parseLaunchdState(string(out))
		}
	}

	conn, err := ipcmsg.Dial(cfg.Options.SocketPath)
	if err != nil {
		result.Healthy = false
		result.IPC.Error = err.Error()
		result.Issues = append(result.Issues, "daemon socket is unavailable")
		if runtime.GOOS == "darwin" {
			result.Fixes = append(result.Fixes, "oci-context daemon install launchd")
		} else {
			result.Fixes = append(result.Fixes, "oci-context daemon serve --auto-refresh")
		}
		return result, nil
	}
	defer conn.Close()
	result.IPC.Available = true

	req := ipcmsg.Request{Method: "auth_status", Name: contextName}
	if err := conn.SendRequest(req); err != nil {
		result.Healthy = false
		result.IPC.Error = err.Error()
		result.Issues = append(result.Issues, "daemon auth-status request failed")
		return result, nil
	}
	var resp struct {
		OK    bool              `json:"ok"`
		Error string            `json:"error,omitempty"`
		Data  daemon.AuthStatus `json:"data,omitempty"`
	}
	if err := conn.ReadResponse(&resp); err != nil {
		result.Healthy = false
		result.IPC.Error = err.Error()
		result.Issues = append(result.Issues, "daemon auth-status response failed")
		return result, nil
	}
	if !resp.OK {
		result.Healthy = false
		result.IPC.Error = resp.Error
		result.Issues = append(result.Issues, "daemon auth-status is unhealthy")
		return result, nil
	}
	status := daemon.FinalizeAuthStatus(resp.Data)
	result.AuthStatus = &status
	if status.ActionRequired {
		result.Healthy = false
		result.Issues = append(result.Issues, status.Reason)
		switch status.Action {
		case "login":
			result.Fixes = append(result.Fixes, fmt.Sprintf("oci-context auth login --context %s", status.ContextName))
		case "nudge":
			result.Fixes = append(result.Fixes, fmt.Sprintf("oci-context daemon nudge --context %s", status.ContextName))
		default:
			result.Fixes = append(result.Fixes, fmt.Sprintf("oci-context auth ensure --context %s", status.ContextName))
		}
	}
	return result, nil
}

func parseLaunchdState(s string) string {
	switch {
	case strings.Contains(s, "state = running"):
		return "running"
	case strings.Contains(s, "state = waiting"):
		return "waiting"
	default:
		return "unknown"
	}
}

func printDaemonDoctorText(cmd *cobra.Command, result daemonDoctorResult) {
	fmt.Fprintf(cmd.OutOrStdout(), "config: %s\n", result.ConfigPath)
	fmt.Fprintf(cmd.OutOrStdout(), "socket: %s\n", result.SocketPath)
	if result.Context != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "context: %s\n", result.Context)
	}
	if result.Launchd.Checked {
		if result.Launchd.Error != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "launchd: unhealthy (%s)\n", result.Launchd.Error)
			if result.Launchd.Detail != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "launchd detail: %s\n", result.Launchd.Detail)
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "launchd: %s (%s)\n", result.Launchd.Target, result.Launchd.State)
		}
	}
	if !result.IPC.Available {
		fmt.Fprintf(cmd.OutOrStdout(), "ipc: unhealthy (%s)\n", result.IPC.Error)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "ipc: healthy")
	}
	if result.AuthStatus != nil {
		st := result.AuthStatus
		fmt.Fprintf(cmd.OutOrStdout(), "auth-status: ready=%t action_required=%t action=%s severity=%s context=%s validate_ok=%t refresh_ok=%t\n", st.Ready, st.ActionRequired, st.Action, st.Severity, st.ContextName, st.LastValidateOK, st.LastRefreshOK)
		if st.Reason != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "auth-reason: %s\n", st.Reason)
		}
	}
	for _, fix := range result.Fixes {
		fmt.Fprintf(cmd.OutOrStdout(), "fix: %s\n", fix)
	}
	if result.Healthy {
		fmt.Fprintln(cmd.OutOrStdout(), "doctor: healthy")
	}
}

func newDaemonInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install daemon integrations for your OS",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonInstallDefault(cmd.OutOrStdout(), "")
		},
	}
	cmd.AddCommand(newDaemonInstallLaunchdCmd())
	cmd.AddCommand(newDaemonInstallSleepwatcherCmd())
	cmd.AddCommand(newDaemonInstallHammerspoonCmd())
	cmd.AddCommand(newDaemonInstallSystemdCmd())
	return cmd
}

func runDaemonInstallDefault(out io.Writer, cfgPath string) error {
	switch runtime.GOOS {
	case "darwin":
		if err := runDaemonLaunchdInstall(
			out,
			cfgPath,
			daemonLaunchdDefaultLabel,
			"",
			"",
			"",
			"",
			true,
			5*time.Minute,
			15*time.Minute,
			true,
			true,
		); err != nil {
			return err
		}
		if _, lookErr := exec.LookPath("sleepwatcher"); lookErr == nil {
			if err := runDaemonSleepwatcherInstall(
				out,
				cfgPath,
				daemonLaunchdDefaultLabel,
				"",
				"",
				"",
				"",
				true,
			); err != nil {
				fmt.Fprintf(out, "warning: sleepwatcher install failed: %v\n", err)
			}
		} else {
			fmt.Fprintln(out, "sleepwatcher not found; skipping. Install with `brew install sleepwatcher` then run `oci-context daemon install sleepwatcher`.")
		}
		if err := runDaemonHammerspoonInstall(
			out,
			cfgPath,
			daemonLaunchdDefaultLabel,
			"",
			"",
			"",
			false,
		); err != nil {
			fmt.Fprintf(out, "warning: hammerspoon install skipped/failed: %v\n", err)
		}
		fmt.Fprintln(out, "Install complete. Use `oci-context daemon up` after wake/resume.")
		return nil
	case "linux":
		return fmt.Errorf("on Linux use `oci-context daemon install systemd`")
	default:
		return fmt.Errorf("daemon install is not supported on %s", runtime.GOOS)
	}
}

func newDaemonRepairCmd() *cobra.Command {
	var (
		cfgPath        string
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
		Use:   "repair [context...]",
		Short: "Install daemon integrations, set monitored contexts, and recover daemon health",
		RunE: func(cmd *cobra.Command, args []string) error {
			monitors = append(monitors, args...)
			return runDaemonRepairCmd(cmd, daemonRepairOptions{
				cfgPath:        cfgPath,
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

type daemonRepairOptions struct {
	cfgPath        string
	label          string
	ociContextBin  string
	monitors       []string
	all            bool
	noLaunchd      bool
	noSleepwatcher bool
	noHammerspoon  bool
	recoverNow     bool
	output         string
}

func runDaemonRepairCmd(cmd *cobra.Command, opts daemonRepairOptions) error {
	out := cmd.OutOrStdout()
	if strings.ToLower(opts.output) != "" && strings.ToLower(opts.output) != "text" {
		out = io.Discard
	}
	result, err := runDaemonRepair(out, opts)
	if err != nil {
		return err
	}
	return printDaemonOutput(cmd, opts.output, result, func() error {
		for _, item := range result.Installed {
			fmt.Fprintf(cmd.OutOrStdout(), "installed: %s\n", item)
		}
		for _, item := range result.Skipped {
			fmt.Fprintf(cmd.OutOrStdout(), "skipped: %s\n", item)
		}
		if len(result.Monitored) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "monitored: %s\n", strings.Join(result.Monitored, ", "))
		}
		if result.Recovered {
			fmt.Fprintln(cmd.OutOrStdout(), "recovered: true")
		}
		return nil
	})
}

func runDaemonRepair(out io.Writer, opts daemonRepairOptions) (daemonRepairResult, error) {
	if runtime.GOOS != "darwin" {
		return daemonRepairResult{}, fmt.Errorf("daemon repair is only supported on macOS")
	}
	cfg, path, err := loadDaemonConfig(opts.cfgPath)
	if err != nil {
		return daemonRepairResult{}, err
	}
	result := daemonRepairResult{OK: true, ConfigPath: path}

	if !opts.noLaunchd {
		if err := runDaemonLaunchdInstall(out, opts.cfgPath, opts.label, opts.ociContextBin, "", "", "", true, 5*time.Minute, 15*time.Minute, true, true); err != nil {
			return result, err
		}
		result.Installed = append(result.Installed, "launchd")
	}
	if opts.all && !opts.noSleepwatcher {
		if _, lookErr := exec.LookPath("sleepwatcher"); lookErr != nil {
			result.Skipped = append(result.Skipped, "sleepwatcher (binary not found; install with `brew install sleepwatcher`)")
		} else if err := runDaemonSleepwatcherInstall(out, opts.cfgPath, opts.label, "", "", "", opts.ociContextBin, true); err != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("sleepwatcher (%v)", err))
		} else {
			result.Installed = append(result.Installed, "sleepwatcher")
		}
	}
	if opts.all && !opts.noHammerspoon {
		if err := runDaemonHammerspoonInstall(out, opts.cfgPath, opts.label, "", "", opts.ociContextBin, true); err != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("hammerspoon (%v)", err))
		} else {
			result.Installed = append(result.Installed, "hammerspoon")
		}
	}

	monitored, err := ensureDaemonMonitorContexts(path, cfg, opts.monitors)
	if err != nil {
		return result, err
	}
	result.Monitored = monitored

	if opts.recoverNow {
		if _, err := runDaemonRecover(out, opts.cfgPath, opts.label, ""); err != nil {
			return result, err
		}
		result.Recovered = true
	}
	return result, nil
}

func newDaemonInstallLaunchdCmd() *cobra.Command {
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
		Use:   "launchd",
		Short: "Install/refresh launchd daemon service and restart it",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonLaunchdInstall(cmd.OutOrStdout(), cfgPath, label, binaryPath, outPath, stdoutPath, stderrPath, autoRefresh, validateInterval, refreshInterval, loadNow, kickstart)
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&label, "label", daemonLaunchdDefaultLabel, "launchd label")
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

func runDaemonLaunchdInstall(out io.Writer, cfgPath, label, binaryPath, outPath, stdoutPath, stderrPath string, autoRefresh bool, validateInterval, refreshInterval time.Duration, loadNow, kickstart bool) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("daemon launchd install is only supported on macOS")
	}
	path, binaryPath, outPath, stdoutPath, stderrPath, err := resolveLaunchdPaths(cfgPath, label, binaryPath, outPath, stdoutPath, stderrPath)
	if err != nil {
		return err
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
	fmt.Fprintf(out, "Wrote launchd plist: %s\n", outPath)

	if !loadNow {
		fmt.Fprintf(out, "Load with:\nlaunchctl unload %s 2>/dev/null || true\nlaunchctl load %s\nlaunchctl start %s\n", outPath, outPath, label)
		return nil
	}

	_ = runCommand(out, "launchctl", "unload", outPath)
	if b, err := runCombinedOutput(out, "launchctl", "load", outPath); err != nil {
		return fmt.Errorf("launchctl load failed: %v: %s", err, strings.TrimSpace(string(b)))
	}
	if b, err := runCombinedOutput(out, "launchctl", "start", label); err != nil {
		return fmt.Errorf("launchctl start failed: %v: %s", err, strings.TrimSpace(string(b)))
	}

	if kickstart {
		target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
		if b, err := runCombinedOutput(out, "launchctl", "kickstart", "-k", target); err != nil {
			return fmt.Errorf("launchctl kickstart failed: %v: %s", err, strings.TrimSpace(string(b)))
		}
		fmt.Fprintf(out, "Loaded and restarted launchd job: %s\n", target)
	} else {
		fmt.Fprintf(out, "Loaded and started launchd job: %s\n", label)
	}
	return nil
}

func newDaemonInstallSleepwatcherCmd() *cobra.Command {
	var cfgPath string
	var daemonLabel string
	var wakeupScript string
	var sleepwatcherPl string
	var sleepwatcherBin string
	var ociContextBin string
	var loadNow bool

	cmd := &cobra.Command{
		Use:   "sleepwatcher",
		Short: "Install sleepwatcher wake hook integration (macOS)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonSleepwatcherInstall(cmd.OutOrStdout(), cfgPath, daemonLabel, wakeupScript, sleepwatcherPl, sleepwatcherBin, ociContextBin, loadNow)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&daemonLabel, "daemon-label", daemonLaunchdDefaultLabel, "launchd label for oci-context daemon")
	cmd.Flags().StringVar(&wakeupScript, "wakeup-script", "", "Path to write wake hook script (default ~/.wakeup)")
	cmd.Flags().StringVar(&sleepwatcherPl, "plist", "", "Path to write sleepwatcher launchd plist")
	cmd.Flags().StringVar(&sleepwatcherBin, "sleepwatcher-bin", "", "Absolute path to sleepwatcher binary")
	cmd.Flags().StringVar(&ociContextBin, "oci-context-bin", "", "Absolute path to oci-context binary")
	cmd.Flags().BoolVar(&loadNow, "load", true, "Load and start launchd agent after writing files")
	return cmd
}

func newDaemonInstallHammerspoonCmd() *cobra.Command {
	var cfgPath string
	var daemonLabel string
	var wakeupScript string
	var hammerspoonDir string
	var ociContextBin string
	var reloadNow bool

	cmd := &cobra.Command{
		Use:   "hammerspoon",
		Short: "Install Hammerspoon notification/auth integration (macOS)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonHammerspoonInstall(cmd.OutOrStdout(), cfgPath, daemonLabel, wakeupScript, hammerspoonDir, ociContextBin, reloadNow)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&daemonLabel, "daemon-label", daemonLaunchdDefaultLabel, "launchd label for oci-context daemon")
	cmd.Flags().StringVar(&wakeupScript, "wakeup-script", "", "Path to write wake hook script (default ~/.wakeup)")
	cmd.Flags().StringVar(&hammerspoonDir, "hammerspoon-dir", "", "Path to Hammerspoon config directory (default ~/.hammerspoon)")
	cmd.Flags().StringVar(&ociContextBin, "oci-context-bin", "", "Absolute path to oci-context binary")
	cmd.Flags().BoolVar(&reloadNow, "reload", true, "Launch Hammerspoon and reload config after writing files")
	return cmd
}

func newDaemonInstallSystemdCmd() *cobra.Command {
	var cfgPath string
	var unitPath string
	var binaryPath string
	var autoRefresh bool
	var validateInterval time.Duration
	var refreshInterval time.Duration
	var loadNow bool

	cmd := &cobra.Command{
		Use:   "systemd",
		Short: "Install a user-level systemd service for daemon (Linux)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" {
				return fmt.Errorf("systemd install is only supported on Linux")
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
				if p, lookErr := exec.LookPath("oci-context"); lookErr == nil {
					binaryPath = p
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
			if unitPath == "" {
				unitPath = filepath.Join(home, ".config", "systemd", "user", "oci-context-daemon.service")
			}
			if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
				return err
			}
			content := renderSystemdUserUnit(binaryPath, path, autoRefresh, validateInterval, refreshInterval)
			if err := os.WriteFile(unitPath, []byte(content), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote systemd user unit: %s\n", unitPath)
			if !loadNow {
				fmt.Fprintln(cmd.OutOrStdout(), "Load with:\nsystemctl --user daemon-reload\nsystemctl --user enable --now oci-context-daemon.service")
				return nil
			}
			if b, err := runCombinedOutput(cmd.OutOrStdout(), "systemctl", "--user", "daemon-reload"); err != nil {
				return fmt.Errorf("systemctl daemon-reload failed: %v: %s", err, strings.TrimSpace(string(b)))
			}
			if b, err := runCombinedOutput(cmd.OutOrStdout(), "systemctl", "--user", "enable", "--now", "oci-context-daemon.service"); err != nil {
				return fmt.Errorf("systemctl enable --now failed: %v: %s", err, strings.TrimSpace(string(b)))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Enabled and started systemd user service: oci-context-daemon.service")
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&unitPath, "unit", "", "Output unit path (default ~/.config/systemd/user/oci-context-daemon.service)")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Absolute path to oci-context binary")
	cmd.Flags().BoolVar(&autoRefresh, "auto-refresh", true, "Enable daemon auth validate/refresh loop")
	cmd.Flags().DurationVar(&validateInterval, "validate-interval", 5*time.Minute, "How often to validate auth")
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 15*time.Minute, "How often to refresh security-token auth")
	cmd.Flags().BoolVar(&loadNow, "load", true, "Reload and enable/start systemd user service")
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
	var output string

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
			resp.Data = daemon.FinalizeAuthStatus(resp.Data)
			return printDaemonOutput(cmd, output, resp.Data, func() error {
				b, err := json.MarshalIndent(resp.Data, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return nil
			})
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (default current)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
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
			monitored, err := ensureDaemonMonitorContexts(path, cfg, args)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Daemon monitor set: %s\n", strings.Join(monitored, ", "))
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	return cmd
}

func ensureDaemonMonitorContexts(path string, cfg config.Config, names []string) ([]string, error) {
	if len(names) == 0 {
		return cfg.Options.DaemonContexts, nil
	}
	exists := make(map[string]bool, len(cfg.Options.DaemonContexts))
	for _, name := range cfg.Options.DaemonContexts {
		exists[name] = true
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, err := cfg.GetContext(name); err != nil {
			return cfg.Options.DaemonContexts, fmt.Errorf("context %q not found", name)
		}
		if exists[name] {
			continue
		}
		cfg.Options.DaemonContexts = append(cfg.Options.DaemonContexts, name)
		exists[name] = true
	}
	if err := config.Save(path, cfg); err != nil {
		return cfg.Options.DaemonContexts, err
	}
	return cfg.Options.DaemonContexts, nil
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
		Use:        "launchd",
		Short:      "Generate/install launchd configuration for daemon",
		Deprecated: "use `oci-context daemon install launchd` or `oci-context daemon install`",
	}
	cmd.AddCommand(newDaemonLaunchdGenerateCmd())
	return cmd
}

func newDaemonSleepwatcherCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "sleepwatcher",
		Short:      "Install wake automation that nudges daemon auth checks",
		Deprecated: "use `oci-context daemon install sleepwatcher`",
	}
	cmd.AddCommand(newDaemonSleepwatcherInstallCmd())
	return cmd
}

func newDaemonHammerspoonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "hammerspoon",
		Short:      "Install Hammerspoon auth notifications and wake automation",
		Deprecated: "use `oci-context daemon install hammerspoon`",
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
			return runDaemonHammerspoonInstall(cmd.OutOrStdout(), cfgPath, daemonLabel, wakeupScript, hammerspoonDir, ociContextBin, reloadNow)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&daemonLabel, "daemon-label", daemonLaunchdDefaultLabel, "launchd label for oci-context daemon")
	cmd.Flags().StringVar(&wakeupScript, "wakeup-script", "", "Path to write wake hook script (default ~/.wakeup)")
	cmd.Flags().StringVar(&hammerspoonDir, "hammerspoon-dir", "", "Path to Hammerspoon config directory (default ~/.hammerspoon)")
	cmd.Flags().StringVar(&ociContextBin, "oci-context-bin", "", "Absolute path to oci-context binary")
	cmd.Flags().BoolVar(&reloadNow, "reload", true, "Launch Hammerspoon and reload config after writing files")
	return cmd
}

func runDaemonHammerspoonInstall(out io.Writer, cfgPath, daemonLabel, wakeupScript, hammerspoonDir, ociContextBin string, reloadNow bool) error {
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

	fmt.Fprintf(out, "Wrote Hammerspoon module: %s\n", modulePath)
	fmt.Fprintf(out, "Updated Hammerspoon init: %s\n", initPath)
	fmt.Fprintf(out, "Wrote wake script: %s\n", wakeupScript)

	if reloadNow {
		if b, err := runCombinedOutput(out, "open", "-a", "Hammerspoon"); err != nil {
			return fmt.Errorf("failed to launch Hammerspoon: %v: %s", err, strings.TrimSpace(string(b)))
		}
		if b, err := runCombinedOutput(out, "open", "-g", "hammerspoon://reloadConfig"); err != nil {
			return fmt.Errorf("failed to reload Hammerspoon config: %v: %s", err, strings.TrimSpace(string(b)))
		}
		fmt.Fprintln(out, "Hammerspoon launched and config reloaded.")
	} else {
		fmt.Fprintln(out, "Reload Hammerspoon config with: open -g 'hammerspoon://reloadConfig'")
	}
	return nil
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
			msg := fmt.Sprintf("Auth check for %s (%s)", ctxName, prof)
			if strings.TrimSpace(reason) != "" {
				msg = fmt.Sprintf("%s\n%s", msg, reason)
			}
			result, err := sendAlerterAuthNotification(prof, reg, tenancy, eventURL, msg, "OCI Access Required", ctxName)
			if err != nil {
				return err
			}
			if nativeNotify {
				fmt.Fprintln(cmd.OutOrStdout(), "native notify is implicit in alerter backend")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Triggered alerter auth notification for context=%s profile=%s region=%s result=%s\n", ctxName, prof, reg, result)
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
			return runDaemonSleepwatcherInstall(cmd.OutOrStdout(), cfgPath, daemonLabel, wakeupScript, sleepwatcherPl, sleepwatcherBin, ociContextBin, loadNow)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&daemonLabel, "daemon-label", daemonLaunchdDefaultLabel, "launchd label for oci-context daemon")
	cmd.Flags().StringVar(&wakeupScript, "wakeup-script", "", "Path to write wake hook script (default ~/.wakeup)")
	cmd.Flags().StringVar(&sleepwatcherPl, "plist", "", "Path to write sleepwatcher launchd plist")
	cmd.Flags().StringVar(&sleepwatcherBin, "sleepwatcher-bin", "", "Absolute path to sleepwatcher binary")
	cmd.Flags().StringVar(&ociContextBin, "oci-context-bin", "", "Absolute path to oci-context binary")
	cmd.Flags().BoolVar(&loadNow, "load", true, "Load and start launchd agent after writing files")
	return cmd
}

func runDaemonSleepwatcherInstall(out io.Writer, cfgPath, daemonLabel, wakeupScript, sleepwatcherPl, sleepwatcherBin, ociContextBin string, loadNow bool) error {
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
	fmt.Fprintf(out, "Wrote wake script: %s\n", wakeupScript)
	fmt.Fprintf(out, "Wrote launchd plist: %s\n", sleepwatcherPl)
	if loadNow {
		_ = runCommand(out, "launchctl", "unload", sleepwatcherPl)
		if b, err := runCombinedOutput(out, "launchctl", "load", sleepwatcherPl); err != nil {
			return fmt.Errorf("launchctl load failed: %v: %s", err, strings.TrimSpace(string(b)))
		}
		if b, err := runCombinedOutput(out, "launchctl", "start", "com.adrianmross.oci-context.sleepwatcher"); err != nil {
			return fmt.Errorf("launchctl start failed: %v: %s", err, strings.TrimSpace(string(b)))
		}
		fmt.Fprintln(out, "Loaded and started sleepwatcher launch agent.")
	} else {
		fmt.Fprintf(out, "Load with:\nlaunchctl unload %s 2>/dev/null || true\nlaunchctl load %s\nlaunchctl start com.adrianmross.oci-context.sleepwatcher\n", sleepwatcherPl, sleepwatcherPl)
	}
	return nil
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
			path, binaryPath, outPath, stdoutPath, stderrPath, err := resolveLaunchdPaths(cfgPath, label, binaryPath, outPath, stdoutPath, stderrPath)
			if err != nil {
				return err
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
	cmd.Flags().StringVar(&label, "label", daemonLaunchdDefaultLabel, "launchd label")
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

func dialDaemonSocketWithRetry(out io.Writer, socketPath string, timeout time.Duration) (*ipcmsg.Conn, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	waitingPrinted := false
	for {
		conn, err := ipcmsg.Dial(socketPath)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		if daemonVerbose && !waitingPrinted {
			fmt.Fprintf(out, "waiting for daemon socket: %s\n", socketPath)
			waitingPrinted = true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, lastErr
}

func resolveLaunchdPaths(cfgPath, label, binaryPath, outPath, stdoutPath, stderrPath string) (string, string, string, string, string, error) {
	path, err := daemon.EnsureConfig(cfgPath)
	if err != nil {
		return "", "", "", "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", "", "", err
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
		return "", "", "", "", "", fmt.Errorf("could not resolve oci-context binary path; pass --binary")
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
	return path, binaryPath, outPath, stdoutPath, stderrPath, nil
}

func newDaemonNudgeCmd() *cobra.Command {
	var cfgPath string
	var contextName string
	var output string
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
			var resp daemonCommandResult
			if err := conn.ReadResponse(&resp); err != nil {
				return err
			}
			if !resp.OK {
				return errors.New(resp.Error)
			}
			return printDaemonOutput(cmd, output, resp, func() error {
				b, err := json.MarshalIndent(resp.Data, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (default monitored list)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
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
    <key>EnvironmentVariables</key>
    <dict>
      <key>PATH</key>
      <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    </dict>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
  </dict>
</plist>
`, xmlEscape(label), strings.Join(argXML, "\n"), xmlEscape(stdoutPath), xmlEscape(stderrPath))
}

func renderSystemdUserUnit(binaryPath, cfgPath string, autoRefresh bool, validateInterval, refreshInterval time.Duration) string {
	args := []string{
		shellQuote(binaryPath),
		"daemon",
		"serve",
		"--config",
		shellQuote(cfgPath),
	}
	if autoRefresh {
		args = append(args, "--auto-refresh")
	}
	args = append(args, "--validate-interval", validateInterval.String(), "--refresh-interval", refreshInterval.String())
	return fmt.Sprintf(`[Unit]
Description=oci-context daemon
After=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
`, strings.Join(args, " "))
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

func buildAlerterAuthArgs(eventURL, message, title, subtitle string) []string {
	args := []string{
		"--title", title,
		"--subtitle", subtitle,
		"--message", message,
		"--actions", "Re-auth now",
		"--closeLabel", "Dismiss",
		"--sound", "default",
		"--timeout", "45",
		"--group", "oci-context-auth-" + subtitle,
	}
	if icon := defaultAlerterIconPath(); icon != "" {
		args = append(args, "--appIcon", icon, "--contentImage", icon)
	}
	_ = eventURL
	return args
}

func defaultAlerterIconPath() string {
	for _, path := range []string{
		"/System/Library/CoreServices/CoreTypes.bundle/Contents/Resources/GenericNetworkIcon.icns",
		"/System/Library/CoreServices/CoreTypes.bundle/Contents/Resources/AlertCautionIcon.icns",
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func buildOCIAuthenticateArgs(profile, region, tenancyName string) []string {
	args := []string{"session", "authenticate", "--profile-name", profile}
	if strings.TrimSpace(region) != "" {
		args = append(args, "--region", region)
	}
	if strings.TrimSpace(tenancyName) != "" {
		args = append(args, "--tenancy-name", tenancyName)
	}
	return args
}

func sendAlerterAuthNotification(profile, region, tenancyName, eventURL, message, title, subtitle string) (string, error) {
	alerterPath, err := exec.LookPath("alerter")
	if err != nil {
		return "", fmt.Errorf("alerter is required for this branch; install with `brew install vjeantet/tap/alerter`")
	}
	args := buildAlerterAuthArgs(eventURL, message, title, subtitle)
	out, err := exec.Command(alerterPath, args...).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return result, fmt.Errorf("alerter failed: %v: %s", err, result)
	}
	if strings.EqualFold(result, "Re-auth now") || strings.Contains(result, "Re-auth now") || strings.Contains(result, "ACTION") {
		ociArgs := buildOCIAuthenticateArgs(profile, region, tenancyName)
		if out, err := exec.Command("oci", ociArgs...).CombinedOutput(); err != nil {
			return result, fmt.Errorf("oci session authenticate failed after alerter action: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	return result, nil
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

func runCommand(out io.Writer, name string, args ...string) error {
	if daemonVerbose {
		fmt.Fprintf(out, "+ %s", name)
		for _, a := range args {
			fmt.Fprintf(out, " %s", shellQuote(a))
		}
		fmt.Fprintln(out)
	}
	return exec.Command(name, args...).Run()
}

func runCombinedOutput(out io.Writer, name string, args ...string) ([]byte, error) {
	if daemonVerbose {
		fmt.Fprintf(out, "+ %s", name)
		for _, a := range args {
			fmt.Fprintf(out, " %s", shellQuote(a))
		}
		fmt.Fprintln(out)
	}
	return exec.Command(name, args...).CombinedOutput()
}
