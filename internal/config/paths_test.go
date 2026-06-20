package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
)

func criticalityFixture() *config.Config {
	return &config.Config{
		Project: config.Project{
			Name:     "demo",
			Language: "go",
			PathCriticality: config.PathCriticality{
				Production: []string{"internal/**", "cmd/**"},
				Test:       []string{"**/*_test.go"},
				Example:    []string{"examples/**", "docs/**"},
			},
		},
	}
}

func TestPathCriticalityFor(t *testing.T) {
	c := criticalityFixture()
	cases := map[string]string{
		"internal/core/findings/findings.go": "production",
		"cmd/codefit/main.go":                "production",
		"internal/config/config_test.go":     "test", // test wins over production
		"examples/basic/main.go":             "example",
		"docs/PRD.md":                        "example",
		"README.md":                          "",
	}
	for file, want := range cases {
		if got := c.PathCriticalityFor(file); got != want {
			t.Errorf("PathCriticalityFor(%q) = %q, want %q", file, got, want)
		}
	}
}

func TestExpandGlobs(t *testing.T) {
	root := t.TempDir()
	must := func(rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must("db/001_init.sql")
	must("db/002_users.sql")
	must("db/readme.md")
	must("src/app.go")

	got, err := config.ExpandGlobs(root, []string{"db/*.sql"})
	if err != nil {
		t.Fatalf("ExpandGlobs errored: %v", err)
	}
	slices.Sort(got)
	want := []string{"db/001_init.sql", "db/002_users.sql"}
	if !slices.Equal(got, want) {
		t.Errorf("ExpandGlobs = %v, want %v", got, want)
	}
}
