package config_test

import (
	"path/filepath"
	"slices"
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
