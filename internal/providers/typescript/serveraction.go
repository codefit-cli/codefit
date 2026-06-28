package typescript

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// serverActionsIn enumerates the Next.js Server Actions in a file by SHAPE, never
// by filename (ADR 0005). A Server Action is an async function under a "use
// server" directive, detected two ways:
//
//   - file-level: a `"use server"` directive opens the module — every EXPORTED
//     async function in it is a Server Action (the Next contract: only exports
//     are client-invocable).
//   - function-level: a `"use server"` directive opens a function body — that
//     function is a Server Action wherever it lives (inline in a Server
//     Component, a non-exported helper, lib/, anywhere).
//
// Server Actions are POST endpoints in disguise: they receive client input and
// carry the same IDOR/authz/over-fetch risk as route handlers, but a route.ts
// path gate would miss them entirely (they have no route file). Detection is the
// directive, not the path.
func serverActionsIn(root syntax.Node) []tsHandler {
	var out []tsHandler
	seen := map[int]bool{} // function body start byte, to dedupe the two passes

	add := func(fn syntax.Node, name string, line int, snippet string) {
		body := bodyBlock(fn)
		if body == nil {
			return
		}
		if seen[body.StartByte()] {
			return
		}
		seen[body.StartByte()] = true
		out = append(out, tsHandler{
			kind:    "action",
			method:  name,
			params:  fn.ChildByField("parameters"),
			body:    body,
			line:    line,
			snippet: snippet,
		})
	}

	// Pass 1 — file-level directive: every exported async function is an action.
	if fileHasUseServer(root) {
		walkTS(root, func(n syntax.Node) {
			if n.Type() != "export_statement" {
				return
			}
			line := n.StartLine()
			snippet := firstLine(string(n.Text()))
			for _, d := range exportedFns(n) {
				if isAsyncFn(d.node) {
					add(d.node, d.name, line, snippet)
				}
			}
		})
	}

	// Pass 2 — function-level directive: any async function whose body opens with
	// "use server" (covers inline actions in components and non-exported ones).
	walkTS(root, func(n syntax.Node) {
		if !isFunctionNode(n) || !isAsyncFn(n) || !fnHasUseServer(n) {
			return
		}
		add(n, fnName(n), n.StartLine(), firstLine(string(n.Text())))
	})

	return out
}

// fnDecl is a function node paired with its declared name (empty for an anonymous
// arrow/expression).
type fnDecl struct {
	node syntax.Node
	name string
}

// exportedFns returns the function declarations an export_statement introduces,
// in both forms: `export async function f()` and `export const f = async () =>`.
func exportedFns(exp syntax.Node) []fnDecl {
	var out []fnDecl
	for i := 0; i < exp.NamedChildCount(); i++ {
		child := exp.NamedChild(i)
		switch child.Type() {
		case "function_declaration", "generator_function_declaration":
			out = append(out, fnDecl{node: child, name: fnName(child)})
		case "lexical_declaration", "variable_declaration":
			for j := 0; j < child.NamedChildCount(); j++ {
				vd := child.NamedChild(j)
				if vd.Type() != "variable_declarator" {
					continue
				}
				name := field(vd, "name", 0)
				val := field(vd, "value", 1)
				if name == nil || val == nil {
					continue
				}
				if val.Type() == "arrow_function" || val.Type() == "function_expression" {
					out = append(out, fnDecl{node: val, name: string(name.Text())})
				}
			}
		}
	}
	return out
}

// collectActionInputs maps a Server Action's client input to the same id-input
// mold the IDOR machinery expects (signals + idVars). A route handler reads input
// from the request; a Server Action RECEIVES it as arguments (or a FormData):
//   - every parameter binding is a client-controlled value, seeded as an id-var
//     (name-agnostic — ADR 0005 — so a non-standard or nested id is not a blind
//     spot; an object arg's binding flows to its `.id` access);
//   - a FormData parameter's `formData.get("key")` reads name the form fields and
//     seed the bound result vars, mirroring destructured-param handling.
func collectActionInputs(h tsHandler) (signals []string, idVars map[string]bool) {
	idVars = map[string]bool{}
	var argNames []string
	fdBindings := map[string]bool{}

	if h.params != nil {
		for i := 0; i < h.params.NamedChildCount(); i++ {
			binding, isFormData := paramBinding(h.params.NamedChild(i))
			if binding == nil {
				continue
			}
			switch binding.Type() {
			case "identifier":
				name := string(binding.Text())
				idVars[name] = true
				if isFormData {
					fdBindings[name] = true
				} else {
					argNames = append(argNames, name)
				}
			case "object_pattern", "array_pattern":
				for _, nm := range patternNames(binding) {
					idVars[nm] = true
					argNames = append(argNames, nm)
				}
			}
		}
	}

	var formKeys []string
	if len(fdBindings) > 0 {
		walkTS(h.body, func(n syntax.Node) {
			switch n.Type() {
			case "variable_declarator":
				name := field(n, "name", 0)
				val := field(n, "value", 1)
				if name != nil && val != nil && name.Type() == "identifier" {
					if _, ok := formDataKey(unwrapAwait(val), fdBindings); ok {
						idVars[string(name.Text())] = true
					}
				}
			case "call_expression":
				if k, ok := formDataKey(n, fdBindings); ok {
					formKeys = append(formKeys, k)
				}
			}
		})
	}

	if len(argNames) > 0 {
		signals = append(signals, "receives client-controlled argument(s): "+strings.Join(dedupe(argNames), ", "))
	}
	if len(fdBindings) > 0 {
		if len(formKeys) > 0 {
			signals = append(signals, "reads form field(s): "+strings.Join(dedupe(formKeys), ", "))
		} else {
			signals = append(signals, "receives a client-controlled FormData argument")
		}
	}
	return signals, idVars
}

