package syntax_test

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

// TS cross-provider debt lock (db-model-completeness-contract, design SS10,
// resolved decision #4: "a skipped test is a TODO, not a control"). Node.
// HasError() (syntax.go:34-36, implemented node.go:45) is declared but
// consulted by ZERO production paths today — codefit never actually uses
// tree-sitter's own error/MISSING-node detection to gate anything. This
// asserts TODAY's behavior with a real go/ast walk (the project already
// parses its own Go source this way, per CLAUDE.md), not a text grep: if
// this ever goes red because a production path starts calling HasError(),
// that is the signal to delete this lock and update the coverage manifest
// in the SAME change — never silently "fix" the test to keep it green.
//
// Citations re-verified against this working tree before writing (per
// architect direction 2026-07-30, not carried from exploration #1178 on
// trust): syntax.go:34 (doc comment), :36 (interface method), node.go:45
// (sole implementation). A repo-wide grep for the literal ".HasError()"
// across every non-test .go file under internal/ returns exactly that one
// line (node.go:45) — confirmed again here, mechanically, via go/ast.
func TestSyntax_HasError_ZeroProductionCallSites(t *testing.T) {
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	internalRoot := filepath.Join(repoRoot, "internal")

	var callSites []string
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(internalRoot, func(path string, d fs.DirEntry, err error) error {
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
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "HasError" {
				return true
			}
			pos := fset.Position(sel.Pos())
			rel, _ := filepath.Rel(repoRoot, pos.Filename)
			callSites = append(callSites, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line))
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", internalRoot, walkErr)
	}

	// EXACTLY one call site is expected: node.go's own HasError() method,
	// which calls the underlying vendor tree-sitter node's HasError() to
	// IMPLEMENT the interface — that is not "production consumption" of the
	// signal (nothing reasons about the result), it is the implementation
	// itself.
	const wantImplementationFile = "internal/providers/typescript/node.go"
	if len(callSites) != 1 {
		t.Fatalf("HasError() call sites = %v, want exactly 1 (node.go's own implementation). "+
			"If this changed because a NEW production path now consults HasError(), delete this "+
			"lock and update the coverage manifest in the same change", callSites)
	}
	if !strings.HasPrefix(callSites[0], wantImplementationFile+":") {
		t.Fatalf("the one HasError() call site is %q, want it in %q (node.go's own implementation)",
			callSites[0], wantImplementationFile)
	}
}
