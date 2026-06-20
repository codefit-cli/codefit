package golang

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// practiceChecks runs every Go best-practice detector over a parsed file.
func practiceChecks(p *parsed) []findings.Finding {
	var out []findings.Finding
	add := func(f findings.Finding) { out = append(out, f) }
	isTest := strings.HasSuffix(p.path, "_test.go")

	ast.Inspect(p.file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			checkIgnoredError(p, node, add)
		case *ast.GoStmt:
			add(pracFinding(p, "PRAC-004", findings.SeverityInfo, node.Pos(),
				"Unsynchronized goroutine",
				"A goroutine is started without a visible WaitGroup or channel to synchronize it.",
				"Track goroutine lifetime with sync.WaitGroup, errgroup, or a channel."))
		case *ast.InterfaceType:
			if node.Methods == nil || len(node.Methods.List) == 0 {
				add(pracFinding(p, "PRAC-003", findings.SeverityInfo, node.Pos(),
					"Empty interface used",
					"interface{}/any discards type information.",
					"Prefer a concrete type or a small, named interface."))
			}
		case *ast.CallExpr:
			if !isTest && isBuiltinPanic(node) {
				add(pracFinding(p, "PRAC-005", findings.SeverityMedium, node.Pos(),
					"panic in production code",
					"panic aborts the program; library code should return errors instead.",
					"Return a wrapped error and let the caller decide."))
			}
		}
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

// checkIgnoredError flags `v, _ := f()` style assignments where a call's result
// is discarded into the blank identifier (commonly an error).
func checkIgnoredError(p *parsed, a *ast.AssignStmt, add func(findings.Finding)) {
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
		"Possibly ignored error",
		"A call's last return value is discarded with the blank identifier.",
		"Check the returned error and handle or wrap it."))
}

func isBuiltinPanic(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "panic"
}

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
