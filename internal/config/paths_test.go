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

// TestTestSeverityMode pins the ONE place that resolves an absent
// sensors.security.test_severity into a mode (RF-10). Every caller asks the
// Config, so the default lives here and nowhere else — a sensor that repeated
// the "" → info rule locally is how two defaults drift apart.
//
// The nil-Config case is not hypothetical: AuditContext.Config is nil whenever
// a project has no .codefit.yaml at all, and applyCriticality is reached with
// it.
func TestTestSeverityMode(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{"nil config defaults to info", nil, config.TestSeverityInfo},
		{"unset defaults to info", withTestSeverity(""), config.TestSeverityInfo},
		{"explicit info", withTestSeverity("info"), config.TestSeverityInfo},
		{"explicit downgrade", withTestSeverity("downgrade"), config.TestSeverityDowngrade},
		{"explicit keep", withTestSeverity("keep"), config.TestSeverityKeep},
		// validate() rejects this at Load; a hand-built Config can still carry it.
		// The resolver never invents a fourth mode — it falls back to the default.
		{"unrecognised falls back to info", withTestSeverity("silence"), config.TestSeverityInfo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cfg.TestSeverityMode(); got != c.want {
				t.Errorf("TestSeverityMode() = %q, want %q", got, c.want)
			}
		})
	}
}

func withTestSeverity(mode string) *config.Config {
	cfg := criticalityFixture()
	cfg.Sensors.Security.TestSeverity = mode
	return cfg
}
