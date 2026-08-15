package namematch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
	// parser.ParseFile per entry rather than parser.ParseDir: ParseDir is
	// deprecated (Go 1.25) and so is the ast.Package it returns (Go 1.22).
	// Walking the directory ourselves keeps the real parser — which is the
	// point of this test — without the deprecated surface, and it makes the
	// non-test filter explicit instead of a callback.
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading namematch dir: %v", err)
	}

	files := map[string]*ast.File{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Clean(name), nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parsing %s: %v", name, perr)
		}
		// Positive control on the parse itself: a file that landed in another
		// package would mean this enumeration is describing the wrong code.
		if f.Name.Name != "namematch" {
			t.Fatalf("%s declares package %q, not namematch", name, f.Name.Name)
		}
		files[name] = f
	}

	// Positive control: a walk that found no non-test file would report
	// "stdlib only" about nothing at all.
	if len(files) == 0 {
		t.Fatal("vacuum: package namematch has no non-test files to enumerate")
	}

	var seen []string
	for name, f := range files {
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
	t.Logf("namematch non-test imports across %d file(s): %v", len(files), seen)

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
