package crossrules_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// F5 (4R ledger obs #1282, ADR 0018 corollary: a declared limit must be
// MACHINE-visible, not left to prose alone). DB-010/DB-013 are absence-based
// rules that run in scan-all and today do NOT consult
// db.Table.StructureProven() — a real, bounded limit (both rules only emit
// SURFACE, never a deterministic finding, so the blast radius is a possible
// surface item over unproven structure, never a false affirmation).
//
// Same go/ast walk discipline as internal/core/syntax/hasError_callsites_test.go
// (this project already parses its own Go source this way; that file was
// hasError_debt_test.go until its debt was paid, and it shows what this lock
// should become if StructureProven() ever gains a consumer: an inverted census,
// not a deletion). Asserts TODAY's
// behavior: if this ever goes red because crossrules starts consulting
// StructureProven(), that is the signal to delete this lock, update the
// coverage manifest, and correct the completeness-note exception clause in
// the SAME change — never silently "fix" the test to keep it green.
func TestCrossrules_StructureProven_NotConsultedToday(t *testing.T) {
	pkgDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	var callSites []string
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(pkgDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}
		ast.Inspect(src, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "StructureProven" {
				return true
			}
			pos := fset.Position(sel.Pos())
			callSites = append(callSites, fmt.Sprintf("%s:%d", filepath.Base(pos.Filename), pos.Line))
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", pkgDir, walkErr)
	}

	if len(callSites) != 0 {
		t.Fatalf("StructureProven references in internal/core/crossrules (non-test) = %v, want none. "+
			"If this changed because DB-010/DB-013 now gate on completeness, delete this lock, update the "+
			"coverage manifest, and correct sensors/db.completenessNote's crossrules exception clause in the "+
			"same change", callSites)
	}
}
