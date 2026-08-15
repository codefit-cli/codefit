package namematch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// TestLeafPurity enumerates namematch's NON-TEST imports with the real go/ast
// parser and asserts they are standard library only.
//
// The check is worth a test rather than a code comment because the leaf's whole
// value is structural: three consumers in two layers depend on it, and a single
// import of a provider or of core/findings would let one of them reach the
// others through it. A doc comment saying "leaf" cannot fail.
//
// Test files are excluded on purpose, and that exclusion is the reason this
// enumerates rather than greps: the EXTERNAL test package namematch_test
// legitimately imports internal/core/ruleengine and rules to bind Go's
// vocabulary against TypeScript's. That import is not in the package's build
// graph and does not compromise the leaf; a grep over *.go could not tell the
// difference.
func TestLeafPurity(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parsing namematch: %v", err)
	}

	// Positive control: a parse that found no package, or a package with no
	// files, would report "stdlib only" about nothing at all.
	pkg, ok := pkgs["namematch"]
	if !ok {
		t.Fatalf("vacuum: package namematch not found; parsed %d package(s)", len(pkgs))
	}
	if len(pkg.Files) == 0 {
		t.Fatal("vacuum: package namematch has no non-test files to enumerate")
	}

	var seen []string
	for name, f := range pkg.Files {
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			seen = append(seen, path)
			// A stdlib import path has no dot in its first segment; every
			// codefit package starts with github.com/codefit-cli/codefit.
			if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
				t.Errorf("%s imports %q — namematch must be a leaf (stdlib only)", name, path)
			}
		}
	}
	t.Logf("namematch non-test imports across %d file(s): %v", len(pkg.Files), seen)

	// The enumeration must actually have looked at import declarations. If the
	// leaf ever legitimately drops to zero imports this needs a conscious edit,
	// which is preferable to a check that silently stops checking.
	if len(seen) == 0 {
		t.Fatal("vacuum: no imports enumerated at all — the parse found no import declarations")
	}
	if !contains(seen, "strings") {
		t.Errorf("expected the tokenizer's \"strings\" import among %v — the enumeration may be reading the wrong files", seen)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// TestASTNodeIsUsed keeps the go/ast import above honest for linters that
// cannot see it is used only through parser.ParseDir's result type.
var _ = ast.Package{}
