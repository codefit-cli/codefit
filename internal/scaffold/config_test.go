package scaffold_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/providers/registry"
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
	if cfg.Database.Paradigm != "auto" {
		t.Errorf("paradigm = %q, want auto", cfg.Database.Paradigm)
	}
	if !slices.Contains(cfg.Database.SchemaPaths, "prisma/schema.prisma") {
		t.Errorf("schema paths = %v, want to contain prisma/schema.prisma", cfg.Database.SchemaPaths)
	}
	if !slices.Contains(cfg.Project.PathCriticality.Production, "app/**") {
		t.Errorf("production globs = %v, want app/**", cfg.Project.PathCriticality.Production)
	}
}

// A project dir name with YAML-hostile characters (quotes, backslashes) must
// still produce a config that loads, with the name preserved verbatim — not a
// silent broken file reported as success.
func TestRenderConfigEscapesHostileName(t *testing.T) {
	for _, name := range []string{`my"weird`, `back\slash`, `with: colon`, `1leading`} {
		info := scaffold.ProjectInfo{
			Name:            name,
			Language:        "go",
			PathCriticality: config.PathCriticality{Production: []string{"**/*.go"}},
		}
		cfg := loadRendered(t, info)
		if cfg.Project.Name != name {
			t.Errorf("name round-trip: got %q, want %q", cfg.Project.Name, name)
		}
	}
}

// undetectedInfo is what Detect really returns for a root holding no marker
// file that resolves a provider — built through the REAL Detect over a real
// directory rather than by hand, so the rendered config is the one a developer
// actually receives.
func undetectedInfo(t *testing.T) scaffold.ProjectInfo {
	t.Helper()
	info, err := scaffold.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("Detect on an empty root: %v", err)
	}
	if info.Detected() {
		t.Fatalf("fixture is not the undetected case: language = %q", info.Language)
	}
	return info
}

// TestRenderConfig_UndetectedOmitsPathCriticalityWhole is D6. With no provider
// defaults there are no globs to emit, and the key is omitted ENTIRELY rather
// than emitted empty.
//
// The difference is not cosmetic. `path_criticality:` with nothing under it
// parses as YAML null, which reads as "configured, and empty" — a developer
// scanning the file sees a classification that was made and came out blank. The
// key's absence, plus a comment block in its place, reads as what actually
// happened: codefit never classified anything, and here is how to.
func TestRenderConfig_UndetectedOmitsPathCriticalityWhole(t *testing.T) {
	info := undetectedInfo(t)
	data, err := scaffold.RenderConfig(info)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	out := string(data)

	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue // the comment block is where path_criticality SHOULD be discussed
		}
		if strings.HasPrefix(trimmed, "path_criticality:") || strings.HasPrefix(trimmed, "production:") {
			t.Errorf("undetected config must omit the path_criticality key whole; found live key %q\n---\n%s", trimmed, out)
		}
	}

	// The comment must state the RF-10 consequence AND how to set globs —
	// inform, do not decide.
	low := strings.ToLower(out)
	if !strings.Contains(low, "path_criticality") {
		t.Errorf("the comment block must still name path_criticality so the developer can find it\n---\n%s", out)
	}
	if !strings.Contains(low, "natural severity") {
		t.Errorf("the comment must state the RF-10 consequence: nothing is classified, so severity is not re-weighted\n---\n%s", out)
	}
	if !strings.Contains(out, "production:") {
		t.Errorf("the comment must SHOW how to set globs, not just mention that one can\n---\n%s", out)
	}
}

// TestRenderConfig_UndetectedRoundTrips: the file codefit writes must load
// through the very validator writeConfig runs on it.
func TestRenderConfig_UndetectedRoundTrips(t *testing.T) {
	cfg := loadRendered(t, undetectedInfo(t))
	if cfg.Project.Language != config.LanguageUndetected {
		t.Errorf("language = %q, want %q", cfg.Project.Language, config.LanguageUndetected)
	}
	if got := cfg.PathCriticalityFor("src/x_test.go"); got != "" {
		t.Errorf("PathCriticalityFor = %q, want \"\" — no path may be classified when codefit invented no globs", got)
	}
}

// TestRenderConfig_UndetectedNamesTheMarkersItLooksFor: the generated config is
// the artifact the developer keeps. It must say what codefit looked for, in
// names taken from the registry rather than typed by hand.
func TestRenderConfig_UndetectedNamesTheMarkersItLooksFor(t *testing.T) {
	data, err := scaffold.RenderConfig(undetectedInfo(t))
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	out := string(data)
	markers := registry.InitDetectMarkerFiles()
	if len(markers) == 0 {
		t.Fatal("no InitDetect markers registered; the loop below would pass vacuously")
	}
	for _, m := range markers {
		if !strings.Contains(out, m) {
			t.Errorf("the generated config must name the marker %q codefit looks for\n---\n%s", m, out)
		}
	}
	for _, cannotHelp := range []string{"pyproject.toml", "requirements.txt", "pom.xml", "build.gradle"} {
		if strings.Contains(out, cannotHelp) {
			t.Errorf("the generated config names %q, which resolves no provider — the original defect\n---\n%s", cannotHelp, out)
		}
	}
}

// TestRenderConfig_DeclaresTheSchemaGapWheneverNoORM is the PR-B declaration.
//
// The whole `database:` block — schema_paths included — is gated behind an ORM,
// and SchemaPaths is only ever populated from a Prisma schema. There is no
// detection of a SQL-migration directory. So a Flyway project receives a config
// with no database section: a config that audits NOTHING.
//
// The condition is ORM == "", not "undetected". A TypeScript project with no
// Prisma schema has the identical gap, and it would be dishonest to declare it
// only in the case that happens to be new.
func TestRenderConfig_DeclaresTheSchemaGapWheneverNoORM(t *testing.T) {
	cases := map[string]scaffold.ProjectInfo{
		"undetected":          undetectedInfo(t),
		"detected but no orm": {Name: "x", Language: "go"},
		"typescript without orm": {Name: "x", Language: "typescript",
			PathCriticality: config.PathCriticality{Production: []string{"src/**"}}},
	}
	for name, info := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := scaffold.RenderConfig(info)
			if err != nil {
				t.Fatalf("RenderConfig: %v", err)
			}
			out := string(data)
			if !strings.Contains(out, "schema_paths") {
				t.Errorf("a config with no ORM must tell the developer schema_paths is how the DB dimension turns on\n---\n%s", out)
			}
			low := strings.ToLower(out)
			if !strings.Contains(low, "migration") {
				t.Errorf("the declaration must say SQL migration directories are NOT detected — the gap that leaves this config auditing nothing\n---\n%s", out)
			}
		})
	}

	// The counter-case: a project WITH an ORM already has a real database
	// block, so the gap declaration must not appear and confuse it.
	withORM, err := scaffold.RenderConfig(scaffold.ProjectInfo{
		Name: "x", Language: "typescript", ORM: "prisma",
		SchemaPaths: []string{"prisma/schema.prisma"},
	})
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if strings.Contains(strings.ToLower(string(withORM)), "migration") {
		t.Errorf("a config with a detected ORM must not carry the no-ORM gap declaration\n---\n%s", withORM)
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
