package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type pathsResult struct {
	ConfigPath        string                `json:"config_path" yaml:"config_path"`
	ConfigSource      string                `json:"config_source" yaml:"config_source"`
	WorkingDirectory  string                `json:"working_directory,omitempty" yaml:"working_directory,omitempty"`
	GlobalConfigPath  string                `json:"global_config_path" yaml:"global_config_path"`
	ProjectCandidates []configPathCandidate `json:"project_candidates,omitempty" yaml:"project_candidates,omitempty"`
	OCIConfigPath     string                `json:"oci_config_path,omitempty" yaml:"oci_config_path,omitempty"`
	SocketPath        string                `json:"socket_path,omitempty" yaml:"socket_path,omitempty"`
	ConfigError       string                `json:"config_error,omitempty" yaml:"config_error,omitempty"`
}

func newPathsCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	var output string

	cmd := &cobra.Command{
		Use:   "paths",
		Short: "Show resolved oci-context paths",
		RunE: func(cmd *cobra.Command, args []string) error {
			g, err := cmd.Flags().GetBool("global")
			if err != nil {
				return err
			}
			if g {
				useGlobal = true
			}
			resolution, err := resolveConfigPathInfo(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			result := buildPathsResult(resolution)
			return printPathsResult(cmd, result, output)
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
	return cmd
}

func buildPathsResult(resolution configPathResolution) pathsResult {
	result := pathsResult{
		ConfigPath:        resolution.Path,
		ConfigSource:      resolution.Source,
		WorkingDirectory:  resolution.WorkingDirectory,
		GlobalConfigPath:  resolution.GlobalPath,
		ProjectCandidates: resolution.ProjectCandidates,
	}
	cfg, err := config.Load(resolution.Path)
	if err != nil {
		result.ConfigError = err.Error()
		return result
	}
	result.OCIConfigPath = cfg.Options.OCIConfigPath
	result.SocketPath = cfg.Options.SocketPath
	return result
}

func printPathsResult(cmd *cobra.Command, result pathsResult, output string) error {
	switch strings.ToLower(output) {
	case "", "text":
		fmt.Fprintf(cmd.OutOrStdout(), "config_path: %s\n", result.ConfigPath)
		fmt.Fprintf(cmd.OutOrStdout(), "config_source: %s\n", result.ConfigSource)
		if result.WorkingDirectory != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "working_directory: %s\n", result.WorkingDirectory)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "global_config_path: %s\n", result.GlobalConfigPath)
		if result.OCIConfigPath != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "oci_config_path: %s\n", result.OCIConfigPath)
		}
		if result.SocketPath != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "socket_path: %s\n", result.SocketPath)
		}
		if result.ConfigError != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "config_error: %s\n", result.ConfigError)
		}
		if len(result.ProjectCandidates) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "project_candidates:")
			for _, candidate := range result.ProjectCandidates {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s exists=%t file=%t\n", candidate.Path, candidate.Exists, candidate.IsFile)
			}
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
