package scaffold_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// loadRendered renders the config for info, writes it to a temp .codefit.yaml,
// and loads it back through the real config loader+validator.
func loadRendered(t *testing.T, info scaffold.ProjectInfo) *config.Config {
	t.Helper()
	data, err := scaffold.RenderConfig(info)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if !strings.Contains(string(data), "#") {
		t.Errorf("generated config should be commented for humans, got:\n%s", data)
	}
	path := filepath.Join(t.TempDir(), ".codefit.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("generated config does not round-trip through config.Load: %v\n--- yaml ---\n%s", err, data)
	}
	return cfg
}

func TestRenderConfigNextPrismaRoundTrips(t *testing.T) {
	info, err := scaffold.Detect(sampleNext)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	cfg := loadRendered(t, info)

	if cfg.Project.Name != "sample-next" {
		t.Errorf("name = %q, want sample-next", cfg.Project.Name)
	}
	if cfg.Project.Language != "typescript" {
		t.Errorf("language = %q, want typescript", cfg.Project.Language)
	}
	if cfg.Project.Framework != "next" {
		t.Errorf("framework = %q, want next", cfg.Project.Framework)
	}
	if cfg.Database.ORM != "prisma" {
		t.Errorf("orm = %q, want prisma", cfg.Database.ORM)
	}
	if cfg.Database.Type != "postgresql" {
		t.Errorf("db type = %q, want postgresql", cfg.Database.Type)
	}
	if cfg.Database.Paradigm != "oltp" {
		t.Errorf("paradigm = %q, want oltp", cfg.Database.Paradigm)
	}
	if !slices.Contains(cfg.Database.SchemaPaths, "prisma/schema.prisma") {
		t.Errorf("schema paths = %v, want to contain prisma/schema.prisma", cfg.Database.SchemaPaths)
	}
	if !slices.Contains(cfg.Project.PathCriticality.Production, "app/**") {
		t.Errorf("production globs = %v, want app/**", cfg.Project.PathCriticality.Production)
	}
}

// A Go project has no ORM/DB: the rendered config must omit the database section
// and still round-trip.
func TestRenderConfigGoRoundTrips(t *testing.T) {
	info := scaffold.ProjectInfo{
		Name:     "codefit",
		Language: "go",
		PathCriticality: config.PathCriticality{
			Production: []string{"**/*.go"},
			Test:       []string{"**/*_test.go"},
		},
	}
	cfg := loadRendered(t, info)
	if cfg.Database.ORM != "" {
		t.Errorf("go project must have no orm, got %q", cfg.Database.ORM)
	}
}