// paramBinding returns the binding pattern of a formal parameter (identifier or
// object/array pattern) and whether its declared type is FormData. The FormData
// check is by the type annotation (shape), not the parameter name.
func paramBinding(p syntax.Node) (syntax.Node, bool) {
	switch p.Type() {
	case "required_parameter", "optional_parameter":
		binding := p.ChildByField("pattern")
		if binding == nil && p.NamedChildCount() > 0 {
			binding = p.NamedChild(0)
		}
		isFormData := false
		for i := 0; i < p.NamedChildCount(); i++ {
			c := p.NamedChild(i)
			if c.Type() == "type_annotation" && strings.Contains(string(c.Text()), "FormData") {
				isFormData = true
			}
		}
		return binding, isFormData
	case "identifier":
		// arrow with a single unparenthesized parameter: `async id => {}`.
		return p, false
	}
	return nil, false
}

// formDataKey returns the key string of a `<formData>.get("key")` /
// `.getAll("key")` call, where <formData> is a known FormData-typed binding.
func formDataKey(call syntax.Node, fdBindings map[string]bool) (string, bool) {
	if call == nil || call.Type() != "call_expression" {
		return "", false
	}
	fn := field(call, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return "", false
	}
	obj := field(fn, "object", 0)
	prop := field(fn, "property", 1)
	if obj == nil || prop == nil || obj.Type() != "identifier" || !fdBindings[string(obj.Text())] {
		return "", false
	}
	if m := string(prop.Text()); m != "get" && m != "getAll" {
		return "", false
	}
	args := field(call, "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 || args.NamedChild(0).Type() != "string" {
		return "", false
	}
	return strings.Trim(string(args.NamedChild(0).Text()), `"'`+"`"), true
}

// --- directive + function helpers ---

// fileHasUseServer reports whether the module opens with a `"use server"`
// directive (the file-level Server Actions marker).
func fileHasUseServer(root syntax.Node) bool { return hasUseServerPrologue(root) }

// fnHasUseServer reports whether a function body opens with a `"use server"`
// directive (an inline / function-level Server Action).
func fnHasUseServer(fn syntax.Node) bool { return hasUseServerPrologue(bodyBlock(fn)) }

// hasUseServerPrologue reports whether the leading directive prologue of a block
// (a program or a statement_block) contains `"use server"`. The prologue is the
// run of string-literal statements at the top; leading comments are skipped, and
// the first non-directive statement ends it.
func hasUseServerPrologue(block syntax.Node) bool {
	if block == nil {
		return false
	}
	for i := 0; i < block.NamedChildCount(); i++ {
		stmt := block.NamedChild(i)
		if stmt.Type() == "comment" {
			continue
		}
		v, ok := directiveValue(stmt)
		if !ok {
			return false // prologue ended at the first non-directive statement
		}
		if v == "use server" {
			return true
		}
	}
	return false
}

// directiveValue returns the unquoted value of a directive statement (an
// expression_statement whose expression is a string literal), e.g. `"use
// server";` → "use server".
func directiveValue(stmt syntax.Node) (string, bool) {
	if stmt == nil || stmt.Type() != "expression_statement" || stmt.NamedChildCount() == 0 {
		return "", false
	}
	s := stmt.NamedChild(0)
	if s.Type() != "string" {
		return "", false
	}
	return strings.Trim(string(s.Text()), `"'`+"`"), true
}

// isFunctionNode reports whether n is any function form that can carry a body.
func isFunctionNode(n syntax.Node) bool {
	switch n.Type() {
	case "function_declaration", "generator_function_declaration", "arrow_function", "function_expression":
		return true
	}
	return false
}

// isAsyncFn reports whether a function node is async (Server Actions must be).
// The async keyword is an anonymous token in the grammar, so it is read from the
// node's leading source text.
func isAsyncFn(fn syntax.Node) bool {
	return strings.HasPrefix(strings.TrimSpace(string(fn.Text())), "async")
}

// fnName returns a function declaration's name, or "" for an anonymous
// arrow/function expression.
func fnName(fn syntax.Node) string {
	if n := fn.ChildByField("name"); n != nil {
		return string(n.Text())
	}
	return ""
}
