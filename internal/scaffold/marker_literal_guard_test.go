package scaffold_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// Spec R4 forbids a hardcoded marker list in the code that writes `codefit
// init`'s artifacts. Until this guard, that prohibition was enforced only
// BEHAVIOURALLY: a hand-typed list goes red the moment the registry changes and
// the two stop agreeing.
//
// That is real protection, but it is protection with a delay, and the delay is
// the whole defect. A list typed today is byte-identical to the derived one
// today, so every existing test passes — measured, not assumed: replacing
// markerList() in UndetectedStatement with the literal "go.mod, package.json or
// tsconfig.json" left internal/scaffold, internal/cli, internal/providers/... and
// internal/mcp ALL green (go vet 0). The lie only becomes visible at the next
// registry change, which is exactly the three-release window the deleted refusal
// message lived in.
//
// So the prohibition is made STRUCTURAL, in the shape this repo already uses for
// this class of rule (internal/config's IncludeInfo tripwire, internal/mcp's
// skill/tool census): a go/ast census of every string literal in the production
// source of the packages that render init's user-facing text, failing on any
// literal that contains a registry marker name and has not been recorded with a
// reason. It does not forbid reading a marker file — it forbids doing so
// silently, and it cannot be satisfied by a literal that merely happens to agree
// with the registry today.
//
// markerLiteralAllowlist maps "file:owner" (owner = enclosing func or
// package-level var) to the reason that literal was accepted. An entry is a
// DECISION, and its reason must say why the literal is not user-facing text.
var markerLiteralAllowlist = map[string]string{
	"detect.go:readPackageDeps": "reads the DETECTED TypeScript project's own manifest to enrich framework/ORM. " +
		"It is a file access, not a claim to the developer about what codefit looks for: nothing in it is " +
		"rendered into the report, the config or the skill, and the file is opened only after the registry " +
		"already resolved the language.",
}

// markerLiteralPackages are the production packages that render `codefit init`'s
// user-facing text: internal/scaffold builds the statements, the config and the
// skill; internal/cli formats the report the developer reads on screen. A marker
// typed by hand in either is the same defect.
var markerLiteralPackages = []string{".", "../cli"}

// markerLiteral is one production string literal naming a registry marker.
type markerLiteral struct {
	file  string // base name, e.g. "detect.go"
	owner string // enclosing func or package-level var name
	line  int
	value string
	found string // the marker it contains
}

func (m markerLiteral) key() string { return m.file + ":" + m.owner }

// censusMarkerLiterals parses every production .go file of markerLiteralPackages
// and returns each string literal containing a registry marker name.
//
// Containment, not equality: a hand-typed marker list would arrive as a
// PREFIX/substring of a template or a format string, never as a literal equal to
// one file name, so equality would leave the exact defect this guards against
// undetected.
func censusMarkerLiterals(t *testing.T) (hits []markerLiteral, parsedFiles int) {
	t.Helper()

	markers := registry.InitDetectMarkerFiles()
	if len(markers) == 0 {
		t.Fatal("registry.InitDetectMarkerFiles() is empty; the census below would match nothing and " +
			"pass vacuously")
	}

	for _, pkgDir := range markerLiteralPackages {
		entries, err := os.ReadDir(pkgDir)
		if err != nil {
			t.Fatalf("reading %q: %v", pkgDir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(pkgDir, name)
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				// A file the census cannot parse is NOT a file without a
				// hardcoded marker. Failing loudly is the point.
				t.Fatalf("parsing %s: %v — the census proved nothing about this file", path, err)
			}
			parsedFiles++

			for _, decl := range f.Decls {
				owner := declOwner(decl)
				ast.Inspect(decl, func(n ast.Node) bool {
					lit, ok := n.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						value = lit.Value // concatenated/odd literal: inspect it raw
					}
					for _, m := range markers {
						if strings.Contains(value, m) {
							hits = append(hits, markerLiteral{
								file: name, owner: owner,
								line: fset.Position(lit.Pos()).Line, value: value, found: m,
							})
							break
						}
					}
					return true
				})
			}
		}
	}
	return hits, parsedFiles
}

// declOwner names the top-level declaration a literal lives under: a function,
// or a package-level var/const (which is where a template literal lives).
func declOwner(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Name != nil {
			return d.Name.Name
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.ValueSpec:
				if len(s.Names) > 0 {
					return s.Names[0].Name
				}
			case *ast.TypeSpec:
				if s.Name != nil {
					return s.Name.Name
				}
			}
		}
	}
	return "<file scope>"
}

// TestScaffoldNamesNoMarkerFileByHand is the structural half of R4.
func TestScaffoldNamesNoMarkerFileByHand(t *testing.T) {
	hits, parsedFiles := censusMarkerLiterals(t)

	// Positive probe, both halves. A census that walked nothing, or an inspector
	// that matched nothing, would report "no hardcoded markers" forever and be
	// indistinguishable from a clean tree — the same false all-clear the guard
	// exists to prevent, reproduced inside the guard.
	if parsedFiles == 0 {
		t.Fatalf("census parsed 0 production .go files under %v — the walk is broken, so a clean "+
			"result is meaningless", markerLiteralPackages)
	}
	const anchor = "detect.go:readPackageDeps"
	anchored := false
	for _, h := range hits {
		if h.key() == anchor {
			anchored = true
			t.Logf("positive probe: %d production files parsed; the known marker literal is anchored at "+
				"%s:%d (%q)", parsedFiles, h.file, h.line, h.value)
		}
	}
	if !anchored {
		t.Fatalf("census parsed %d files but never matched the KNOWN marker literal at %s — the "+
			"inspector matches nothing, so its verdict is a false all-clear, not evidence. If that "+
			"literal was legitimately removed, move this anchor to another known one rather than "+
			"deleting the probe", parsedFiles, anchor)
	}

	for _, h := range hits {
		if _, allowed := markerLiteralAllowlist[h.key()]; allowed {
			continue
		}
		t.Errorf("%s:%d (%s) contains the marker name %q in a string literal: %q\n"+
			"Marker names in init's artifacts must be DERIVED from "+
			"registry.InitDetectMarkerFiles() — through markerList() in scaffold — never typed. A "+
			"typed list is byte-identical to the derived one on the day it is written and starts "+
			"lying at the next registry change; that delay is the defect this change removed.\n"+
			"If this literal is not a claim to the developer about what codefit looks for, add %q to "+
			"markerLiteralAllowlist with a reason saying so.",
			h.file, h.line, h.owner, h.found, h.value, h.key())
	}
}

// TestScaffoldMarkerAllowlistHasNoGhosts guards the reverse direction: an
// allowlist entry whose literal no longer exists is stale permission, and would
// silently accept a FUTURE hardcoded list written in that same function.
func TestScaffoldMarkerAllowlistHasNoGhosts(t *testing.T) {
	hits, _ := censusMarkerLiterals(t)
	live := make(map[string]bool, len(hits))
	for _, h := range hits {
		live[h.key()] = true
	}
	var stale []string
	for key, reason := range markerLiteralAllowlist {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("allowlist entry %q carries no reason; an entry without a reason records no "+
				"decision", key)
		}
		if !live[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("allowlist entry %q no longer matches any marker literal — stale permission that "+
			"would silently accept a hardcoded list written there later. Remove it", key)
	}
}
