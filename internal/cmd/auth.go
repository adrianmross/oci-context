package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	daemonpkg "github.com/adrianmross/oci-context/internal/daemon"
	"github.com/adrianmross/oci-context/pkg/config"
	ipcmsg "github.com/adrianmross/oci-context/pkg/ipc"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type authCapability struct {
	CanLogin    bool
	CanRefresh  bool
	CanValidate bool
	CanSetup    bool
	LoginHint   string
	RefreshHint string
	SetupHint   string
}

type authEnsureResult struct {
	OK             bool                `json:"ok" yaml:"ok"`
	State          string              `json:"state" yaml:"state"`
	Context        string              `json:"context" yaml:"context"`
	Profile        string              `json:"profile" yaml:"profile"`
	AuthMethod     string              `json:"auth_method" yaml:"auth_method"`
	Validated      bool                `json:"validated" yaml:"validated"`
	Refreshed      bool                `json:"refreshed" yaml:"refreshed"`
	LoginAttempted bool                `json:"login_attempted" yaml:"login_attempted"`
	LoginRequired  bool                `json:"login_required" yaml:"login_required"`
	Ready          bool                `json:"ready" yaml:"ready"`
	ActionRequired bool                `json:"action_required" yaml:"action_required"`
	Action         string              `json:"action" yaml:"action"`
	Severity       string              `json:"severity" yaml:"severity"`
	LoginCommand   string              `json:"login_command,omitempty" yaml:"login_command,omitempty"`
	HomeRegion     *regionSubscription `json:"home_region,omitempty" yaml:"home_region,omitempty"`
	Message        string              `json:"message,omitempty" yaml:"message,omitempty"`
	Error          string              `json:"error,omitempty" yaml:"error,omitempty"`
}

const (
	authEnsureStateReady            = "ready"
	authEnsureStateRefreshed        = "refreshed"
	authEnsureStateLoginRequired    = "login_required"
	authEnsureStateLoginFailed      = "login_failed"
	authEnsureStateValidationFailed = "validation_failed"
)

type authEnsureOptions struct {
	Login         bool
	NoInteractive bool
}

type authActions struct {
	Login    bool `json:"login" yaml:"login"`
	Refresh  bool `json:"refresh" yaml:"refresh"`
	Validate bool `json:"validate" yaml:"validate"`
	Setup    bool `json:"setup" yaml:"setup"`
}

type authMethodInfo struct {
	Method      string      `json:"method" yaml:"method"`
	Description string      `json:"description" yaml:"description"`
	Actions     authActions `json:"actions" yaml:"actions"`
	LoginHint   string      `json:"login_hint,omitempty" yaml:"login_hint,omitempty"`
	RefreshHint string      `json:"refresh_hint,omitempty" yaml:"refresh_hint,omitempty"`
	SetupHint   string      `json:"setup_hint,omitempty" yaml:"setup_hint,omitempty"`
}

type authMethodsResult struct {
	Methods []authMethodInfo `json:"methods" yaml:"methods"`
}

type authShowResult struct {
	Context         string                `json:"context" yaml:"context"`
	Profile         string                `json:"profile" yaml:"profile"`
	AuthMethod      string                `json:"auth_method" yaml:"auth_method"`
	User            string                `json:"user,omitempty" yaml:"user,omitempty"`
	Actions         authActions           `json:"actions" yaml:"actions"`
	LoginHint       string                `json:"login_hint,omitempty" yaml:"login_hint,omitempty"`
	RefreshHint     string                `json:"refresh_hint,omitempty" yaml:"refresh_hint,omitempty"`
	SetupHint       string                `json:"setup_hint,omitempty" yaml:"setup_hint,omitempty"`
	DaemonAvailable bool                  `json:"daemon_available" yaml:"daemon_available"`
	DaemonError     string                `json:"daemon_error,omitempty" yaml:"daemon_error,omitempty"`
	Daemon          *daemonpkg.AuthStatus `json:"daemon,omitempty" yaml:"daemon,omitempty"`
}

