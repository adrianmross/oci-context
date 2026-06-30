package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

type toolSetupEnvironmentEntry struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Secret bool   `json:"secret,omitempty"`
}

type toolSetupAuthProfile struct {
	TokenCommand string `json:"tokenCommand"`
}

type toolSetupPayload struct {
	SchemaVersion string                          `json:"schemaVersion"`
	Tool          string                          `json:"tool"`
	Service       string                          `json:"service,omitempty"`
	Profile       string                          `json:"profile,omitempty"`
	TokenCommand  string                          `json:"tokenCommand"`
	AuthProfiles  map[string]toolSetupAuthProfile `json:"authProfiles,omitempty"`
	Environment   []toolSetupEnvironmentEntry     `json:"environment"`
}

func newToolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool",
		Short: "Set up external CLI handoffs from oci-context",
	}
	cmd.AddCommand(newToolSetupCmd())
	return cmd
}

func newToolSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Print setup exports and examples for an external CLI",
	}
	cmd.AddCommand(newToolSetupOChainCmd())
	return cmd
}

func newToolSetupOChainCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var output string
	var shell bool
	var jsonOutput bool
	var includeToken bool
	var service string
	var profile string

	cmd := &cobra.Command{
		Use:   "ochain",
		Short: "Print OChain auth handoff setup for the current token service",
		RunE: func(cmd *cobra.Command, args []string) error {
			useGlobal, err := cmd.Flags().GetBool("global")
			if err != nil {
				return err
			}
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			serviceName := firstNonEmpty(service, cfg.CurrentService)
			tokenCommand := buildOChainTokenCommand(service)
			payload := toolSetupPayload{
				SchemaVersion: "oci-context.tool-setup.v1",
				Tool:          "ochain",
				Service:       serviceName,
				Profile:       profile,
				TokenCommand:  tokenCommand,
				AuthProfiles: map[string]toolSetupAuthProfile{
					profile: {TokenCommand: tokenCommand},
				},
				Environment: []toolSetupEnvironmentEntry{
					{Name: "OCHAIN_TOKEN_COMMAND", Value: tokenCommand, Secret: true},
				},
			}
			if includeToken {
				token, err := runToolSetupToken(cmd, cfg, serviceName)
				if err != nil {
					return err
				}
				payload.Environment = append(payload.Environment, toolSetupEnvironmentEntry{
					Name:   "OCHAIN_TOKEN",
					Value:  token,
					Secret: true,
				})
			}
			return printToolSetupPayload(cmd, payload, output, shell, jsonOutput)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVarP(&output, "output", "o", "json", "Output format: json|shell")
	cmd.Flags().BoolVar(&shell, "shell", false, "Print shell exports")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&includeToken, "include-token", false, "Include a current access token in the setup payload")
	cmd.Flags().StringVar(&service, "service", "", "Token service name for generated commands (default current_service)")
	cmd.Flags().StringVar(&profile, "profile", "oci-context-obp", "OChain auth profile name")
	return cmd
}

func buildOChainTokenCommand(service string) string {
	args := []string{"oci-context", "auth", "token"}
	if strings.TrimSpace(service) != "" {
		args = append(args, "--service", strings.TrimSpace(service))
	}
	args = append(args, "--no-login", "--format", "raw")
	return strings.Join(args, " ")
}

func runToolSetupToken(cmd *cobra.Command, cfg config.Config, service string) (string, error) {
	if cfg.CurrentContext == "" {
		return "", fmt.Errorf("no current context set")
	}
	ctx, err := cfg.GetContext(cfg.CurrentContext)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	tokenCmd := &cobra.Command{}
	tokenCmd.SetOut(&out)
	tokenCmd.SetErr(cmd.ErrOrStderr())
	if err := runAuthToken(tokenCmd, cfg, ctx, tokenServiceOptions{
		Service: service,
	}, authTokenRunOptions{
		Format:  "raw",
		NoLogin: true,
	}); err != nil {
		return "", err
	}
	token := strings.TrimSpace(out.String())
	if token == "" {
		return "", fmt.Errorf("token command produced no token")
	}
	return token, nil
}

func printToolSetupPayload(cmd *cobra.Command, payload toolSetupPayload, output string, shell bool, jsonOutput bool) error {
	if shell || strings.EqualFold(output, "shell") {
		for _, entry := range payload.Environment {
			fmt.Fprintf(cmd.OutOrStdout(), "export %s=%s\n", entry.Name, strconv.Quote(entry.Value))
		}
		return nil
	}
	if jsonOutput {
		output = "json"
	}
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "", "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	default:
		return fmt.Errorf("unsupported output format: %s", output)
	}
}
