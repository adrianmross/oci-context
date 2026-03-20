package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	srvipc "github.com/adrianmross/oci-context/internal/ipc"
	"github.com/adrianmross/oci-context/pkg/config"
	ipcmsg "github.com/adrianmross/oci-context/pkg/ipc"
)

// ServiceOptions controls daemon background behaviors.
type ServiceOptions struct {
	AutoRefresh            bool
	ValidateInterval       time.Duration
	RefreshInterval        time.Duration
	RefreshOnValidateError bool
	ValidateOnStart        bool
}

// DefaultServiceOptions returns conservative defaults.
func DefaultServiceOptions() ServiceOptions {
	return ServiceOptions{
		AutoRefresh:            false,
		ValidateInterval:       5 * time.Minute,
		RefreshInterval:        15 * time.Minute,
		RefreshOnValidateError: true,
		ValidateOnStart:        true,
	}
}

// AuthStatus is the daemon runtime auth status for a context.
type AuthStatus struct {
	ContextName      string `json:"context_name"`
	AuthMethod       string `json:"auth_method"`
	HomeRegionName   string `json:"home_region_name,omitempty"`
	HomeRegionKey    string `json:"home_region_key,omitempty"`
	HomeRegionStatus string `json:"home_region_status,omitempty"`

	LastValidatedAt string `json:"last_validated_at,omitempty"`
	LastValidateOK  bool   `json:"last_validate_ok"`
	LastRefreshedAt string `json:"last_refreshed_at,omitempty"`
	LastRefreshOK   bool   `json:"last_refresh_ok"`
	LastError       string `json:"last_error,omitempty"`
	Mode            string `json:"mode"`
}

type authStatusState struct {
	ContextName      string
	AuthMethod       string
	HomeRegionName   string
	HomeRegionKey    string
	HomeRegionStatus string

	LastValidatedAt time.Time
	LastValidateOK  bool
	LastRefreshedAt time.Time
	LastRefreshOK   bool
	LastError       string
	Mode            string
}

// Service holds daemon state.
type Service struct {
	cfgPath string

	mu  sync.RWMutex
	cfg config.Config

	opts ServiceOptions

	statusMu sync.RWMutex
	status   map[string]authStatusState
}

// NewService loads config and returns a Service.
func NewService(cfgPath string) (*Service, error) {
	return NewServiceWithOptions(cfgPath, DefaultServiceOptions())
}

// NewServiceWithOptions loads config and returns a Service with explicit options.
func NewServiceWithOptions(cfgPath string, opts ServiceOptions) (*Service, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if opts.ValidateInterval <= 0 {
		opts.ValidateInterval = 5 * time.Minute
	}
	if opts.RefreshInterval <= 0 {
		opts.RefreshInterval = 15 * time.Minute
	}
	return &Service{cfgPath: cfgPath, cfg: cfg, opts: opts, status: make(map[string]authStatusState)}, nil
}

// Serve runs the IPC server.
func (s *Service) Serve() error {
	if s.opts.AutoRefresh {
		go s.authMaintenanceLoop()
	}
	return srvipc.Serve(s.currentConfig().Options.SocketPath, s.handle)
}

func (s *Service) handle(req ipcmsg.Request) (interface{}, error) {
	switch req.Method {
	case "get_current":
		return s.getCurrent()
	case "list":
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.cfg.Contexts, nil
	case "use_context":
		return s.useContext(req.Name)
	case "add_context":
		return s.addContext(req.Context)
	case "delete_context":
		return s.deleteContext(req.Name)
	case "export":
		return s.export(req.Format)
	case "auth_status":
		return s.authStatus(req.Name)
	default:
		return nil, srvipc.ErrNotImplemented
	}
}

func (s *Service) currentConfig() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Service) reloadConfig() error {
	cfg, err := config.Load(s.cfgPath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return nil
}

func (s *Service) getCurrent() (interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.CurrentContext == "" {
		return nil, errors.New("no current context set")
	}
	ctx, err := s.cfg.GetContext(s.cfg.CurrentContext)
	if err != nil {
		return nil, err
	}
	return ctx, nil
}

func (s *Service) useContext(name string) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.cfg.GetContext(name); err != nil {
		return nil, err
	}
	s.cfg.CurrentContext = name
	if err := config.Save(s.cfgPath, s.cfg); err != nil {
		return nil, err
	}
	return map[string]string{"current_context": name}, nil
}