func authCapabilityForMethod(method string) authCapability {
	switch config.NormalizeAuthMethod(method) {
	case config.AuthMethodSecurityToken:
		return authCapability{
			CanLogin:    true,
			CanRefresh:  true,
			CanValidate: true,
			CanSetup:    true,
			LoginHint:   "oci session authenticate",
			RefreshHint: "oci session refresh",
			SetupHint:   "oci setup config (for profile bootstrap)",
		}
	case config.AuthMethodAPIKey:
		return authCapability{
			CanLogin:    false,
			CanRefresh:  false,
			CanValidate: true,
			CanSetup:    true,
			SetupHint:   "oci setup config",
		}
	case config.AuthMethodInstancePrincipal:
		return authCapability{
			CanLogin:    false,
			CanRefresh:  false,
			CanValidate: true,
			CanSetup:    true,
			SetupHint:   "oci setup instance-principal",
		}
	case config.AuthMethodResourcePrincipal, config.AuthMethodInstanceOBOUser, config.AuthMethodOKEWorkload:
		return authCapability{
			CanLogin:    false,
			CanRefresh:  false,
			CanValidate: true,
			CanSetup:    false,
		}
	default:
		return authCapability{}
	}
}

func authMethodDescription(method string) string {
	switch method {
	case config.AuthMethodAPIKey:
		return "API signing key (default)"
	case config.AuthMethodSecurityToken:
		return "Session token from `oci session authenticate`"
	case config.AuthMethodInstancePrincipal:
		return "Compute instance principal"
	case config.AuthMethodResourcePrincipal:
		return "Resource principal"
	case config.AuthMethodInstanceOBOUser:
		return "Cloud Shell delegation token"
	case config.AuthMethodOKEWorkload:
		return "OKE workload identity"
	default:
		return ""
	}
}

func buildAuthMethodsResult() authMethodsResult {
	methods := config.ValidAuthMethods()
	result := authMethodsResult{Methods: make([]authMethodInfo, 0, len(methods))}
	for _, method := range methods {
		cap := authCapabilityForMethod(method)
		result.Methods = append(result.Methods, authMethodInfo{
			Method:      method,
			Description: authMethodDescription(method),
			Actions: authActions{
				Login:    cap.CanLogin,
				Refresh:  cap.CanRefresh,
				Validate: cap.CanValidate,
				Setup:    cap.CanSetup,
			},
			LoginHint:   cap.LoginHint,
			RefreshHint: cap.RefreshHint,
			SetupHint:   cap.SetupHint,
		})
	}
	return result
}

func printAuthMethodsResult(cmd *cobra.Command, result authMethodsResult, output string) error {
	switch strings.ToLower(output) {
	case "", "text":
		fmt.Fprintln(cmd.OutOrStdout(), "Supported auth methods:")
		for _, method := range result.Methods {
			fmt.Fprintf(cmd.OutOrStdout(), "- %s: %s\n", method.Method, method.Description)
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

func loadCurrentContext(path string) (config.Config, config.Context, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return config.Config{}, config.Context{}, err
	}
	if cfg.CurrentContext == "" {
		return config.Config{}, config.Context{}, fmt.Errorf("no current context set")
	}
	ctx, err := cfg.GetContext(cfg.CurrentContext)
	if err != nil {
		return config.Config{}, config.Context{}, err
	}
	return cfg, ctx, nil
}

func runOCI(cmd *cobra.Command, args []string) error {
	ociCmd := exec.CommandContext(cmd.Context(), "oci", args...)
	ociCmd.Stdin = cmd.InOrStdin()
	ociCmd.Stdout = cmd.OutOrStdout()
	ociCmd.Stderr = cmd.ErrOrStderr()
	if err := ociCmd.Run(); err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err != nil {
			return fmt.Errorf("failed to execute oci CLI (%w): install with `pip install oci-cli` or ensure it is in PATH", ee.Err)
		}
		return err
	}
	return nil
}

func runOCICapture(cmd *cobra.Command, args []string) ([]byte, error) {
	ociCmd := exec.CommandContext(cmd.Context(), "oci", args...)
	ociCmd.Stdin = cmd.InOrStdin()
	ociCmd.Stderr = cmd.ErrOrStderr()
	out, err := ociCmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err != nil {
			return nil, fmt.Errorf("failed to execute oci CLI (%w): install with `pip install oci-cli` or ensure it is in PATH", ee.Err)
		}
		return nil, err
	}
	return out, nil
}

var runOCIForAuth = runOCI
var runOCICaptureForAuth = runOCICapture

