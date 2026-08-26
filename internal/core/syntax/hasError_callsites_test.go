package syntax_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSyntax_HasError_CallSiteCensus is the SUCCESSOR to a debt lock, not a new
// test. It used to be TestSyntax_HasError_ZeroProductionCallSites, in
// hasError_debt_test.go. What follows is why it changed and what it now holds.
//
// WHAT IT USED TO SAY. Node.HasError() was declared on the core's parser-agnostic
// interface (syntax.go), implemented by the TypeScript provider (node.go), and
// consulted by ZERO production paths. The old test asserted EXACTLY ONE call
// site — node.go's own implementation — which is what a declared-but-unconsumed
// signal looks like from the outside. It was a DEBT lock in the sense of
// db-model-completeness-contract design SS10, resolved decision #4 ("a skipped
// test is a TODO, not a control"): it existed to keep an unused signal VISIBLE
// rather than let it rot unnoticed. It carried its own exit condition in the
// failure message: if a production path ever started consulting HasError(),
// rewrite the lock in the SAME change — never silently make it green.
//
// WHY IT CHANGED. The debt is PAID, and it was not cosmetic debt. tree-sitter is
// error-RECOVERING: a rule pattern that does not parse comes back as a TREE
// containing ERROR nodes, with a NIL error. ruleengine.Compile read only that nil
// error, accepted the rule, and the rule then matched NOTHING for the life of the
// process — a silent rule, which is a silent vulnerability, precisely what
// ruleengine/loader.go's doc comment promises can never happen. HasError() was
// the signal that would have caught it, sitting unread the whole time. Compile
// now consults it and REJECTS the pattern with a located error.
//
// WHAT THIS NOW LOCKS. The census is inverted rather than retired: HasError() must
// have EXACTLY the two call sites named in wantCallSites below — the
// implementation and the compile gate. Delete the gate and this goes RED (the
// ruleengine entry disappears). Add a THIRD call site and it also goes red, which
// is deliberate: a signal this load-bearing does not get to spread through
// production unexamined, and the next person to consume it should have to come
// here and say why.
//
// WHAT THIS DOES NOT LOCK. A census sees CALLS, not semantics — `_ =
// root.HasError()` in compilePattern would satisfy it. The BEHAVIOUR (Compile
// returning a located error for an unparsable pattern, and still accepting every
// rule codefit ships) is locked separately, and proven by mutation, in
// internal/core/ruleengine/compilegate_test.go. Both are needed and neither
// replaces the other: that one keeps the gate honest, this one keeps the signal's
// reach visible.
//
// The old test's failure message also told a future reader to update the coverage
// manifest in the same change. Checked, mechanically, when the gate landed: no
// coverage manifest makes any claim about HasError — a search for "HasError"
// across internal/providers/typescript/coverage.go,
// internal/core/dbcoverage/dbcoverage.go and COVERAGE.md returns nothing, with a
// positive control confirming the search itself works. There was nothing there to
// correct.
//
// Measurement discipline, unchanged from the lock it replaces: a real go/ast walk
// over every non-test .go file under internal/ (the project parses its own Go
// source this way, per CLAUDE.md), never a text grep.
func TestSyntax_HasError_CallSiteCensus(t *testing.T) {
	// The complete set of production code that may consult HasError(), each with
	// the reason it is allowed to. A call site not in this map is a census
	// failure by construction — that is the point of a census.
	wantCallSites := map[string]string{
		"internal/providers/typescript/node.go": "the IMPLEMENTATION: tsNode.HasError() forwards to the " +
			"vendored tree-sitter node. Nothing reasons about the result here, so this is not consumption.",
		"internal/core/ruleengine/engine.go": "the COMPILE GATE: compilePattern rejects a rule whose parsed " +
			"pattern tree contains a syntax error, so a rule that would silently match nothing cannot enter the engine.",
	}

	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	internalRoot := filepath.Join(repoRoot, "internal")

	// callSites maps file -> the lines in it that call HasError(). Grouping by
	// file (rather than a flat list) is what lets the assertion below name a
	// MISSING allowed site and an UNEXPECTED new one as two different failures.
	callSites := map[string][]int{}
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
			rel, relErr := filepath.Rel(repoRoot, pos.Filename)
			if relErr != nil {
				rel = pos.Filename
			}
			key := filepath.ToSlash(rel)
			callSites[key] = append(callSites[key], pos.Line)
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", internalRoot, walkErr)
	}

	// The walk itself must be able to see a call site. Without this, a census
	// broken into finding nothing would report a clean, empty, WRONG result —
	// and "the gate is gone" and "the search stopped working" would be
	// indistinguishable.
	if len(callSites) == 0 {
		t.Fatal("the go/ast census found ZERO HasError() call sites anywhere under internal/. " +
			"That is not a passing state, it is a broken search: the interface method is implemented, " +
			"so at least the implementation must show up")
	}

	for file, why := range wantCallSites {
		if len(callSites[file]) == 0 {
			t.Errorf("REQUIRED HasError() call site missing from %s.\n"+
				"  It is required because: %s\n"+
				"  Do not delete this expectation to go green. Losing the ruleengine call site means "+
				"unparsable rule patterns are silently accepted again; losing the node.go one means the "+
				"signal itself stopped reporting. Restore the call, or justify its removal here and in "+
				"internal/core/ruleengine/compilegate_test.go together.", file, why)
		}
	}

	var unexpected []string
	for file, lines := range callSites {
		if _, ok := wantCallSites[file]; ok {
			continue
		}
		for _, ln := range lines {
			unexpected = append(unexpected, fmt.Sprintf("%s:%d", file, ln))
		}
	}
	sort.Strings(unexpected)
	if len(unexpected) > 0 {
		t.Errorf("NEW production call sites of HasError() that this census does not know about: %v.\n"+
			"  HasError() is a load-bearing correctness signal (it is what tells codefit a parse silently "+
			"failed), so a new consumer is a decision, not a detail. Add it to wantCallSites with the reason "+
			"it consults the signal and what it does when the answer is true — and lock that BEHAVIOUR in a "+
			"test of its own, the way internal/core/ruleengine/compilegate_test.go does for the compile gate.", unexpected)
	}
}
