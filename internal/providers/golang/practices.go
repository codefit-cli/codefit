package golang

import (
	"go/ast"
	"go/token"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// The practices rules are deterministic affirmations: every finding is emitted
// at Confidence 1.0 with no Probabilistic flag. That sets a hard bar — a rule's
// message may state only what its code established. A rule that cannot check
// what it claims is taught to check it or dropped; softening the wording to
// match a weaker check is not an option, because a finding nobody can act on is
// noise, and noise in an affirmation channel is worse than silence.
//
// No rule here decides what a test file is. The path belongs to config's
// PathCriticality, which the sensor applies on the way out, exactly as the
// security sensor does.

// practiceChecks runs every Go best-practice detector over a parsed file.
func practiceChecks(p *parsed) []findings.Finding {
	var out []findings.Finding
	add := func(f findings.Finding) { out = append(out, f) }

	// `package main` is a command, not library code — the only distinction the
	// AST can make between the two, and the one PRAC-005's message rests on.
	isLibrary := p.file.Name == nil || p.file.Name.Name != "main"
	idiomatic := idiomaticEmptyInterfaces(p.file)
	anyIsPredeclared := !fileRedeclaresAny(p.file)

	var ancestors []ast.Node
	ast.Inspect(p.file, func(n ast.Node) bool {
		if n == nil {
			ancestors = ancestors[:len(ancestors)-1]
			return true
		}
		switch node := n.(type) {
		case *ast.AssignStmt:
			checkDiscardedReturnValue(p, node, add)
		case *ast.InterfaceType:
			if isEmptyInterface(node) && !idiomatic[node] {
				add(emptyInterfaceFinding(p, node.Pos()))
			}
		case *ast.Ident:
			// `any` is a predeclared identifier, not a keyword, so it parses as
			// an Ident rather than an InterfaceType. It only names the empty
			// interface when the file has not redeclared it and it sits in a
			// type position.
			if anyIsPredeclared && node.Name == "any" && !idiomatic[node] &&
				isTypePosition(parentOf(ancestors), node) {
				add(emptyInterfaceFinding(p, node.Pos()))
			}
		case *ast.CallExpr:
			if isLibrary && isBuiltinPanic(node) {
				add(pracFinding(p, "PRAC-005", findings.SeverityMedium, node.Pos(),
					"panic in library code",
					"panic aborts the program; library code should return errors instead.",
					"Return a wrapped error and let the caller decide."))
			}
		}
		ancestors = append(ancestors, n)
		return true
	})

	checkDeferInLoop(p, add)
	return out
}

func pracFinding(p *parsed, id string, sev findings.Severity, pos token.Pos, title, desc, sugg string) findings.Finding {
	return findings.Finding{
		ID:          id,
		Dimension:   findings.DimensionPractices,
		Severity:    sev,
		File:        p.path,
		Line:        p.line(pos),
		Title:       title,
		Description: desc,
		Suggestion:  sugg,
		Confidence:  1.0,
	}
}

// checkDiscardedReturnValue flags `v, _ := f()` style assignments where a call's
// trailing result is dropped into the blank identifier.
//
// The provider has no go/types information, so the rule cannot know the
// discarded value is an error — it names only the syntactic fact it decided.
func checkDiscardedReturnValue(p *parsed, a *ast.AssignStmt, add func(findings.Finding)) {
	if len(a.Lhs) < 2 || len(a.Rhs) != 1 {
		return
	}
	if _, ok := a.Rhs[0].(*ast.CallExpr); !ok {
		return
	}
	last, ok := a.Lhs[len(a.Lhs)-1].(*ast.Ident)
	if !ok || last.Name != "_" {
		return
	}
	add(pracFinding(p, "PRAC-001", findings.SeverityLow, a.Pos(),
		"Discarded return value",
		"A call's last return value is discarded with the blank identifier.",
		"Confirm the discarded value is safe to drop — in Go the trailing return is commonly an error."))
}

// PRAC-003 -------------------------------------------------------------------

func emptyInterfaceFinding(p *parsed, pos token.Pos) findings.Finding {
	return pracFinding(p, "PRAC-003", findings.SeverityInfo, pos,
		"Empty interface used",
		"interface{}/any discards type information.",
		"Prefer a concrete type or a small, named interface.")
}

func isEmptyInterface(it *ast.InterfaceType) bool {
	return it.Methods == nil || len(it.Methods.List) == 0
}

// idiomaticEmptyInterfaces collects the type expressions where an empty
// interface is unavoidable, so PRAC-003 cannot claim the author discarded type
// information they could have kept:
//
//   - a generic type-parameter constraint — `func F[T any]`, `type S[T any]`;
//   - a variadic parameter's element type — `...any` / `...interface{}`.
func idiomaticEmptyInterfaces(f *ast.File) map[ast.Expr]bool {
	skip := make(map[ast.Expr]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncType:
			markConstraints(node.TypeParams, skip)
		case *ast.TypeSpec:
			markConstraints(node.TypeParams, skip)
		case *ast.Ellipsis:
			// A variadic parameter. `[...]T{}` also parses to an Ellipsis, but
			// as ArrayType.Len with a nil Elt.
			if node.Elt != nil {
				skip[node.Elt] = true
			}
		}
		return true
	})
	return skip
}

