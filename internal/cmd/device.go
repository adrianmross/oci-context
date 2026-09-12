package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	cmd.AddCommand(newDeviceExportCmd(), newDeviceImportCmd())
	return cmd
}

func newDeviceExportCmd() *cobra.Command {
	var cfgPath, output, targetContext string
	var useGlobal, includePrivateKey, archive bool

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
				output = name + ".ocix.tar.gz"
				archive = true
			}
			if archive {
				parent := filepath.Dir(output)
				if err := os.MkdirAll(parent, 0o700); err != nil {
					return err
				}
				tmp, err := os.MkdirTemp(parent, ".oci-context-device-")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmp)
				if err := writeDeviceBundle(cfg, ctx, tmp, includePrivateKey); err != nil {
					return err
				}
				if err := archiveDeviceBundle(tmp, output); err != nil {
					return err
				}
			} else {
				if err := writeDeviceBundle(cfg, ctx, output, includePrivateKey); err != nil {
					return err
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote device bundle to %s\n", output)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to oci-context config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config")
	cmd.Flags().StringVar(&targetContext, "context", "", "Context to export (default current)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output directory or archive file")
	cmd.Flags().BoolVar(&includePrivateKey, "include-private-key", false, "Copy the profile private key into the bundle")
	cmd.Flags().BoolVar(&archive, "archive", false, "Write a .tar.gz bundle to --output")
	return cmd
}

func runDeviceExport(parent *cobra.Command, cfgPath string, useGlobal bool, context, output string, includePrivateKey, archive bool) error {
	cmd := newDeviceExportCmd()
	cmd.SetOut(parent.OutOrStdout())
	cmd.SetErr(parent.ErrOrStderr())
	_ = cmd.Flags().Set("config", cfgPath)
	_ = cmd.Flags().Set("global", fmt.Sprint(useGlobal))
	_ = cmd.Flags().Set("context", context)
	_ = cmd.Flags().Set("output", output)
	_ = cmd.Flags().Set("include-private-key", fmt.Sprint(includePrivateKey))
	_ = cmd.Flags().Set("archive", fmt.Sprint(archive))
	return cmd.RunE(cmd, nil)
}

func writeDeviceBundle(cfg config.Config, ctx config.Context, output string, includePrivateKey bool) error {
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
	instructions += "This bundle contains one OCI context and one OCI profile.\n"
	if includePrivateKey {
		instructions += "It also contains a private API key; protect and delete it after import.\n"
	} else {
		instructions += "No private key is included; configure API-key authentication on the destination.\n"
	}
	instructions += "\nFrom this directory, run:\n\noci-context import --file .\noci-context use " + ctx.Name + " --global\n"
	return os.WriteFile(filepath.Join(output, "README.md"), []byte(instructions), 0o600)
}

func archiveDeviceBundle(source, output string) error {
	f, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.ModTime = time.Time{}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func newDeviceImportCmd() *cobra.Command {
	var cfgPath, input string
	var useGlobal, overwrite bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import one portable context bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			if input == "" {
				return fmt.Errorf("--input is required")
			}
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			bundleDir, cleanup, err := openDeviceBundle(input)
			if err != nil {
				return err
			}
			defer cleanup()
			var bundle deviceBundle
			data, err := os.ReadFile(filepath.Join(bundleDir, "context.json"))
			if err != nil {
				return fmt.Errorf("read context.json: %w", err)
			}
			if err := json.Unmarshal(data, &bundle); err != nil {
				return fmt.Errorf("parse context.json: %w", err)
			}
			if bundle.Profile == "" || bundle.Profile != bundle.Context.Profile || bundle.Region != bundle.Context.Region {
				return fmt.Errorf("context.json has inconsistent profile or region")
			}
			if err := bundle.Context.Validate(); err != nil {
				return fmt.Errorf("invalid context: %w", err)
			}
			if !overwrite {
				if _, err := cfg.GetContext(bundle.Context.Name); err == nil {
					return fmt.Errorf("context %s already exists; use --overwrite", bundle.Context.Name)
				}
			}
			profile, err := os.ReadFile(filepath.Join(bundleDir, "oci-profile"))
			if err != nil {
				return fmt.Errorf("read oci-profile: %w", err)
			}
			if err := mergeOCIProfile(cfg.Options.OCIConfigPath, bundle.Profile, string(profile), overwrite); err != nil {
				return err
			}
			if key, err := os.ReadFile(filepath.Join(bundleDir, "oci_api_key.pem")); err == nil {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				keyDir := filepath.Join(home, ".oci", "sessions", bundle.Profile)
				if err := os.MkdirAll(keyDir, 0o700); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(keyDir, "oci_api_key.pem"), key, 0o600); err != nil {
					return err
				}
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("read private key: %w", err)
			}
			if err := cfg.UpsertContext(bundle.Context); err != nil {
				return err
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Imported context %s from %s\n", bundle.Context.Name, input)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to oci-context config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config")
	cmd.Flags().StringVarP(&input, "input", "i", "", "Bundle directory or .tar.gz file")
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "w", false, "Overwrite the existing profile and context")
	return cmd
}

