package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// pathsEqual normalizes symlinks (macOS /private/tmp) before comparison.
func pathsEqual(a, b string) bool {
	aAbs, _ := filepath.EvalSymlinks(a)
	bAbs, _ := filepath.EvalSymlinks(b)
	if aAbs == "" {
		aAbs = a
	}
	if bAbs == "" {
		bAbs = b
	}
	return aAbs == bAbs
}

// helper to run resolveConfigPath in a temp working directory with files created.
func withTempWd(t *testing.T, fn func(tmp string)) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	fn(tmp)
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	f.Close()
}

func TestResolveConfigPath_ProjectPriority(t *testing.T) {
	withTempWd(t, func(tmp string) {
		// create lower-priority file
		touch(t, filepath.Join(tmp, "oci-context.yml"))
		// higher-priority hidden top-level should win
		touch(t, filepath.Join(tmp, ".oci-context.yml"))

		got, err := resolveConfigPath("", false)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		want := filepath.Join(tmp, ".oci-context.yml")
		if !pathsEqual(got, want) {
			t.Fatalf("want %s, got %s", want, got)
		}
	})
}

func TestResolveConfigPath_DirectoryConfig(t *testing.T) {
	withTempWd(t, func(tmp string) {
		// prefer ./.oci-context/config.yml over ./oci-context.yml
		touch(t, filepath.Join(tmp, "oci-context.yml"))
		touch(t, filepath.Join(tmp, ".oci-context", "config.yml"))

		got, err := resolveConfigPath("", false)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		want := filepath.Join(tmp, ".oci-context", "config.yml")
		if !pathsEqual(got, want) {
			t.Fatalf("want %s, got %s", want, got)
		}
	})
}

func TestResolveConfigPath_GlobalFlag(t *testing.T) {
	got, err := resolveConfigPath("", true)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".oci-context", "config.yml")
	if !pathsEqual(got, want) {
		t.Fatalf("want global %s, got %s", want, got)
	}
}

func TestResolveConfigPath_Explicit(t *testing.T) {
	got, err := resolveConfigPath("/tmp/custom.yml", false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "/tmp/custom.yml" {
		t.Fatalf("expected explicit path, got %s", got)
	}
}
