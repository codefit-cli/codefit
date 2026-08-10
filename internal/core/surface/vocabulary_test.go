package surface

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// providerCategoryConstsInSourceFile parses filename (a Go source file in this
// package's directory) with go/ast — the stdlib parser CLAUDE.md mandates for
// Go — and returns the string value of every const explicitly typed `Category`
// declared in it, partitioned into provider-emitted (no db-/dw- prefix) and
// schema-derived (db-/dw- prefix). This is D1b's control: it reads the SAME
// source file surface.go declares its consts in, not a second hand-written
// list, so a const added and forgotten from ProviderCategories fails here, and
// a ProviderCategories entry with no backing const fails here too.
func providerCategoryConstsInSourceFile(t *testing.T, filename string) (providerEmitted, schemaDerived []string) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed — cannot locate this package's directory")
	}
	path := filepath.Join(filepath.Dir(thisFile), filename)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("go/ast parsing %s: %v", path, err)
	}

	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != "Category" {
				continue // not a Category-typed const (this package declares no other iota block)
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquoting const literal %s: %v", lit.Value, err)
				}
				if strings.HasPrefix(val, "db-") || strings.HasPrefix(val, "dw-") {
					schemaDerived = append(schemaDerived, val)
				} else {
					providerEmitted = append(providerEmitted, val)
				}
			}
		}
	}
	return providerEmitted, schemaDerived
}

// TestProviderCategories_MatchesConstBlockBothDirections is D1b: the sharpest
// correctness question in the whole change. ProviderCategories must be exactly
// the provider-emitted (non db-/dw-) Category consts declared in surface.go —
// checked by parsing that file, not by trusting the slice. A const added to
// surface.go and forgotten here fails; a ProviderCategories entry with no
// backing const also fails. This is NOT dbcoverage_test.go's rejected Control
// C (a second hand-maintained list asserted against a manifest) — the list here
// is asserted against the const block ITSELF, read from source, so there is no
// third place left to drift to.
func TestProviderCategories_MatchesConstBlockBothDirections(t *testing.T) {
	providerEmitted, schemaDerived := providerCategoryConstsInSourceFile(t, "surface.go")

	if len(providerEmitted) == 0 {
		t.Fatal("no provider-emitted Category const found in surface.go — the probe itself is broken")
	}
	if len(schemaDerived) == 0 {
		t.Fatal("no db-/dw- prefixed Category const found in surface.go — the probe itself is broken")
	}

	wantSorted := append([]string{}, providerEmitted...)
	sort.Strings(wantSorted)

	got := make([]string, 0, len(ProviderCategories))
	for _, c := range ProviderCategories {
		got = append(got, string(c))
	}
	gotSorted := append([]string{}, got...)
	sort.Strings(gotSorted)

	if !slices.Equal(wantSorted, gotSorted) {
		t.Errorf("surface.ProviderCategories drifted from the const block in surface.go:\n"+
			"  const block (provider-emitted, no db-/dw- prefix): %v\n"+
			"  ProviderCategories:                                %v", wantSorted, gotSorted)
	}
}

// TestDefaultSeverityExhaustiveOverProviderCategories locks framework.go's
// defaultSeverity map exhaustive over ProviderCategories, both directions — a
// category with no severity default falls back silently to medium today
// (severityFor), which is exactly the kind of unchecked hand-maintained mirror
// D1b exists to police (dbcoverage_test.go's Control C precedent).
func TestDefaultSeverityExhaustiveOverProviderCategories(t *testing.T) {
	for _, c := range ProviderCategories {
		if _, ok := defaultSeverity[string(c)]; !ok {
			t.Errorf("defaultSeverity has no entry for %q (declared in ProviderCategories) — severityFor would silently fall back to medium", c)
		}
	}
	declared := make(map[string]bool, len(ProviderCategories))
	for _, c := range ProviderCategories {
		declared[string(c)] = true
	}
	for cat := range defaultSeverity {
		if !declared[cat] {
			t.Errorf("defaultSeverity has a stale entry %q not in ProviderCategories", cat)
		}
	}
}

// TestTitlesExhaustiveOverProviderCategories mirrors the severity lock for
// framework.go's titles map — an undeclared category falls back silently to a
// generic "Surface item confirmed by agent reasoning: X" title (titleFor).
func TestTitlesExhaustiveOverProviderCategories(t *testing.T) {
	for _, c := range ProviderCategories {
		if _, ok := titles[string(c)]; !ok {
			t.Errorf("titles has no entry for %q (declared in ProviderCategories) — titleFor would silently fall back to a generic title", c)
		}
	}
	declared := make(map[string]bool, len(ProviderCategories))
	for _, c := range ProviderCategories {
		declared[string(c)] = true
	}
	for cat := range titles {
		if !declared[cat] {
			t.Errorf("titles has a stale entry %q not in ProviderCategories", cat)
		}
	}
}
