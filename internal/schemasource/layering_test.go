package schemasource

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

// Spec R11: exactly ONE production package maps schema input shape to a concrete
// parser. The mapping site MAY move — this change moves it out of internal/mcp —
// but it MUST NOT multiply.
//
// The invariant is asserted as a COUNT, not as a location, and that is the whole
// point: a test naming a file would have to be edited by the very change that
// duplicates the mapping, and would therefore never catch it. A count cannot be
// satisfied by moving the problem.
//
// The proxy for "maps input to a concrete parser" is an import of
// internal/providers/sqlddl from production (non-_test.go) source. It is a proxy
// and it is honest about being one: sqlddl is the parser a second mapping site
// would have to name, and R11's realistic mutation is exactly
// `import ".../providers/sqlddl"` in internal/scaffold — the shortest path to a
// working feature.
const sqlddlImportPath = "github.com/codefit-cli/codefit/internal/providers/sqlddl"

// sqlddlSelfPackage is the provider's own directory. Its files are `package
// sqlddl` and cannot import themselves, so it never appears in the census; it is
// named here only so a reader knows it was considered, not overlooked.
const sqlddlSelfPackage = "internal/providers/sqlddl"

// censusSQLDDLImporters walks the whole repository and returns the distinct
// package directories (repo-relative, slash-separated) whose .go files import
// sqlddl. When includeTests is false only production files are parsed.
//
// It parses with ImportsOnly because that is all the question needs, and it
// FAILS the test on an unparseable file: a file the census cannot read is not a
// file without the import, and a census that silently skips is a false all-clear
// of exactly the shape it exists to prevent.
func censusSQLDDLImporters(t *testing.T, includeTests bool) (pkgs []string, parsedFiles int) {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	// The walk is only meaningful over the real tree; prove it is the real tree
	// before trusting anything it reports.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("census root %q holds no go.mod, so it is not the repository root: %v", root, err)
	}

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
			if value != sqlddlImportPath {
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

// TestSQLDDLHasExactlyOneProductionImporter is R11's lock.
func TestSQLDDLHasExactlyOneProductionImporter(t *testing.T) {
	production, parsedFiles := censusSQLDDLImporters(t, false)
	if parsedFiles == 0 {
		t.Fatal("the census parsed 0 production .go files — the walk is broken, so a count of 1 " +
			"would be an accident rather than evidence")
	}

	// POSITIVE CONTROL. A census that cannot count would report "1" forever and
	// be indistinguishable from a healthy tree. Including tests must find MORE
	// than one importer; if it does not, the probe is dead and the production
	// number below means nothing.
	withTests, _ := censusSQLDDLImporters(t, true)
	if len(withTests) <= 1 {
		t.Fatalf("positive control failed: counting _test.go files too found %d importer(s) %v, want >1. "+
			"The census cannot detect an importer, so its production verdict is a false all-clear, "+
			"not evidence", len(withTests), withTests)
	}
	t.Logf("positive control: %d importer package(s) with tests included: %v", len(withTests), withTests)

	if len(production) != 1 {
		t.Fatalf("%d production package(s) import %s: %v\n"+
			"Spec R11: the input→parser mapping site may MOVE, it must not MULTIPLY. Two sites are how "+
			"`codefit init` starts proving a schema under a different parser than the one the audit "+
			"reads it with. Route the second caller through internal/schemasource instead of importing "+
			"the provider directly.\n"+
			"(The provider's own package %s never appears here — its files are `package sqlddl`.)",
			len(production), sqlddlImportPath, production, sqlddlSelfPackage)
	}
	t.Logf("the single production importer of %s is %q (%d production files parsed)",
		sqlddlImportPath, production[0], parsedFiles)
}
