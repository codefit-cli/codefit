package scaffold_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/codefit-cli/codefit/internal/scaffold"
)

const sampleNext = "testdata/sample-next"

func TestDetectNextPrismaProject(t *testing.T) {
	info, err := scaffold.Detect(sampleNext)
	if err != nil {
		t.Fatalf("Detect(%q): %v", sampleNext, err)
	}

	if info.Language != "typescript" {
		t.Errorf("language = %q, want typescript", info.Language)
	}
	if info.Framework != "next" {
		t.Errorf("framework = %q, want next", info.Framework)
	}
	if info.ORM != "prisma" {
		t.Errorf("orm = %q, want prisma", info.ORM)
	}
	if info.DBType != "postgresql" {
		t.Errorf("db type = %q, want postgresql", info.DBType)
	}
	if info.DBParadigm != "oltp" {
		t.Errorf("db paradigm = %q, want oltp", info.DBParadigm)
	}
	if want := filepath.FromSlash("prisma/schema.prisma"); !slices.Contains(info.SchemaPaths, want) {
		t.Errorf("schema paths = %v, want to contain %q", info.SchemaPaths, want)
	}
	if info.Name != "sample-next" {
		t.Errorf("name = %q, want sample-next (the project dir)", info.Name)
	}
	if info.RouteHandlers < 3 {
		t.Errorf("route handlers = %d, want >= 3 (users, profile, feed)", info.RouteHandlers)
	}
}

// The generated path_criticality must classify the framework's route location.
// For Next.js the handlers live under app/, so production globs must reach them;
// the provider default alone (src/**) would miss them.
func TestDetectNextIncludesAppInProduction(t *testing.T) {
	info, err := scaffold.Detect(sampleNext)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	hasApp := false
	for _, g := range info.PathCriticality.Production {
		if g == "app/**" {
			hasApp = true
		}
	}
	if !hasApp {
		t.Errorf("production globs = %v, want to include app/** for Next.js", info.PathCriticality.Production)
	}
}

func TestDetectGoProject(t *testing.T) {
	// codefit itself is a Go project: detecting its own root must yield go.
	info, err := scaffold.Detect("../..")
	if err != nil {
		t.Fatalf("Detect(repo root): %v", err)
	}
	if info.Language != "go" {
		t.Errorf("language = %q, want go", info.Language)
	}
}

func TestDetectUnknownProjectErrors(t *testing.T) {
	dir := t.TempDir() // empty: no marker files
	if _, err := scaffold.Detect(dir); err == nil {
		t.Errorf("Detect on a dir with no markers must return an error, got nil")
	}
}

// go.mod wins over package.json in a polyglot root.
func TestDetectGoWinsOverPackageJSON(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module x\n")
	writeFile(t, root, "package.json", `{"dependencies":{"next":"14"}}`)
	info, err := scaffold.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Language != "go" {
		t.Errorf("language = %q, want go (go.mod precedence)", info.Language)
	}
}

// frameworkFromDeps + prismaProvider value-mapping branches feed the config
// enums, so each literal must be exercised.
func TestDetectFrameworkAndDBVariants(t *testing.T) {
	tsProject := func(t *testing.T, deps, prismaProvider string) scaffold.ProjectInfo {
		t.Helper()
		root := t.TempDir()
		writeFile(t, root, "package.json", `{"dependencies":{`+deps+`}}`)
		if prismaProvider != "" {
			mkdirs(t, root, "prisma")
			writeFile(t, root, filepath.FromSlash("prisma/schema.prisma"),
				"datasource db {\n  provider = \""+prismaProvider+"\"\n}\n")
		}
		info, err := scaffold.Detect(root)
		if err != nil {
			t.Fatalf("Detect: %v", err)
		}
		return info
	}

	t.Run("express", func(t *testing.T) {
		if got := tsProject(t, `"express":"4"`, "").Framework; got != "express" {
			t.Errorf("framework = %q, want express", got)
		}
	})
	t.Run("react", func(t *testing.T) {
		if got := tsProject(t, `"react":"18"`, "").Framework; got != "react" {
			t.Errorf("framework = %q, want react", got)
		}
	})
	t.Run("drizzle", func(t *testing.T) {
		if got := tsProject(t, `"drizzle-orm":"0.3"`, "").ORM; got != "drizzle" {
			t.Errorf("orm = %q, want drizzle", got)
		}
	})
	t.Run("typeorm", func(t *testing.T) {
		if got := tsProject(t, `"typeorm":"0.3"`, "").ORM; got != "typeorm" {
			t.Errorf("orm = %q, want typeorm", got)
		}
	})
	t.Run("mysql", func(t *testing.T) {
		if got := tsProject(t, `"@prisma/client":"5"`, "mysql").DBType; got != "mysql" {
			t.Errorf("db type = %q, want mysql", got)
		}
	})
	t.Run("sqlite", func(t *testing.T) {
		if got := tsProject(t, `"@prisma/client":"5"`, "sqlite").DBType; got != "sqlite" {
			t.Errorf("db type = %q, want sqlite", got)
		}
	})
	t.Run("unknown_provider_omitted", func(t *testing.T) {
		info := tsProject(t, `"@prisma/client":"5"`, "mongodb")
		if info.ORM != "prisma" {
			t.Errorf("orm = %q, want prisma (schema present)", info.ORM)
		}
		if info.DBType != "" {
			t.Errorf("db type = %q, want empty for an unsupported provider", info.DBType)
		}
	})
}
