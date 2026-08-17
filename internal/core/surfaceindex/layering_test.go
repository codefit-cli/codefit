package surfaceindex_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Design D1: surfaceindex is a pure leaf. It imports only
// internal/core/findings (design's own boundary), and nothing outside
// internal/mcp imports IT — the MCP adapter is the single consumer, exactly
// like internal/core/coverage. The census pattern mirrors
// internal/schemasource/layering_test.go's TestSQLDDLHasExactlyOneProductionImporter:
// the invariant is asserted as a COUNT/SET over a real walk, not a location a
// future refactor could quietly break without a test noticing.
const surfaceindexImportPath = "github.com/codefit-cli/codefit/internal/core/surfaceindex"

// allowedSurfaceindexOwnImports are the only import paths surfaceindex's own
// .go files (production, non-test) may carry: the package it projects and
// nothing else — no MCP, no provider, no other core package.
var allowedSurfaceindexOwnImports = map[string]bool{
	"github.com/codefit-cli/codefit/internal/core/findings": true,
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("census root %q holds no go.mod, so it is not the repository root: %v", root, err)
	}
	return root
}

// TestSurfaceindexOwnImportsAreOnlyFindings locks the leaf's OWN import set:
// its production files (surfaceindex.go, doc.go) may import
// internal/core/findings and nothing else in this module.
func TestSurfaceindexOwnImportsAreOnlyFindings(t *testing.T) {
	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var parsedFiles int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		parsedFiles++
		for _, imp := range f.Imports {
			value, unquoteErr := strconv.Unquote(imp.Path.Value)
			if unquoteErr != nil {
				continue
			}
			if strings.HasPrefix(value, "github.com/codefit-cli/codefit/") && !allowedSurfaceindexOwnImports[value] {
				t.Errorf("%s imports %q — surfaceindex is a pure leaf over internal/core/findings only (design D1)",
					name, value)
			}
		}
	}
	if parsedFiles == 0 {
		t.Fatal("parsed 0 production files in internal/core/surfaceindex — the census is broken")
	}
}

// censusSurfaceindexImporters walks the whole repository and returns the
// distinct package directories (repo-relative, slash-separated) whose .go
// files import surfaceindex.
func censusSurfaceindexImporters(t *testing.T, includeTests bool) (pkgs []string, parsedFiles int) {
	t.Helper()
	root := repoRoot(t)

	seen := map[string]bool{}
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "dist", "bin":
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if filepath.Ext(name) != ".go" {
			return nil
		}
		if !includeTests && strings.HasSuffix(name, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v — the census proved nothing about this file", path, parseErr)
		}
		parsedFiles++
		for _, imp := range f.Imports {
			value, unquoteErr := strconv.Unquote(imp.Path.Value)
			if unquoteErr != nil {
				continue
			}
			if value != surfaceindexImportPath {
				continue
			}
			rel, relErr := filepath.Rel(root, filepath.Dir(path))
			if relErr != nil {
				rel = filepath.Dir(path)
			}
			seen[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %q: %v", root, walkErr)
	}
	for p := range seen {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	return pkgs, parsedFiles
}

// TestSurfaceindexHasNoImporterOutsideMCP locks the other half of D1: nothing
// outside internal/mcp imports surfaceindex. A positive control (counting
// _test.go files too) proves the census can actually detect an importer
// before trusting its production verdict — this test itself, in this same
// package, is that positive control's importer.
func TestSurfaceindexHasNoImporterOutsideMCP(t *testing.T) {
	production, parsedFiles := censusSurfaceindexImporters(t, false)
	if parsedFiles == 0 {
		t.Fatal("the census parsed 0 production .go files — the walk is broken")
	}

	withTests, _ := censusSurfaceindexImporters(t, true)
	if len(withTests) == 0 {
		t.Fatal("positive control failed: counting _test.go files too found 0 importers — the census cannot " +
			"detect an importer at all, so its production verdict below is a false all-clear")
	}
	t.Logf("positive control: %d importer package(s) with tests included: %v", len(withTests), withTests)

	for _, pkg := range production {
		if pkg != "internal/mcp" && !strings.HasPrefix(pkg, "internal/mcp/") {
			t.Errorf("production package %q imports surfaceindex — only internal/mcp may (design D1); "+
				"production importers: %v", pkg, production)
		}
	}
}
