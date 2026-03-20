package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
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
