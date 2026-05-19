package cmd

import (
	"os"
	"path/filepath"
)

type configPathCandidate struct {
	RelativePath string `json:"relative_path" yaml:"relative_path"`
	Path         string `json:"path" yaml:"path"`
	Exists       bool   `json:"exists" yaml:"exists"`
	IsFile       bool   `json:"is_file" yaml:"is_file"`
}

type configPathResolution struct {
	Path              string                `json:"path" yaml:"path"`
	Source            string                `json:"source" yaml:"source"`
	WorkingDirectory  string                `json:"working_directory" yaml:"working_directory"`
	GlobalPath        string                `json:"global_path" yaml:"global_path"`
	ProjectCandidates []configPathCandidate `json:"project_candidates" yaml:"project_candidates"`
}

// resolveConfigPath returns the config path based on flags and project discovery.
// Priority:
//  1. explicit --config
//  2. if global flag set -> ~/.oci-context/config.yml
//  3. project-local configs (in order):
//     ./.oci-context.yml, ./.oci-context.json,
//     ./.oci-context/config.yml, ./.oci-context/config.json,
//     ./oci-context.yml, ./oci-context.json,
//     ./oci-context/config.yml, ./oci-context/config.json
//  4. fallback to ~/.oci-context/config.yml
func resolveConfigPath(cfg string, global bool) (string, error) {
	resolution, err := resolveConfigPathInfo(cfg, global)
	if err != nil {
		return "", err
	}
	return resolution.Path, nil
}

func resolveConfigPathInfo(cfg string, global bool) (configPathResolution, error) {
	globalPath, err := globalConfigPath()
	if err != nil {
		return configPathResolution{}, err
	}
	resolution := configPathResolution{
		GlobalPath: globalPath,
	}

	if cfg != "" {
		resolution.Path = cfg
		resolution.Source = "explicit"
		return resolution, nil
	}

	// global override
	if global {
		resolution.Path = globalPath
		resolution.Source = "global_flag"
		return resolution, nil
	}

	// project discovery (cwd)
	if wd, err := os.Getwd(); err == nil {
		resolution.WorkingDirectory = wd
		for _, rel := range configCandidateRelPaths() {
			p := filepath.Join(wd, rel)
			candidate := configPathCandidate{
				RelativePath: rel,
				Path:         p,
			}
			if fi, err := os.Stat(p); err == nil {
				candidate.Exists = true
				candidate.IsFile = !fi.IsDir()
			}
			resolution.ProjectCandidates = append(resolution.ProjectCandidates, candidate)
			if candidate.Exists && candidate.IsFile && resolution.Path == "" {
				resolution.Path = p
				resolution.Source = "project"
			}
		}
		if resolution.Path != "" {
			return resolution, nil
		}
	}

	resolution.Path = globalPath
	resolution.Source = "global_fallback"
	return resolution, nil
}

func configCandidateRelPaths() []string {
	return []string{
		".oci-context.yml",
		".oci-context.json",
		filepath.Join(".oci-context", "config.yml"),
		filepath.Join(".oci-context", "config.json"),
		"oci-context.yml",
		"oci-context.json",
		filepath.Join("oci-context", "config.yml"),
		filepath.Join("oci-context", "config.json"),
	}
}

func globalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".oci-context", "config.yml"), nil
}
