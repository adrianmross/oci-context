package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type authServiceResolvePathFunc func(*cobra.Command) (string, error)

type tokenServiceImportDocument struct {
	TokenServices      []config.TokenService     `yaml:"token_services" json:"token_services"`
	CamelTokenServices []handoffTokenServiceSpec `yaml:"tokenServices" json:"tokenServices"`
}

type handoffTokenServiceSpec struct {
	Name                   string `yaml:"name" json:"name"`
	Type                   string `yaml:"type" json:"type"`
	Flow                   string `yaml:"flow" json:"flow"`
	Issuer                 string `yaml:"issuer" json:"issuer"`
	AuthorizationEndpoint  string `yaml:"authorizationEndpoint" json:"authorizationEndpoint"`
	TokenEndpoint          string `yaml:"tokenEndpoint" json:"tokenEndpoint"`
	ClientID               string `yaml:"clientId" json:"clientId"`
	ClientSecretEnv        string `yaml:"clientSecretEnv" json:"clientSecretEnv"`
	Scope                  string `yaml:"scope" json:"scope"`
	RedirectURL            string `yaml:"redirectUrl" json:"redirectUrl"`
	PrivateKeyFileEnv      string `yaml:"privateKeyFileEnv" json:"privateKeyFileEnv"`
	KeyID                  string `yaml:"keyId" json:"keyId"`
	JWTAudience            string `yaml:"jwtAudience" json:"jwtAudience"`
	AssertionCommandEnv    string `yaml:"assertionCommandEnv" json:"assertionCommandEnv"`
	SubjectTokenCommandEnv string `yaml:"subjectTokenCommandEnv" json:"subjectTokenCommandEnv"`
	SubjectTokenType       string `yaml:"subjectTokenType" json:"subjectTokenType"`
	RequestedTokenType     string `yaml:"requestedTokenType" json:"requestedTokenType"`
}

type authServiceView struct {
	Name                          string `json:"name" yaml:"name"`
	Type                          string `json:"type,omitempty" yaml:"type,omitempty"`
	Flow                          string `json:"flow,omitempty" yaml:"flow,omitempty"`
	Issuer                        string `json:"issuer,omitempty" yaml:"issuer,omitempty"`
	ClientID                      string `json:"client_id,omitempty" yaml:"client_id,omitempty"`
	Scope                         string `json:"scope,omitempty" yaml:"scope,omitempty"`
	RedirectURL                   string `json:"redirect_url,omitempty" yaml:"redirect_url,omitempty"`
	TokenEndpoint                 string `json:"token_endpoint,omitempty" yaml:"token_endpoint,omitempty"`
	AuthorizationEndpoint         string `json:"authorization_endpoint,omitempty" yaml:"authorization_endpoint,omitempty"`
	ClientSecretConfigured        bool   `json:"client_secret_configured" yaml:"client_secret_configured"`
	ClientSecretEnv               string `json:"client_secret_env,omitempty" yaml:"client_secret_env,omitempty"`
	PrivateKeyFileConfigured      bool   `json:"private_key_file_configured" yaml:"private_key_file_configured"`
	PrivateKeyFileEnv             string `json:"private_key_file_env,omitempty" yaml:"private_key_file_env,omitempty"`
	AssertionCommandConfigured    bool   `json:"assertion_command_configured" yaml:"assertion_command_configured"`
	AssertionCommandEnv           string `json:"assertion_command_env,omitempty" yaml:"assertion_command_env,omitempty"`
	SubjectTokenCommandEnv        string `json:"subject_token_command_env,omitempty" yaml:"subject_token_command_env,omitempty"`
	SubjectTokenCommandConfigured bool   `json:"subject_token_command_configured" yaml:"subject_token_command_configured"`
}

type authServiceImportResult struct {
	ConfigPath string   `json:"config_path" yaml:"config_path"`
	DryRun     bool     `json:"dry_run" yaml:"dry_run"`
	Added      []string `json:"added" yaml:"added"`
	Updated    []string `json:"updated" yaml:"updated"`
	Unchanged  []string `json:"unchanged" yaml:"unchanged"`
}

func newAuthServiceCmd(resolvePath authServiceResolvePathFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage token services for command handoff",
	}
	cmd.AddCommand(newAuthServiceListCmd(resolvePath))
	cmd.AddCommand(newAuthServiceImportCmd(resolvePath))
	return cmd
}

func newAuthServiceListCmd(resolvePath authServiceResolvePathFunc) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured token services",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			views := make([]authServiceView, 0, len(cfg.TokenServices))
			for _, service := range cfg.TokenServices {
				views = append(views, viewTokenService(service))
			}
			return printAuthServiceList(cmd, views, output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
	return cmd
}

func newAuthServiceImportCmd(resolvePath authServiceResolvePathFunc) *cobra.Command {
	var file string
	var output string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import --file oci-context-token-services.yml",
		Short: "Import token services from an oci-idm handoff file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(file) == "" {
				return fmt.Errorf("--file is required")
			}
			services, err := readTokenServicesImport(file)
			if err != nil {
				return err
			}
			path, err := resolvePath(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			result := importTokenServices(&cfg, services)
			result.ConfigPath = path
			result.DryRun = dryRun
			if !dryRun {
				if err := config.Save(path, cfg); err != nil {
					return err
				}
			}
			return printAuthServiceImportResult(cmd, result, output)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Path to oci-idm oci-context handoff YAML or JSON")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "Output format: text|json|yaml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing config")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func readTokenServicesImport(path string) ([]config.TokenService, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc tokenServiceImportDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	services := make([]config.TokenService, 0, len(doc.TokenServices)+len(doc.CamelTokenServices))
	services = append(services, doc.TokenServices...)
	for _, service := range doc.CamelTokenServices {
		services = append(services, service.toConfig())
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no token services found in %s", path)
	}
	for _, service := range services {
		if strings.TrimSpace(service.Name) == "" {
			return nil, fmt.Errorf("token service name is required")
		}
	}
	return services, nil
}

