package typescript

import (
	"regexp"
	"strconv"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// nplus1Query enumerates the N+1 surface (PRD DB-201) of a handler: every
// query call site — a local Prisma access OR an application call at the
// cross-function frontier, reusing isPrismaCall/isServiceCall verbatim — that
// is lexically nested inside an iteration construct. It is a pure AST fact
// (no schema needed), so it lives in the TS provider, never in
// internal/core/dbrules or internal/core/db (design's central layering call,
// locked by TestCoreDB_DBRules_NeverImportTypeScriptProvider). It implements
// surface.Query and reuses auditTargets (handler discovery) verbatim.
//
// Per ADR 0005, N+1 is ORDERED, never FILTERED: a loop over three literal
// elements is enumerated exactly like a loop over an unbounded query result —
// codefit states the structural fact, the agent judges whether it matters.
type nplus1Query struct{}

func (nplus1Query) Enumerate(root syntax.Node, file string) []findings.SurfaceItem {
	if root == nil {
		return nil
	}
	var out []findings.SurfaceItem
	for _, h := range auditTargets(root, file) {
		walkForNPlus1(h.body, nil, false, false, &out, file)
	}
	return out
}

// callbackIterationMethods are the per-element array/iterable methods whose
// sole meaningful argument is a callback invoked once per element — the
// natural JS/TS equivalent of a for-loop over an array (design §7).
var callbackIterationMethods = map[string]bool{
	"forEach": true, "map": true, "flatMap": true, "filter": true,
	"reduce": true, "some": true, "every": true, "find": true,
}

// loopFrame is the innermost enclosing iteration construct at the point a
// query call is found. depth counts enclosing loop-shaped constructs (a
// for/while/do statement or a callback iteration), so depth>1 means
// nested_loop. promiseAll is true when this frame's per-iteration calls are
// collected and awaited together via Promise.all(...) rather than
// individually — still N distinct queries, enumerated either way (ADR 0005).
type loopFrame struct {
	kind       string
	source     string
	depth      int
	promiseAll bool
}

// walkForNPlus1 recurses n, entering a new loopFrame at each genuine loop
// statement or per-element callback iteration, and emitting one item per query
// call site found while a frame is active. awaited is true only for a call
// directly under an await_expression (the sequential round-trip shape);
// promiseAllPending is true for the single call about to be entered as the
// sole argument of Promise.all(...), so the NEW frame it creates is stamped
// promiseAll — never an already-active enclosing frame.
func walkForNPlus1(n syntax.Node, frame *loopFrame, awaited, promiseAllPending bool, out *[]findings.SurfaceItem, file string) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "for_statement", "for_in_statement", "while_statement", "do_statement":
		nf := enterStatementLoop(n, frame)
		nf.promiseAll = promiseAllPending
		walkForNPlus1(loopBody(n), nf, false, false, out, file)
		return
	case "await_expression":
		if n.NamedChildCount() == 1 {
			walkForNPlus1(n.NamedChild(0), frame, true, promiseAllPending, out, file)
			return
		}
	case "call_expression":
		if arg, ok := promiseAllArg(n); ok {
			walkForNPlus1(arg, frame, false, true, out, file)
			return
		}
		if recv, method, body, ok := callbackIterationParts(n); ok {
			nf := enterCallbackLoop(recv, method, frame)
			nf.promiseAll = promiseAllPending
			walkForNPlus1(body, nf, false, false, out, file)
			return
		}
		if frame != nil {
			switch {
			case isPrismaCall(n):
				*out = append(*out, nplus1Item(n, *frame, true, awaited && !frame.promiseAll, file))
			case isServiceCall(n):
				*out = append(*out, nplus1Item(n, *frame, false, awaited && !frame.promiseAll, file))
			}
		}
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		walkForNPlus1(n.NamedChild(i), frame, false, false, out, file)
	}
}

// forOfPattern matches the "of" keyword in a for_in_statement's header text
// (the substring of the loop's own text up to its body), which distinguishes
// for...of (iterates values) from for...in (iterates keys) — the tree-sitter
// TypeScript grammar gives both the SAME node type, for_in_statement, with no
// field that names which keyword was used.
var forOfPattern = regexp.MustCompile(`\bof\b`)

// enterStatementLoop builds the loopFrame for a genuine loop statement
// (for/for...of/for...in/while/do...while), naming its kind and, for
// for_in_statement, the iterated source when statically determinable.
func enterStatementLoop(n syntax.Node, parent *loopFrame) *loopFrame {
	kind := "for"
	source := ""
	switch n.Type() {
	case "for_in_statement":
		kind = "for...in"
		if b := n.ChildByField("body"); b != nil {
			header := string(n.Text())[:b.StartByte()-n.StartByte()]
			if forOfPattern.MatchString(header) {
				kind = "for...of"
			}
		}
		if right := n.ChildByField("right"); right != nil {
			source = describeIteratedSource(right)
		}
	case "while_statement":
		kind = "while"
	case "do_statement":
		kind = "do...while"
	}
	depth := 1
	if parent != nil {
		depth = parent.depth + 1
	}
	return &loopFrame{kind: kind, source: source, depth: depth}
}

// enterCallbackLoop builds the loopFrame for a per-element callback iteration
// (.forEach/.map/...), naming its kind (".<method>(...)") and the iterated
// source (the receiver expression) when statically determinable.
func enterCallbackLoop(recv syntax.Node, method string, parent *loopFrame) *loopFrame {
	depth := 1
	if parent != nil {
		depth = parent.depth + 1
	}
	return &loopFrame{kind: "." + method + "(...)", source: describeIteratedSource(recv), depth: depth}
}