type regionSubscriptionList struct {
	Data []regionSubscription `json:"data"`
}

type regionSubscription struct {
	IsHomeRegion bool   `json:"is-home-region"`
	RegionKey    string `json:"region-key"`
	RegionName   string `json:"region-name"`
	Status       string `json:"status"`
}

func findHomeRegion(payload []byte) (regionSubscription, error) {
	var list regionSubscriptionList
	if err := json.Unmarshal(payload, &list); err != nil {
		return regionSubscription{}, err
	}
	for _, region := range list.Data {
		if region.IsHomeRegion {
			return region, nil
		}
	}
	return regionSubscription{}, fmt.Errorf("home region not found in OCI region subscriptions")
}

func buildAuthValidateOCIArgs(ctx config.Context, ociConfigPath string) []string {
	method := config.NormalizeAuthMethod(ctx.AuthMethod)
	args := []string{
		"iam", "region-subscription", "list",
		"--tenancy-id", ctx.TenancyOCID,
		"--output", "json",
	}
	if ociConfigPath != "" {
		args = append(args, "--config-file", ociConfigPath)
	}
	if ctx.Profile != "" {
		args = append(args, "--profile", ctx.Profile)
	}
	if method != "" {
		args = append(args, "--auth", method)
	}
	if ctx.Region != "" {
		args = append(args, "--region", ctx.Region)
	}
	return args
}

func authLoginCommand(ctx config.Context) string {
	parts := []string{"oci-context", "auth", "login"}
	if ctx.Name != "" {
		parts = append(parts, "--context", ctx.Name)
	}
	return strings.Join(parts, " ")
}

func commandNoInteractive(cmd *cobra.Command) bool {
	if cliNoInteractive {
		return true
	}
	if flag := cmd.Flag("no-interactive"); flag != nil {
		v, err := cmd.Flags().GetBool("no-interactive")
		return err == nil && v
	}
	return false
}

func interactiveDisabledError() error {
	return fmt.Errorf("interactive login/setup flow disabled by --no-interactive")
}

func validateAuthContext(cmd *cobra.Command, ctx config.Context, ociConfigPath string) (regionSubscription, error) {
	out, err := runOCICaptureForAuth(cmd, buildAuthValidateOCIArgs(ctx, ociConfigPath))
	if err != nil {
		return regionSubscription{}, err
	}
	return findHomeRegion(out)
}

func runAuthEnsure(cmd *cobra.Command, cfg config.Config, ctx config.Context, name string, opts authEnsureOptions) (authEnsureResult, error) {
	method := config.NormalizeAuthMethod(ctx.AuthMethod)
	if name == "" {
		name = ctx.Name
	}
	result := authEnsureResult{
		Context:    name,
		Profile:    ctx.Profile,
		AuthMethod: method,
	}
	if home, err := validateAuthContext(cmd, ctx, cfg.Options.OCIConfigPath); err == nil {
		result.OK = true
		result.State = authEnsureStateReady
		result.Validated = true
		result.HomeRegion = &home
		return finalizeAuthEnsureResult(result), nil
	} else {
		result.Error = err.Error()
	}
	if method == config.AuthMethodSecurityToken {
		if err := runOCIForAuth(cmd, []string{"session", "refresh", "--profile", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath}); err == nil {
			result.Refreshed = true
			if home, validateErr := validateAuthContext(cmd, ctx, cfg.Options.OCIConfigPath); validateErr == nil {
				result.OK = true
				result.State = authEnsureStateRefreshed
				result.Validated = true
				result.Error = ""
				result.HomeRegion = &home
				return finalizeAuthEnsureResult(result), nil
			} else {
				result.Error = validateErr.Error()
			}
		} else {
			result.Error = err.Error()
		}
	}
	if opts.Login {
		if opts.NoInteractive {
			result.State = authEnsureStateLoginRequired
			result.LoginRequired = method == config.AuthMethodSecurityToken
			result.LoginCommand = authLoginCommand(ctx)
			result.Message = "run `oci-context auth login` in an interactive shell"
			if result.Error == "" {
				result.Error = "interactive login/setup flow disabled by --no-interactive"
			}
			return finalizeAuthEnsureResult(result), fmt.Errorf("auth ensure failed for %s (%s): login required", name, method)
		}
		result.LoginAttempted = true
		if err := runOCIForAuth(cmd, []string{"session", "authenticate", "--profile-name", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath, "--region", ctx.Region}); err != nil {
			result.State = authEnsureStateLoginFailed
			result.Error = err.Error()
			result.LoginRequired = true
			result.LoginCommand = authLoginCommand(ctx)
			return finalizeAuthEnsureResult(result), fmt.Errorf("auth ensure failed for %s (%s): %w", name, method, err)
		}
		if home, err := validateAuthContext(cmd, ctx, cfg.Options.OCIConfigPath); err == nil {
			result.OK = true
			result.State = authEnsureStateReady
			result.Validated = true
			result.Error = ""
			result.HomeRegion = &home
			return finalizeAuthEnsureResult(result), nil
		} else {
			result.Error = err.Error()
		}
	}
	result.LoginRequired = method == config.AuthMethodSecurityToken
	if result.LoginRequired {
		result.State = authEnsureStateLoginRequired
		result.LoginCommand = authLoginCommand(ctx)
		if result.Message == "" {
			result.Message = "run `oci-context auth login` or rerun with `oci-context auth ensure --login`"
		}
	} else {
		result.State = authEnsureStateValidationFailed
	}
	return finalizeAuthEnsureResult(result), fmt.Errorf("auth ensure failed for %s (%s): %s", name, method, result.Error)
}