func (s handoffTokenServiceSpec) toConfig() config.TokenService {
	return config.TokenService{
		Name:                   s.Name,
		Type:                   s.Type,
		Flow:                   s.Flow,
		Issuer:                 s.Issuer,
		AuthorizationEndpoint:  s.AuthorizationEndpoint,
		TokenEndpoint:          s.TokenEndpoint,
		ClientID:               s.ClientID,
		ClientSecretEnv:        s.ClientSecretEnv,
		Scope:                  s.Scope,
		RedirectURL:            s.RedirectURL,
		PrivateKeyFileEnv:      s.PrivateKeyFileEnv,
		KeyID:                  s.KeyID,
		JWTAudience:            s.JWTAudience,
		AssertionCommandEnv:    s.AssertionCommandEnv,
		SubjectTokenCommandEnv: s.SubjectTokenCommandEnv,
		SubjectTokenType:       s.SubjectTokenType,
		RequestedTokenType:     s.RequestedTokenType,
	}
}

func importTokenServices(cfg *config.Config, services []config.TokenService) authServiceImportResult {
	result := authServiceImportResult{}
	for _, service := range services {
		idx := tokenServiceIndex(cfg.TokenServices, service.Name)
		if idx == -1 {
			cfg.TokenServices = append(cfg.TokenServices, service)
			result.Added = append(result.Added, service.Name)
			continue
		}
		if tokenServicesEqual(cfg.TokenServices[idx], service) {
			result.Unchanged = append(result.Unchanged, service.Name)
			continue
		}
		cfg.TokenServices[idx] = service
		result.Updated = append(result.Updated, service.Name)
	}
	return result
}

func tokenServiceIndex(services []config.TokenService, name string) int {
	for i, service := range services {
		if service.Name == name {
			return i
		}
	}
	return -1
}

func tokenServicesEqual(a, b config.TokenService) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func viewTokenService(service config.TokenService) authServiceView {
	return authServiceView{
		Name:                          service.Name,
		Type:                          service.Type,
		Flow:                          service.Flow,
		Issuer:                        firstNonEmpty(service.Issuer, firstString(service.IssuerEnvs), service.IssuerEnv),
		ClientID:                      firstNonEmpty(service.ClientID, firstString(service.ClientIDEnvs), service.ClientIDEnv),
		Scope:                         firstNonEmpty(service.Scope, firstString(service.ScopeEnvs), service.ScopeEnv),
		RedirectURL:                   firstNonEmpty(service.RedirectURL, firstString(service.RedirectURLEnvs), service.RedirectURLEnv),
		TokenEndpoint:                 firstNonEmpty(service.TokenEndpoint, firstString(service.TokenEndpointEnvs), service.TokenEndpointEnv),
		AuthorizationEndpoint:         firstNonEmpty(service.AuthorizationEndpoint, firstString(service.AuthorizationEndpointEnvs), service.AuthorizationEndpointEnv),
		ClientSecretConfigured:        service.ClientSecret != "" || service.ClientSecretEnv != "" || len(service.ClientSecretEnvs) > 0,
		ClientSecretEnv:               firstNonEmpty(service.ClientSecretEnv, firstString(service.ClientSecretEnvs)),
		PrivateKeyFileConfigured:      service.PrivateKeyFile != "" || service.PrivateKeyFileEnv != "",
		PrivateKeyFileEnv:             service.PrivateKeyFileEnv,
		AssertionCommandConfigured:    service.AssertionCommand != "" || service.AssertionCommandEnv != "",
		AssertionCommandEnv:           service.AssertionCommandEnv,
		SubjectTokenCommandConfigured: service.SubjectTokenCommand != "" || service.SubjectTokenCommandEnv != "",
		SubjectTokenCommandEnv:        service.SubjectTokenCommandEnv,
	}
}

func printAuthServiceList(cmd *cobra.Command, services []authServiceView, output string) error {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "", "text":
		for _, service := range services {
			fmt.Fprintf(cmd.OutOrStdout(), "%s flow=%s client_id=%s scope=%s\n", service.Name, service.Flow, service.ClientID, service.Scope)
		}
		return nil
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(services)
	case "yaml", "yml":
		enc := yaml.NewEncoder(cmd.OutOrStdout())
		defer enc.Close()
		return enc.Encode(services)
	default:
		return fmt.Errorf("unsupported output format: %s", output)
	}
}

func printAuthServiceImportResult(cmd *cobra.Command, result authServiceImportResult, output string) error {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "", "text":
		if result.DryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Would import token services into %s\n", result.ConfigPath)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Imported token services into %s\n", result.ConfigPath)
		}
		if len(result.Added) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "added: %s\n", strings.Join(result.Added, ", "))
		}
		if len(result.Updated) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "updated: %s\n", strings.Join(result.Updated, ", "))
		}
		if len(result.Unchanged) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "unchanged: %s\n", strings.Join(result.Unchanged, ", "))
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

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
