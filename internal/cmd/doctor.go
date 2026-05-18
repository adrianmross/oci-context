package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	daemonpkg "github.com/adrianmross/oci-context/internal/daemon"
	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type doctorResult struct {
	ConfigPath     string             `json:"config_path" yaml:"config_path"`
	CurrentContext string             `json:"current_context" yaml:"current_context"`
	Context        doctorContextInfo  `json:"context" yaml:"context"`
	OCIConfig      doctorPathStatus   `json:"oci_config" yaml:"oci_config"`
	OCICLI         doctorCLIStatus    `json:"oci_cli" yaml:"oci_cli"`
	Daemon         doctorDaemonStatus `json:"daemon" yaml:"daemon"`
	AuthEnsure     authEnsureResult   `json:"auth_ensure" yaml:"auth_ensure"`
}

type doctorContextInfo struct {
	Profile    string `json:"profile" yaml:"profile"`
	Region     string `json:"region" yaml:"region"`
	AuthMethod string `json:"auth_method" yaml:"auth_method"`
}

type doctorPathStatus struct {
	Path   string `json:"path" yaml:"path"`
	Exists bool   `json:"exists" yaml:"exists"`
	IsFile bool   `json:"is_file" yaml:"is_file"`
	Error  string `json:"error,omitempty" yaml:"error,omitempty"`
}

type doctorCLIStatus struct {
	Available bool   `json:"available" yaml:"available"`
	Path      string `json:"path,omitempty" yaml:"path,omitempty"`
	Version   string `json:"version,omitempty" yaml:"version,omitempty"`
	Error     string `json:"error,omitempty" yaml:"error,omitempty"`
}

type doctorDaemonStatus struct {
	Available bool                  `json:"available" yaml:"available"`
	Socket    string                `json:"socket" yaml:"socket"`
	Error     string                `json:"error,omitempty" yaml:"error,omitempty"`
	Status    *daemonpkg.AuthStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

var lookPathForDoctor = exec.LookPath

var runDoctorCommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

var fetchDaemonAuthStatusForDoctor = fetchDaemonAuthStatus

func newDoctorCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var contextName string
	var output string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Summarize local oci-context and OCI CLI health",
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := cmd.Flags().GetBool("global")
			if err != nil {
				return err
			}
			if g {
				useGlobal = true
			}
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			target := strings.TrimSpace(contextName)
			if target == "" {
				target = cfg.CurrentContext
			}
			if target == "" {
				return fmt.Errorf("no current context set")
			}
			ctx, err := cfg.GetContext(target)
			if err != nil {
				return err
			}
			result := buildDoctorResult(cmd, path, cfg, ctx, target)
			return printDoctorResult(cmd, result, output)
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVar(&contextName, "context", "", "Target context name (default current)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
	return cmd
}

func buildDoctorResult(cmd *cobra.Command, configPath string, cfg config.Config, ctx config.Context, contextName string) doctorResult {
	method := config.NormalizeAuthMethod(ctx.AuthMethod)
	result := doctorResult{
		ConfigPath:     configPath,
		CurrentContext: cfg.CurrentContext,
		Context: doctorContextInfo{
			Profile:    ctx.Profile,
			Region:     ctx.Region,
			AuthMethod: method,
		},
		OCIConfig: inspectPath(cfg.Options.OCIConfigPath),
		OCICLI:    inspectOCICLI(cmd.Context()),
		Daemon: doctorDaemonStatus{
			Socket: cfg.Options.SocketPath,
		},
	}
	if st, err := fetchDaemonAuthStatusForDoctor(cfg, contextName); err == nil {
		result.Daemon.Available = true
		result.Daemon.Status = &st
	} else {
		result.Daemon.Error = err.Error()
	}
	authResult, authErr := runAuthEnsure(cmd, cfg, ctx, contextName, authEnsureOptions{
		Login:         false,
		NoInteractive: true,
	})
	if authErr != nil && authResult.Error == "" {
		authResult.Error = authErr.Error()
	}
	result.AuthEnsure = authResult
	return result
}

func inspectPath(path string) doctorPathStatus {
	status := doctorPathStatus{Path: path}
	if path == "" {
		status.Error = "not configured"
		return status
	}
	fi, err := os.Stat(path)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	status.IsFile = !fi.IsDir()
	return status
}

func inspectOCICLI(parent context.Context) doctorCLIStatus {
	path, err := lookPathForDoctor("oci")
	if err != nil {
		return doctorCLIStatus{Available: false, Error: err.Error()}
	}
	status := doctorCLIStatus{Available: true, Path: path}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	out, err := runDoctorCommandOutput(ctx, path, "--version")
	trimmed := strings.TrimSpace(string(out))
	if trimmed != "" {
		status.Version = trimmed
	}
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

func printDoctorResult(cmd *cobra.Command, result doctorResult, output string) error {
	switch strings.ToLower(output) {
	case "", "text":
		fmt.Fprintf(cmd.OutOrStdout(), "config: %s\n", result.ConfigPath)
		fmt.Fprintf(cmd.OutOrStdout(), "current_context: %s\n", result.CurrentContext)
		fmt.Fprintf(cmd.OutOrStdout(), "profile: %s\n", result.Context.Profile)
		fmt.Fprintf(cmd.OutOrStdout(), "region: %s\n", result.Context.Region)
		fmt.Fprintf(cmd.OutOrStdout(), "auth: %s\n", result.Context.AuthMethod)
		fmt.Fprintf(cmd.OutOrStdout(), "oci_config: %s exists=%t file=%t\n", result.OCIConfig.Path, result.OCIConfig.Exists, result.OCIConfig.IsFile)
		if result.OCIConfig.Error != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "oci_config_error: %s\n", result.OCIConfig.Error)
		}
		if result.OCICLI.Available {
			fmt.Fprintf(cmd.OutOrStdout(), "oci_cli: %s", result.OCICLI.Path)
			if result.OCICLI.Version != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " version=%s", result.OCICLI.Version)
			}
			fmt.Fprintln(cmd.OutOrStdout())
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "oci_cli: unavailable (%s)\n", result.OCICLI.Error)
		}
		if result.Daemon.Available {
			fmt.Fprintf(cmd.OutOrStdout(), "daemon: available socket=%s\n", result.Daemon.Socket)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "daemon: unavailable socket=%s error=%s\n", result.Daemon.Socket, result.Daemon.Error)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "auth_ensure: state=%s ok=%t validated=%t refreshed=%t login_required=%t\n", result.AuthEnsure.State, result.AuthEnsure.OK, result.AuthEnsure.Validated, result.AuthEnsure.Refreshed, result.AuthEnsure.LoginRequired)
		if result.AuthEnsure.LoginCommand != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "login_command: %s\n", result.AuthEnsure.LoginCommand)
		}
		if result.AuthEnsure.Error != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "auth_error: %s\n", result.AuthEnsure.Error)
		}
		return nil
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case "yaml", "yml":
		enc := yaml.NewEncoder(cmd.OutOrStdout())
		defer enc.Close()
		return enc.Encode(result)
	default:
		return fmt.Errorf("unsupported output format: %s", output)
	}
}