// loopBody returns the body field shared by for/for_in/while/do statement
// nodes.
func loopBody(n syntax.Node) syntax.Node {
	return n.ChildByField("body")
}

// describeIteratedSource states, as a fact, what is known about an iterated
// expression WITHOUT resolving it further (ADR 0005 — the agent judges; this
// only names the shape). A literal array names its own element count (so a
// loop over 3 config items is dismissed at a glance); anything else that
// cannot be determined statically is named honestly as such, never guessed.
func describeIteratedSource(n syntax.Node) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "array":
		return "a literal array of " + strconv.Itoa(n.NamedChildCount()) + " element(s)"
	case "identifier":
		return "the variable '" + string(n.Text()) + "'"
	case "call_expression":
		if isPrismaCall(n) {
			model, method := prismaCallInfo(n)
			return "the result of prisma." + model + "." + method
		}
		if c := calleeName(n); c != "" {
			return "the result of " + c + "(...)"
		}
	case "member_expression":
		if c := calleeName(n); c != "" {
			return "the property '" + c + "'"
		}
	}
	return "an expression whose shape is not statically determined"
}

// promiseAllArg reports whether call is Promise.all(<arg>) and returns <arg>
// (the single wrapped expression, typically an array-producing .map(...)).
func promiseAllArg(call syntax.Node) (syntax.Node, bool) {
	fn := field(call, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return nil, false
	}
	obj := field(fn, "object", 0)
	prop := field(fn, "property", 1)
	if obj == nil || prop == nil || obj.Type() != "identifier" ||
		string(obj.Text()) != "Promise" || string(prop.Text()) != "all" {
		return nil, false
	}
	args := field(call, "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 {
		return nil, false
	}
	return args.NamedChild(0), true
}

// callbackIterationParts reports whether call is <receiver>.<method>(<callback>)
// where method is one of callbackIterationMethods and the sole argument is a
// function/arrow — returning the receiver (the iterated source), the method
// name, and the callback's body (a statement_block OR a direct expression,
// both valid under the "body" field for both arrow_function and
// function_expression).
func callbackIterationParts(call syntax.Node) (receiver syntax.Node, method string, body syntax.Node, ok bool) {
	fn := field(call, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return nil, "", nil, false
	}
	obj := field(fn, "object", 0)
	prop := field(fn, "property", 1)
	if obj == nil || prop == nil {
		return nil, "", nil, false
	}
	m := string(prop.Text())
	if !callbackIterationMethods[m] {
		return nil, "", nil, false
	}
	args := field(call, "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 {
		return nil, "", nil, false
	}
	cb := args.NamedChild(0)
	if cb.Type() != "arrow_function" && cb.Type() != "function_expression" {
		return nil, "", nil, false
	}
	cbBody := cb.ChildByField("body")
	if cbBody == nil {
		return nil, "", nil, false
	}
	return obj, m, cbBody, true
}

// nplus1Item builds the surface item for one query call site found inside an
// active loopFrame. local distinguishes a local Prisma access from the
// cross-function frontier (a service/repository call, never followed —
// ADR 0005 option C, same semantics idor/authz/overfetch already declare).
func nplus1Item(call syntax.Node, frame loopFrame, local, awaited bool, file string) findings.SurfaceItem {
	var signals []string
	if local {
		model, method := prismaCallInfo(call)
		signals = append(signals, "Runs a Prisma call (prisma."+model+"."+method+
			") inside a "+frame.kind+" loop"+sourceClause(frame))
	} else {
		signals = append(signals, "Runs "+calleeName(call)+
			" inside a "+frame.kind+" loop"+sourceClause(frame)+
			" — this is not a local Prisma call, so codefit does not follow it into the function that "+
			"produced it; whether it performs a query per iteration is NOT verified here. Follow the call "+
			"to its source to determine whether it runs a per-iteration database round-trip.")
	}
	switch {
	case frame.promiseAll:
		signals = append(signals, "The per-iteration calls are wrapped in Promise.all, so they run "+
			"concurrently rather than sequentially — still one query per element")
	case awaited:
		signals = append(signals, "The call is awaited directly inside the loop body — sequential round-trips")
	}
	if frame.depth > 1 {
		signals = append(signals, "This query sits under more than one enclosing loop (nested iteration)")
	}

	return findings.SurfaceItem{
		Category:          "nplus1",
		File:              file,
		Line:              call.StartLine(),
		Snippet:           firstLine(string(call.Text())),
		StructuralSignals: signals,
		StructuralFacts: map[string]bool{
			"local_access_detected": local,
			"awaited_in_loop":       awaited,
			"promise_all_wrapped":   frame.promiseAll,
			"nested_loop":           frame.depth > 1,
		},
		ReasonToReview: "Does this loop run once per element in a collection that can grow — and if so, " +
			"should this query be batched (e.g. findMany/IN) or fetched once outside the loop instead of " +
			"issuing one round-trip per iteration?",
	}
}

// sourceClause phrases the iterated-source fact as a trailing clause, or the
// empty string when the source could not be statically determined enough to
// name (describeIteratedSource never returns empty for a resolvable node, but
// an unresolvable field lookup can still yield "").
func sourceClause(frame loopFrame) string {
	if frame.source == "" {
		return ""
	}
	return " (iterates over " + frame.source + ")"
}
