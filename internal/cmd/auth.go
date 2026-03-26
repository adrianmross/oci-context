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
	return resp.Data, nil
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

	cmd.AddCommand(&cobra.Command{
		Use:   "methods",
		Short: "List supported auth methods",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Supported auth methods:")
			fmt.Fprintln(cmd.OutOrStdout(), "- api_key: API signing key (default)")
			fmt.Fprintln(cmd.OutOrStdout(), "- security_token: Session token from `oci session authenticate`")
			fmt.Fprintln(cmd.OutOrStdout(), "- instance_principal: Compute instance principal")
			fmt.Fprintln(cmd.OutOrStdout(), "- resource_principal: Resource principal")
			fmt.Fprintln(cmd.OutOrStdout(), "- instance_obo_user: Cloud Shell delegation token")
			fmt.Fprintln(cmd.OutOrStdout(), "- oke_workload_identity: OKE workload identity")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
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
			if st, err := fetchDaemonAuthStatus(cfg, name); err == nil {
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
	})

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
				return runOCI(cmd, []string{"session", "authenticate", "--profile-name", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath, "--region", ctx.Region})
			case config.AuthMethodAPIKey:
				return runOCI(cmd, []string{"setup", "config", "--profile", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath})
			case config.AuthMethodInstancePrincipal:
				return runOCI(cmd, []string{"setup", "instance-principal"})
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
			return runOCI(cmd, []string{"session", "refresh", "--profile", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath})
		},
	})

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
			ociArgs := buildAuthValidateOCIArgs(ctx, cfg.Options.OCIConfigPath)
			out, err := runOCICapture(cmd, ociArgs)
			if err != nil {
				return fmt.Errorf("auth validate failed for method %s: %w", method, err)
			}
			homeRegion, err := findHomeRegion(out)
			if err != nil {
				return fmt.Errorf("auth validate succeeded but could not resolve home-region context: %w", err)
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
					return runOCI(cmd, []string{"setup", "config", "--profile", ctx.Profile, "--config-file", cfg.Options.OCIConfigPath})
			case config.AuthMethodInstancePrincipal:
				return runOCI(cmd, []string{"setup", "instance-principal"})
			default:
				return fmt.Errorf("no setup flow for auth method %s", method)
			}
		},
	})

	pf := cmd.PersistentFlags()
	pf.StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	pf.BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	pf.StringVar(&targetContext, "context", "", "Target context name (default current)")
	_ = useGlobal // bound/read through resolvePath via flags
	return cmd
}
