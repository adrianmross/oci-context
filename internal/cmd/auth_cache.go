package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

type authCacheStatusResult struct {
	Backend string `json:"backend"`
}

func newAuthCacheCmd(resolvePath authResolvePathFunc) *cobra.Command {
	cmd := &cobra.Command{Use: "cache", Short: "Manage the OAuth token cache"}
	var output string
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the selected OAuth token cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			backend := config.NormalizeOAuthTokenCache(cfg.Options.OAuthTokenCache)
			if !config.IsValidOAuthTokenCache(backend) {
				return fmt.Errorf("unsupported OAuth token cache %q", cfg.Options.OAuthTokenCache)
			}
			result := authCacheStatusResult{Backend: backend}
			switch output {
			case "", "text":
				fmt.Fprintln(cmd.OutOrStdout(), result.Backend)
				return nil
			case "json":
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			default:
				return fmt.Errorf("unsupported output format: %s", output)
			}
		},
	}
	statusCmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json")
	cmd.AddCommand(statusCmd)

	useCmd := &cobra.Command{
		Use:   "use <file|keychain>",
		Short: "Select the OAuth token cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backend := config.NormalizeOAuthTokenCache(args[0])
			if !config.IsValidOAuthTokenCache(backend) {
				return fmt.Errorf("unsupported OAuth token cache %q", args[0])
			}
			if backend == config.OAuthTokenCacheKeychain && !authTokenKeychainAvailable() {
				return fmt.Errorf("OAuth Keychain cache requires a macOS build with cgo enabled")
			}
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			cfg.Options.OAuthTokenCache = backend
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OAuth token cache: %s\n", backend)
			return nil
		},
	}
	cmd.AddCommand(useCmd)
	return cmd
}