func markConstraints(list *ast.FieldList, skip map[ast.Expr]bool) {
	if list == nil {
		return
	}
	for _, f := range list.List {
		if f.Type != nil {
			skip[f.Type] = true
		}
	}
}

// isTypePosition reports whether ident is used as a type by its parent node.
// Without go/types this is the only way to tell the type `any` from an ordinary
// identifier that happens to be spelled the same.
func isTypePosition(parent ast.Node, ident *ast.Ident) bool {
	switch p := parent.(type) {
	case *ast.Field: // struct field, func parameter, func result, method
		return p.Type == ast.Expr(ident)
	case *ast.ValueSpec: // var / const declaration
		return p.Type == ast.Expr(ident)
	case *ast.TypeSpec: // type definition or alias
		return p.Type == ast.Expr(ident)
	case *ast.Ellipsis:
		return p.Elt == ast.Expr(ident)
	case *ast.ArrayType:
		return p.Elt == ast.Expr(ident)
	case *ast.MapType:
		return p.Key == ast.Expr(ident) || p.Value == ast.Expr(ident)
	case *ast.ChanType:
		return p.Value == ast.Expr(ident)
	case *ast.StarExpr:
		return p.X == ast.Expr(ident)
	case *ast.TypeAssertExpr:
		return p.Type == ast.Expr(ident)
	default:
		return false
	}
}

// fileRedeclaresAny reports whether the file declares its own top-level `any`,
// which would make the identifier name something other than the empty
// interface. Known limit: the check is file-scoped, so a redeclaration in
// another file of the same package is not seen; the `interface{}` spelling,
// which cannot be redeclared, is unaffected either way.
func fileRedeclaresAny(f *ast.File) bool {
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil && decl.Name != nil && decl.Name.Name == "any" {
				return true
			}
		case *ast.GenDecl:
			for _, s := range decl.Specs {
				if genSpecDeclaresAny(s) {
					return true
				}
			}
		}
	}
	return false
}

func genSpecDeclaresAny(s ast.Spec) bool {
	switch spec := s.(type) {
	case *ast.TypeSpec:
		return spec.Name != nil && spec.Name.Name == "any"
	case *ast.ValueSpec:
		for _, n := range spec.Names {
			if n.Name == "any" {
				return true
			}
		}
	}
	return false
}

func parentOf(ancestors []ast.Node) ast.Node {
	if len(ancestors) == 0 {
		return nil
	}
	return ancestors[len(ancestors)-1]
}

// PRAC-005 -------------------------------------------------------------------

func isBuiltinPanic(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "panic"
}

// PRAC-002 -------------------------------------------------------------------

// checkDeferInLoop flags defer statements governed by a loop (not by a nested
// function literal), which accumulate until the surrounding function returns.
func checkDeferInLoop(p *parsed, add func(findings.Finding)) {
	var stack []ast.Node
	ast.Inspect(p.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if _, ok := n.(*ast.DeferStmt); ok && enclosingLoopBeforeFunc(stack) {
			add(pracFinding(p, "PRAC-002", findings.SeverityLow, n.Pos(),
				"defer inside a loop",
				"A deferred call inside a loop runs only when the function returns, not each iteration.",
				"Close resources explicitly per iteration, or extract the loop body into a function."))
		}
		stack = append(stack, n)
		return true
	})
}

// enclosingLoopBeforeFunc reports whether the nearest enclosing block-forming
// ancestor is a loop rather than a function literal.
func enclosingLoopBeforeFunc(stack []ast.Node) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].(type) {
		case *ast.FuncLit:
			return false
		case *ast.ForStmt, *ast.RangeStmt:
			return true
		}
	}
	return false
}
