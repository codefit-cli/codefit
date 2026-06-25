package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
)

// repoCodefitYAML is the project's own .codefit.yaml (self-dogfooding config),
// two levels up from internal/config.
const repoCodefitYAML = "../../.codefit.yaml"

func TestLoadParsesProjectConfig(t *testing.T) {
	cfg, err := config.Load(repoCodefitYAML)
	if err != nil {
		t.Fatalf("Load(%q) returned error: %v", repoCodefitYAML, err)
	}
	if cfg.Version != "1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1")
	}
	if cfg.Project.Name != "codefit" {
		t.Errorf("Project.Name = %q, want %q", cfg.Project.Name, "codefit")
	}
	if cfg.Project.Language != "go" {
		t.Errorf("Project.Language = %q, want %q", cfg.Project.Language, "go")
	}
	if !slices.Contains(cfg.Project.PathCriticality.Production, "internal/**") {
		t.Errorf("PathCriticality.Production = %v, want it to contain %q",
			cfg.Project.PathCriticality.Production, "internal/**")
	}
	if !slices.Contains(cfg.Project.PathCriticality.Test, "**/*_test.go") {
		t.Errorf("PathCriticality.Test = %v, want it to contain %q",
			cfg.Project.PathCriticality.Test, "**/*_test.go")
	}
}

func TestLoadMissingFileErrors(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load on a missing file returned nil error, want an error")
	}
}

// LoadOptional must distinguish the three states the scan path conflated:
// ABSENT (use defaults, no error), PRESENT-BUT-INVALID (error, stop), VALID (load).

func TestLoadOptionalAbsentReturnsNilNoError(t *testing.T) {
	cfg, err := config.LoadOptional(filepath.Join(t.TempDir(), ".codefit.yaml"))
	if err != nil {
		t.Fatalf("LoadOptional on an absent file must NOT error, got: %v", err)
	}
	if cfg != nil {
		t.Errorf("LoadOptional on an absent file must return nil cfg (use defaults), got %+v", cfg)
	}
}

func TestLoadOptionalInvalidErrorsLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codefit.yaml")
	// framework "nextjs" is not in the allow-list ("next" is): the exact bug that
	// silently disabled path_criticality on Bitácora.
	writeYAML(t, path, "version: \"1\"\nproject:\n  name: \"x\"\n  language: \"typescript\"\n  framework: \"nextjs\"\n")

	cfg, err := config.LoadOptional(path)
	if err == nil {
		t.Fatal("LoadOptional on a PRESENT but invalid config must error, got nil")
	}
	if cfg != nil {
		t.Errorf("invalid config must not return a usable cfg, got %+v", cfg)
	}
	msg := err.Error()
	for _, want := range []string{"framework", "nextjs", "next"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message must name the field + offending + allowed values; missing %q in: %s", want, msg)
		}
	}
}

func TestLoadOptionalValidLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codefit.yaml")
	writeYAML(t, path, "version: \"1\"\nproject:\n  name: \"x\"\n  language: \"typescript\"\n  framework: \"next\"\n")

	cfg, err := config.LoadOptional(path)
	if err != nil {
		t.Fatalf("LoadOptional on a valid config errored: %v", err)
	}
	if cfg == nil || cfg.Project.Framework != "next" {
		t.Errorf("valid config must load, got %+v", cfg)
	}
}

func writeYAML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