func finalizeAuthEnsureResult(result authEnsureResult) authEnsureResult {
	result.Ready = result.OK && result.Validated
	result.Action = "none"
	result.Severity = "ok"
	if result.ActionRequired {
		return result
	}
	if result.Ready {
		return result
	}
	result.ActionRequired = true
	result.Severity = "error"
	switch {
	case result.LoginRequired:
		result.Action = "login"
	case result.State == authEnsureStateLoginFailed:
		result.Action = "login"
	default:
		result.Action = "check_auth"
	}
	return result
}

func printAuthEnsureResult(cmd *cobra.Command, result authEnsureResult, output string) error {
	switch strings.ToLower(output) {
	case "", "text":
		if result.OK {
			fmt.Fprintf(cmd.OutOrStdout(), "Auth ready for %s (%s)\n", result.Context, result.AuthMethod)
			if result.Refreshed {
				fmt.Fprintln(cmd.OutOrStdout(), "refreshed: true")
			}
			if result.HomeRegion != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "context: home-region=%s (%s) status=%s\n", result.HomeRegion.RegionName, result.HomeRegion.RegionKey, result.HomeRegion.Status)
			}
			return nil
		}
		if result.LoginRequired {
			fmt.Fprintf(cmd.OutOrStdout(), "Auth requires login for %s (%s)\n", result.Context, result.AuthMethod)
			if result.Message != "" {
				fmt.Fprintln(cmd.OutOrStdout(), result.Message)
			}
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Auth not ready for %s (%s)\n", result.Context, result.AuthMethod)
		if result.Error != "" {
			fmt.Fprintln(cmd.OutOrStdout(), result.Error)
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

func fetchDaemonAuthStatus(cfg config.Config, contextName string) (daemonpkg.AuthStatus, error) {
	conn, err := ipcmsg.Dial(cfg.Options.SocketPath)
	if err != nil {
		return daemonpkg.AuthStatus{}, err
	}
	defer conn.Close()
	req := ipcmsg.Request{Method: "auth_status", Name: contextName}
	if err := conn.SendRequest(req); err != nil {
		return daemonpkg.AuthStatus{}, err
	}
	var resp struct {
		OK    bool                 `json:"ok"`
		Error string               `json:"error,omitempty"`
		Data  daemonpkg.AuthStatus `json:"data,omitempty"`
	}
	if err := conn.ReadResponse(&resp); err != nil {
		return daemonpkg.AuthStatus{}, err
	}
	if !resp.OK {
		return daemonpkg.AuthStatus{}, fmt.Errorf("%s", resp.Error)
	}
	return daemonpkg.FinalizeAuthStatus(resp.Data), nil
}

func newAuthCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var targetContext string

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage context auth method and identity workflows",
	}
	cmd.AddCommand(newNotifyCmd("notify", "Trigger auth notification (Hammerspoon + native macOS)"))

	resolvePath := func(cmd *cobra.Command) (string, error) {
		g, err := cmd.Flags().GetBool("global")
		if err != nil {
			return "", err
		}
		return resolveConfigPath(cfgPath, g)
	}

	loadTarget := func(path string) (config.Config, config.Context, error) {
		cfg, err := config.Load(path)
		if err != nil {
			return config.Config{}, config.Context{}, err
		}
		name := strings.TrimSpace(targetContext)
		if name == "" {
			name = cfg.CurrentContext
		}
		if name == "" {
			return config.Config{}, config.Context{}, fmt.Errorf("no current context set")
		}
		ctx, err := cfg.GetContext(name)
		if err != nil {
			return config.Config{}, config.Context{}, err
		}
		return cfg, ctx, nil
	}

	var methodsOutput string
	methodsCmd := &cobra.Command{
		Use:   "methods",
		Short: "List supported auth methods",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printAuthMethodsResult(cmd, buildAuthMethodsResult(), methodsOutput)
		},
	}
	methodsCmd.Flags().StringVarP(&methodsOutput, "output", "o", "text", "Output format: text|json|yaml")
	cmd.AddCommand(methodsCmd)
	cmd.AddCommand(newAuthTokenCmd(resolvePath, loadTarget))

	var showOutput string
	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show auth settings and available actions for the selected context",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			method := config.NormalizeAuthMethod(ctx.AuthMethod)
			cap := authCapabilityForMethod(method)
			name := ctx.Name
			if name == "" {
				name = cfg.CurrentContext
			}
			result := authShowResult{
				Context:    name,
				Profile:    ctx.Profile,
				AuthMethod: method,
				User:       ctx.User,
				Actions: authActions{
					Login:    cap.CanLogin,
					Refresh:  cap.CanRefresh,
					Validate: cap.CanValidate,
					Setup:    cap.CanSetup,
				},
				LoginHint:   cap.LoginHint,
				RefreshHint: cap.RefreshHint,
				SetupHint:   cap.SetupHint,
			}
			if st, err := fetchDaemonAuthStatus(cfg, name); err == nil {
				result.DaemonAvailable = true
				result.Daemon = &st
			} else {
				result.DaemonError = err.Error()
			}
			switch strings.ToLower(showOutput) {
			case "", "text":
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			case "yaml", "yml":
				enc := yaml.NewEncoder(cmd.OutOrStdout())
				defer enc.Close()
				return enc.Encode(result)
			default:
				return fmt.Errorf("unsupported output format: %s", showOutput)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "context: %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "profile: %s\n", ctx.Profile)
			fmt.Fprintf(cmd.OutOrStdout(), "auth: %s\n", method)
			if ctx.User != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "user: %s\n", ctx.User)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "actions: login=%t refresh=%t validate=%t setup=%t\n", cap.CanLogin, cap.CanRefresh, cap.CanValidate, cap.CanSetup)
			if cap.LoginHint != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "login_hint: %s\n", cap.LoginHint)
			}
			if cap.RefreshHint != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "refresh_hint: %s\n", cap.RefreshHint)
			}
			if cap.SetupHint != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "setup_hint: %s\n", cap.SetupHint)
			}
			if result.Daemon != nil {
				st := *result.Daemon
				fmt.Fprintf(cmd.OutOrStdout(), "daemon_mode: %s\n", st.Mode)
				if st.LastValidatedAt != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "daemon_last_validate: %s (ok=%t)\n", st.LastValidatedAt, st.LastValidateOK)
				}
				if st.LastRefreshedAt != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "daemon_last_refresh: %s (ok=%t)\n", st.LastRefreshedAt, st.LastRefreshOK)
				}
				if st.HomeRegionName != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "daemon_home_region: %s (%s) status=%s\n", st.HomeRegionName, st.HomeRegionKey, st.HomeRegionStatus)
				}
				if st.LastError != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "daemon_last_error: %s\n", st.LastError)
				}
			}
			return nil
		},
	}
	showCmd.Flags().StringVarP(&showOutput, "output", "o", "text", "Output format: text|json|yaml")
	cmd.AddCommand(showCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "set <auth-method>",
		Short: "Set auth method on the selected context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			method := config.NormalizeAuthMethod(args[0])
			if !config.IsValidAuthMethod(method) {
				return fmt.Errorf("unsupported auth method %q", args[0])
			}
			ctx.AuthMethod = method
			if err := cfg.UpsertContext(ctx); err != nil {
				return err
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if ctx.Name == cfg.CurrentContext {
				if err := syncOCIDefaultsForCurrent(cfg); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set auth method for %s to %s\n", ctx.Name, method)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set-user <user-ocid-or-label>",
		Short: "Set user hint on the selected context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			ctx.User = strings.TrimSpace(args[0])
			if err := cfg.UpsertContext(ctx); err != nil {
				return err
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set user for %s to %s\n", ctx.Name, ctx.User)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Start login/bootstrap flow based on current auth method",
		RunE: func(cmd *cobra.Command, args []string) error {
			if commandNoInteractive(cmd) {
				return interactiveDisabledError()
			}
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			method := config.NormalizeAuthMethod(ctx.AuthMethod)
			switch method {
			case config.AuthMethodSecurityToken:
				return runOCIForAuth(cmd, []string{"session", "authenticate", "--profile-name", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath, "--region", ctx.Region})
			case config.AuthMethodAPIKey:
				return runOCIForAuth(cmd, []string{"setup", "config", "--profile", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath})
			case config.AuthMethodInstancePrincipal:
				return runOCIForAuth(cmd, []string{"setup", "instance-principal"})
			default:
				return fmt.Errorf("auth method %s does not support login flow; try `oci-context auth validate`", method)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "refresh",
		Short: "Refresh authentication material when supported",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			method := config.NormalizeAuthMethod(ctx.AuthMethod)
			if method != config.AuthMethodSecurityToken {
				return fmt.Errorf("refresh is only supported for security_token auth")
			}
			return runOCIForAuth(cmd, []string{"session", "refresh", "--profile", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath})
		},
	})

	var ensureOutput string
	var ensureLogin bool
	ensureCmd := &cobra.Command{
		Use:   "ensure",
		Short: "Validate auth and refresh it when supported",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			name := ctx.Name
			if name == "" {
				name = cfg.CurrentContext
			}
			result, err := runAuthEnsure(cmd, cfg, ctx, name, authEnsureOptions{
				Login:         ensureLogin,
				NoInteractive: commandNoInteractive(cmd),
			})
			if printErr := printAuthEnsureResult(cmd, result, ensureOutput); printErr != nil {
				return printErr
			}
			return err
		},
	}
	ensureCmd.Flags().StringVarP(&ensureOutput, "output", "o", "text", "Output format: text|json|yaml")
	ensureCmd.Flags().BoolVar(&ensureLogin, "login", false, "Run interactive login when validate/refresh cannot recover auth")
	cmd.AddCommand(ensureCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate current auth by making a lightweight identity call",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			method := config.NormalizeAuthMethod(ctx.AuthMethod)
			homeRegion, err := validateAuthContext(cmd, ctx, cfg.Options.OCIConfigPath)
			if err != nil {
				return fmt.Errorf("auth validate failed for method %s: %w", method, err)
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"Auth validate succeeded for %s (%s)\ncontext: home-region=%s (%s) status=%s\n",
				ctx.Name,
				method,
				homeRegion.RegionName,
				homeRegion.RegionKey,
				homeRegion.Status,
			)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "setup",
		Short: "Run setup flow suitable for the selected auth method",
		RunE: func(cmd *cobra.Command, args []string) error {
			if commandNoInteractive(cmd) {
				return interactiveDisabledError()
			}
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, ctx, err := loadTarget(path)
			if err != nil {
				return err
			}
			method := config.NormalizeAuthMethod(ctx.AuthMethod)
			switch method {
			case config.AuthMethodAPIKey, config.AuthMethodSecurityToken:
				return runOCIForAuth(cmd, []string{"setup", "config", "--profile", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath})
			case config.AuthMethodInstancePrincipal:
				return runOCIForAuth(cmd, []string{"setup", "instance-principal"})
			default:
				return fmt.Errorf("no setup flow for auth method %s", method)
			}
		},
	})

	pf := cmd.PersistentFlags()
	pf.StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	pf.BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	pf.StringVar(&targetContext, "context", "", "Target context name (default current)")
	pf.BoolVar(&cliNoInteractive, "no-interactive", false, "Disable interactive login/setup flows")
	_ = useGlobal // bound/read through resolvePath via flags
	return cmd
}
