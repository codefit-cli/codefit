package scaffold_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/providers/registry"
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
	if info.DBParadigm != "auto" {
		t.Errorf("db paradigm = %q, want auto", info.DBParadigm)
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

// TestDetectUnregisteredStackSucceeds replaces TestDetectUnknownProjectErrors,
// which asserted only that SOME error came back. That content-blindness is
// exactly how the refusal message drifted: it named four manifests that cannot
// resolve a provider and omitted the one that can, and no test noticed for
// three releases.
//
// Detect no longer refuses. A root whose only manifest is one codefit registers
// no provider for is a real, common project — codefit simply cannot audit its
// CODE, which is a declaration to make, not a reason to withhold a config the
// DB dimension can still use.
//
// The fixture drives the real Detect over a real directory rather than building
// a ProjectInfo by hand, so it exercises the same os.Stat path production takes.
func TestDetectUnregisteredStackSucceeds(t *testing.T) {
	// A build manifest for a stack the registry does not carry. Named by
	// SHAPE — any manifest outside registry.InitDetectMarkerFiles() behaves
	// identically; the probe below proves this one really is outside it.
	const unregisteredManifest = "pom.xml"
	if slices.Contains(registry.InitDetectMarkerFiles(), unregisteredManifest) {
		t.Fatalf("%q is now a registered marker; this fixture no longer exercises the undetected path",
			unregisteredManifest)
	}

	root := t.TempDir()
	writeFile(t, root, unregisteredManifest, "<project/>\n")

	info, err := scaffold.Detect(root)
	if err != nil {
		t.Fatalf("Detect on an unregistered stack must not refuse, got: %v", err)
	}
	if info.Language != config.LanguageUndetected {
		t.Errorf("language = %q, want %q", info.Language, config.LanguageUndetected)
	}
	if info.Detected() {
		t.Errorf("Detected() = true for the sentinel language %q", info.Language)
	}
	// No provider supplied defaults, so codefit invents no globs. Empty is the
	// safe direction: nothing is classified "test", so RF-10's test-path
	// re-weighting never fires and every finding keeps its natural severity.
	pc := info.PathCriticality
	if len(pc.Production) != 0 || len(pc.Test) != 0 || len(pc.Example) != 0 {
		t.Errorf("path_criticality must be empty when no provider supplied defaults, got %+v", pc)
	}
	if info.Name != filepath.Base(root) {
		t.Errorf("name = %q, want the project dir name %q", info.Name, filepath.Base(root))
	}
}

// TestDetectEmptyDirSucceeds pins that "no files at all" is NOT a special case.
// Any definition of "empty" would be arbitrary (does a README count? a
// .gitignore?), so a directory with nothing in it takes exactly the same path
// as a directory full of files codefit does not recognize.
func TestDetectEmptyDirSucceeds(t *testing.T) {
	root := t.TempDir() // no marker files, no files at all

	info, err := scaffold.Detect(root)
	if err != nil {
		t.Fatalf("Detect on an empty dir must not refuse, got: %v", err)
	}
	if info.Language != config.LanguageUndetected {
		t.Errorf("language = %q, want %q", info.Language, config.LanguageUndetected)
	}
	if info.Detected() {
		t.Errorf("Detected() = true on an empty dir")
	}
}

// TestDetectedIsTrueForARealLanguage is the other arm of Detected(): the
// predicate must not be stuck false. It drives a real registered marker rather
// than asserting against a literal, so it tracks the registry.
func TestDetectedIsTrueForARealLanguage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module x\n")
	info, err := scaffold.Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Detected() {
		t.Errorf("Detected() = false for a detected language %q", info.Language)
	}
}

// TestDetectErrorsDoNotNameMarkerFiles guards the direction the deleted message
// failed in. Detect still errors on a genuine path failure, and that error must
// stay about the path — never a list of manifests to create, which is what made
// the old message unactionable.
func TestDetectErrorsDoNotNameMarkerFiles(t *testing.T) {
	// A NUL byte cannot appear in a path on any supported OS, so Abs/Stat
	// fails for a reason that has nothing to do with markers.
	_, err := scaffold.Detect("bad\x00path")
	if err == nil {
		t.Skip("this platform accepts a NUL byte in a path; no genuine I/O failure to assert on")
	}
	msg := err.Error()
	for _, marker := range append(registry.InitDetectMarkerFiles(),
		"pyproject.toml", "requirements.txt", "pom.xml", "build.gradle") {
		if strings.Contains(msg, marker) {
			t.Errorf("a genuine I/O error must not name marker file %q; got: %s", marker, msg)
		}
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
