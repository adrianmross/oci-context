package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
)

type deviceBundle struct {
	Context config.Context `json:"context"`
	Profile string         `json:"profile"`
	Region  string         `json:"region"`
}

func newDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "device", Short: "Prepare a context for another device"}
	cmd.AddCommand(newDeviceExportCmd())
	return cmd
}

func newDeviceExportCmd() *cobra.Command {
	var cfgPath, output, targetContext string
	var useGlobal, includePrivateKey bool

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Create a portable context bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			name := targetContext
			if name == "" {
				name = cfg.CurrentContext
			}
			if name == "" {
				return fmt.Errorf("no context selected")
			}
			ctx, err := cfg.GetContext(name)
			if err != nil {
				return err
			}
			if output == "" {
				return fmt.Errorf("--output is required")
			}
			if err := os.MkdirAll(output, 0o700); err != nil {
				return err
			}

			bundle := deviceBundle{Context: ctx, Profile: ctx.Profile, Region: ctx.Region}
			data, err := json.MarshalIndent(bundle, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(output, "context.json"), append(data, '\n'), 0o600); err != nil {
				return err
			}

			profile, keyPath, err := profileBlock(cfg.Options.OCIConfigPath, ctx.Profile, includePrivateKey)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(output, "oci-profile"), []byte(profile), 0o600); err != nil {
				return err
			}

			if includePrivateKey {
				if keyPath == "" {
					return fmt.Errorf("profile %s has no key_file", ctx.Profile)
				}
				key, err := os.ReadFile(keyPath)
				if err != nil {
					return fmt.Errorf("read private key: %w", err)
				}
				if err := os.WriteFile(filepath.Join(output, "oci_api_key.pem"), key, 0o600); err != nil {
					return err
				}
			}

			instructions := "# OCI context device bundle\n\n"
			instructions += "This bundle contains context metadata and one OCI profile.\n"
			if includePrivateKey {
				instructions += "It also contains a private API key; protect and delete it after import.\n"
			} else {
				instructions += "No private key is included. Run `oci setup config` on the destination.\n"
			}
			if includePrivateKey {
				instructions += "\nOn the destination, run:\n\nmkdir -p ~/.oci/sessions/" + ctx.Profile + "\ncp oci_api_key.pem ~/.oci/sessions/" + ctx.Profile + "/oci_api_key.pem\n"
			}
			instructions += "\nMerge `oci-profile` into `~/.oci/config`, then run:\n\n"
			instructions += "oci-context import --global\noci-context use " + ctx.Name + " --global\n"
			if err := os.WriteFile(filepath.Join(output, "README.md"), []byte(instructions), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote device bundle to %s\n", output)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to oci-context config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config")
	cmd.Flags().StringVar(&targetContext, "context", "", "Context to export (default current)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output directory")
	cmd.Flags().BoolVar(&includePrivateKey, "include-private-key", false, "Copy the profile private key into the bundle")
	return cmd
}

func profileBlock(path, name string, includePrivateKey bool) (string, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(string(b), "\n")
	var out []string
	var keyPath string
	in := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			in = strings.TrimSpace(trimmed[1:len(trimmed)-1]) == name
			if in {
				out = append(out, line)
			}
			continue
		}
		if !in || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		var key, value string
		value = trimmed
		if parts := strings.SplitN(trimmed, "=", 2); len(parts) == 2 {
			key, value = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
		if key == "security_token_file" {
			continue
		}
		if !includePrivateKey && key == "key_file" {
			continue
		}
		if includePrivateKey && key == "key_file" {
			keyPath = value
			out = append(out, "key_file=~/.oci/sessions/"+name+"/oci_api_key.pem")
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return "", "", fmt.Errorf("OCI profile %s not found", name)
	}
	return strings.Join(out, "\n") + "\n", keyPath, nil
}

func profileKeyPath(path, name string) (string, error) {
	_, keyPath, err := profileBlock(path, name, true)
	return keyPath, err
}