func runDeviceImport(parent *cobra.Command, cfgPath string, useGlobal bool, input string, overwrite bool) error {
	cmd := newDeviceImportCmd()
	cmd.SetOut(parent.OutOrStdout())
	cmd.SetErr(parent.ErrOrStderr())
	_ = cmd.Flags().Set("config", cfgPath)
	_ = cmd.Flags().Set("global", fmt.Sprint(useGlobal))
	_ = cmd.Flags().Set("input", input)
	_ = cmd.Flags().Set("overwrite", fmt.Sprint(overwrite))
	return cmd.RunE(cmd, nil)
}

func openDeviceBundle(input string) (string, func(), error) {
	info, err := os.Stat(input)
	if err != nil {
		return "", func() {}, err
	}
	if info.IsDir() {
		return input, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "oci-context-device-import-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	f, err := os.Open(input)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("read bundle archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("read bundle archive: %w", err)
		}
		name := filepath.Clean(filepath.FromSlash(hdr.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) || seen[name] || !map[string]bool{"context.json": true, "oci-profile": true, "README.md": true, "oci_api_key.pem": true}[name] {
			cleanup()
			return "", func() {}, fmt.Errorf("invalid bundle entry %q", hdr.Name)
		}
		seen[name] = true
		if hdr.Typeflag != tar.TypeReg {
			cleanup()
			return "", func() {}, fmt.Errorf("bundle entry %q is not a regular file", hdr.Name)
		}
		out, err := os.OpenFile(filepath.Join(tmp, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			cleanup()
			return "", func() {}, copyErr
		}
		if closeErr != nil {
			cleanup()
			return "", func() {}, closeErr
		}
	}
	return tmp, cleanup, nil
}

func looksLikeDeviceBundle(input string) bool {
	info, err := os.Stat(input)
	if err != nil {
		return false
	}
	if info.IsDir() {
		_, err := os.Stat(filepath.Join(input, "context.json"))
		return err == nil
	}
	lower := strings.ToLower(input)
	return strings.HasSuffix(lower, ".ocix.tar.gz") || strings.HasSuffix(lower, ".tgz")
}

func mergeOCIProfile(path, name, block string, overwrite bool) error {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		lines := strings.Split(string(b), "\n")
		var out []string
		in, found := false, false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				section := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
				if section == name {
					if !overwrite {
						return fmt.Errorf("OCI profile %s already exists; use --overwrite", name)
					}
					out = append(out, strings.TrimSuffix(block, "\n"))
					in, found = true, true
					continue
				}
				in = false
			}
			if !in {
				out = append(out, line)
			}
		}
		if found {
			b = []byte(strings.Join(out, "\n"))
		} else {
			b = append(b, []byte("\n"+strings.TrimPrefix(block, "\n"))...)
		}
	} else {
		b = []byte(block)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
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
