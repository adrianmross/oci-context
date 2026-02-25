package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	srvipc "github.com/adrianmross/oci-context/internal/ipc"
	"github.com/adrianmross/oci-context/pkg/config"
	ipcmsg "github.com/adrianmross/oci-context/pkg/ipc"
)

// Service holds daemon state.
type Service struct {
	cfgPath string
	cfg     config.Config
}

// NewService loads config and returns a Service.
func NewService(cfgPath string) (*Service, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	return &Service{cfgPath: cfgPath, cfg: cfg}, nil
}

// Serve runs the IPC server.
func (s *Service) Serve() error {
	return srvipc.Serve(s.cfg.Options.SocketPath, s.handle)
}

func (s *Service) handle(req ipcmsg.Request) (interface{}, error) {
	switch req.Method {
	case "get_current":
		return s.getCurrent()
	case "list":
		return s.cfg.Contexts, nil
	case "use_context":
		return s.useContext(req.Name)
	case "add_context":
		return s.addContext(req.Context)
	case "delete_context":
		return s.deleteContext(req.Name)
	case "export":
		return s.export(req.Format)
	default:
		return nil, srvipc.ErrNotImplemented
	}
}

func (s *Service) getCurrent() (interface{}, error) {
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
	if err := s.cfg.UpsertContext(ctx); err != nil {
		return nil, err
	}
	if err := config.Save(s.cfgPath, s.cfg); err != nil {
		return nil, err
	}
	return ctx, nil
}

func (s *Service) deleteContext(name string) (interface{}, error) {
	if err := s.cfg.DeleteContext(name); err != nil {
		return nil, err
	}
	if err := config.Save(s.cfgPath, s.cfg); err != nil {
		return nil, err
	}
	return map[string]string{"deleted": name}, nil
}

func (s *Service) export(format string) (interface{}, error) {
	ctx, err := s.getCurrent()
	if err != nil {
		return nil, err
	}
	c := ctx.(config.Context)

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