func (s *Service) addContext(raw json.RawMessage) (interface{}, error) {
	var ctx config.Context
	if err := json.Unmarshal(raw, &ctx); err != nil {
		return nil, err
	}
	if err := ctx.Validate(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cfg.UpsertContext(ctx); err != nil {
		return nil, err
	}
	if err := config.Save(s.cfgPath, s.cfg); err != nil {
		return nil, err
	}
	return ctx, nil
}

func (s *Service) deleteContext(name string) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cfg.DeleteContext(name); err != nil {
		return nil, err
	}
	if err := config.Save(s.cfgPath, s.cfg); err != nil {
		return nil, err
	}
	return map[string]string{"deleted": name}, nil
}

func (s *Service) export(format string) (interface{}, error) {
	ctxAny, err := s.getCurrent()
	if err != nil {
		return nil, err
	}
	c := ctxAny.(config.Context)

	switch format {
	case "env":
		lines := []string{
			fmt.Sprintf("OCI_CLI_PROFILE=%s", c.Profile),
			fmt.Sprintf("OCI_TENANCY_OCID=%s", c.TenancyOCID),
			fmt.Sprintf("OCI_COMPARTMENT_OCID=%s", c.CompartmentOCID),
		}
		if c.Region != "" {
			lines = append(lines, fmt.Sprintf("OCI_REGION=%s", c.Region))
		}
		return map[string][]string{"env": lines}, nil
	case "json", "":
		return c, nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

func (s *Service) authStatus(name string) (interface{}, error) {
	if err := s.reloadConfig(); err != nil {
		return nil, err
	}
	cfg := s.currentConfig()
	if name == "" {
		name = cfg.CurrentContext
	}
	if name == "" {
		return nil, errors.New("no current context set")
	}
	ctx, err := cfg.GetContext(name)
	if err != nil {
		return nil, err
	}

	s.statusMu.RLock()
	st := s.status[name]
	s.statusMu.RUnlock()

	if st.ContextName == "" {
		st.ContextName = name
		st.AuthMethod = config.NormalizeAuthMethod(ctx.AuthMethod)
		st.Mode = authModeForMethod(st.AuthMethod)
	}
	return toAuthStatus(st), nil
}

func toAuthStatus(st authStatusState) AuthStatus {
	out := AuthStatus{
		ContextName:      st.ContextName,
		AuthMethod:       st.AuthMethod,
		HomeRegionName:   st.HomeRegionName,
		HomeRegionKey:    st.HomeRegionKey,
		HomeRegionStatus: st.HomeRegionStatus,
		LastValidateOK:   st.LastValidateOK,
		LastRefreshOK:    st.LastRefreshOK,
		LastError:        st.LastError,
		Mode:             st.Mode,
	}
	if !st.LastValidatedAt.IsZero() {
		out.LastValidatedAt = st.LastValidatedAt.UTC().Format(time.RFC3339)
	}
	if !st.LastRefreshedAt.IsZero() {
		out.LastRefreshedAt = st.LastRefreshedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func authModeForMethod(method string) string {
	if config.NormalizeAuthMethod(method) == config.AuthMethodSecurityToken {
		return "managed-security-token"
	}
	return "validate-only"
}

func (s *Service) authMaintenanceLoop() {
	validateTicker := time.NewTicker(s.opts.ValidateInterval)
	refreshTicker := time.NewTicker(s.opts.RefreshInterval)
	defer validateTicker.Stop()
	defer refreshTicker.Stop()

	if s.opts.ValidateOnStart {
		s.maintainAuth("startup-validate")
	}

	for {
		select {
		case <-validateTicker.C:
			s.maintainAuth("validate")
		case <-refreshTicker.C:
			s.maintainAuth("refresh")
		}
	}
}

func (s *Service) maintainAuth(reason string) {
	if err := s.reloadConfig(); err != nil {
		s.setStatusError("", "", fmt.Sprintf("reload config: %v", err))
		return
	}
	cfg := s.currentConfig()
	if cfg.CurrentContext == "" {
		s.setStatusError("", "", "no current context set")
		return
	}
	ctx, err := cfg.GetContext(cfg.CurrentContext)
	if err != nil {
		s.setStatusError(cfg.CurrentContext, "", err.Error())
		return
	}
	ctxName := cfg.CurrentContext
	method := config.NormalizeAuthMethod(ctx.AuthMethod)

	s.statusMu.Lock()
	st := s.status[ctxName]
	st.ContextName = ctxName
	st.AuthMethod = method
	st.Mode = authModeForMethod(method)
	s.status[ctxName] = st
	s.statusMu.Unlock()

	if method != config.AuthMethodSecurityToken {
		s.validateCurrentContext(cfg, ctxName, ctx, reason)
		return
	}

	if reason == "refresh" {
		s.refreshSecurityToken(cfg, ctxName, ctx, "interval")
		s.validateCurrentContext(cfg, ctxName, ctx, "post-refresh")
		return
	}

	if err := s.validateCurrentContext(cfg, ctxName, ctx, reason); err != nil && s.opts.RefreshOnValidateError {
		s.refreshSecurityToken(cfg, ctxName, ctx, "validate-failed")
		_ = s.validateCurrentContext(cfg, ctxName, ctx, "post-failed-validate-refresh")
	}
}

func (s *Service) setStatusError(ctxName, method, msg string) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	st := s.status[ctxName]
	if st.ContextName == "" {
		st.ContextName = ctxName
	}
	if method != "" {
		st.AuthMethod = method
		st.Mode = authModeForMethod(method)
	}
	st.LastError = msg
	s.status[ctxName] = st
}

func (s *Service) validateCurrentContext(cfg config.Config, ctxName string, ctx config.Context, reason string) error {
	args := buildValidateOCIArgs(ctx, cfg.Options.OCIConfigPath)
	out, err := runOCICapture(args)
	now := time.Now()

	s.statusMu.Lock()
	st := s.status[ctxName]
	st.ContextName = ctxName
	st.AuthMethod = config.NormalizeAuthMethod(ctx.AuthMethod)
	st.Mode = authModeForMethod(st.AuthMethod)
	st.LastValidatedAt = now
	if err != nil {
		st.LastValidateOK = false
		st.LastError = fmt.Sprintf("%s validate failed: %v", reason, err)
		s.status[ctxName] = st
		s.statusMu.Unlock()
		return err
	}

	home, parseErr := findHomeRegion(out)
	if parseErr != nil {
		st.LastValidateOK = false
		st.LastError = fmt.Sprintf("%s validate parse failed: %v", reason, parseErr)
		s.status[ctxName] = st
		s.statusMu.Unlock()
		return parseErr
	}
	st.HomeRegionName = home.RegionName
	st.HomeRegionKey = home.RegionKey
	st.HomeRegionStatus = home.Status
	st.LastValidateOK = true
	st.LastError = ""
	s.status[ctxName] = st
	s.statusMu.Unlock()
	return nil
}

func (s *Service) refreshSecurityToken(cfg config.Config, ctxName string, ctx config.Context, reason string) {
	args := buildRefreshOCIArgs(ctx, cfg.Options.OCIConfigPath)
	err := runOCI(args)
	now := time.Now()

	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	st := s.status[ctxName]
	st.ContextName = ctxName
	st.AuthMethod = config.NormalizeAuthMethod(ctx.AuthMethod)
	st.Mode = authModeForMethod(st.AuthMethod)
	st.LastRefreshedAt = now
	if err != nil {
		st.LastRefreshOK = false
		st.LastError = fmt.Sprintf("%s refresh failed: %v", reason, err)
	} else {
		st.LastRefreshOK = true
		st.LastError = ""
	}
	s.status[ctxName] = st
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

func buildValidateOCIArgs(ctx config.Context, ociConfigPath string) []string {
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

func buildRefreshOCIArgs(ctx config.Context, ociConfigPath string) []string {
	args := []string{"session", "refresh"}
	if ctx.Profile != "" {
		args = append(args, "--profile", ctx.Profile)
	}
	if ociConfigPath != "" {
		args = append(args, "--config-file", ociConfigPath)
	}
	if ctx.Region != "" {
		args = append(args, "--region", ctx.Region)
	}
	return args
}

func runOCICapture(args []string) ([]byte, error) {
	cmd := exec.Command("oci", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err != nil {
			return nil, fmt.Errorf("failed to execute oci CLI (%w): install with `pip install oci-cli` or ensure it is in PATH", ee.Err)
		}
		return nil, err
	}
	return out, nil
}

func runOCI(args []string) error {
	cmd := exec.Command("oci", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err != nil {
			return fmt.Errorf("failed to execute oci CLI (%w): install with `pip install oci-cli` or ensure it is in PATH", ee.Err)
		}
		return err
	}
	return nil
}

// EnsureConfig ensures config exists at path.
func EnsureConfig(path string) (string, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = fmt.Sprintf("%s/.oci-context/config.yml", home)
	}
	if err := config.EnsureDefaultConfig(path); err != nil {
		return "", err
	}
	return path, nil
}
