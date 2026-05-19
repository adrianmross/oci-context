package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type versionResult struct {
	Version string `json:"version" yaml:"version"`
	Commit  string `json:"commit" yaml:"commit"`
	Date    string `json:"date" yaml:"date"`
	Summary string `json:"summary" yaml:"summary"`
}

func newVersionCmd() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build version metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printVersionResult(cmd, buildVersionResult(), output)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
	return cmd
}

func buildVersionResult() versionResult {
	return versionResult{
		Version: version,
		Commit:  commit,
		Date:    date,
		Summary: buildVersionString(),
	}
}

func printVersionResult(cmd *cobra.Command, result versionResult, output string) error {
	switch strings.ToLower(output) {
	case "", "text":
		_, err := fmt.Fprintln(cmd.OutOrStdout(), result.Summary)
		return err
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
