package typescript

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// requestSourceLabels phrase each request source for the human-facing signal.
var requestSourceLabels = map[string]string{
	"params": "route param",
	"query":  "query parameter",
	"body":   "request body field",
}

// collectRequestInputs records the client-controlled identifier inputs of an
// Express/Fastify route handler and the local variables bound to them. The
// request object is the handler's FIRST formal parameter (its name is the
// developer's choice — `req`, `request`, ...), so collection keys off that name
// rather than a hardcoded `req`; this covers both frameworks with one mold. It
// reports direct member reads (req.params.slug), whole-source destructuring
// (const { slug } = req.params), and single-field bindings (const id =
// req.params.id), binding the latter two into idVars so an id passed onward is
// linked to its indirect access.
func collectRequestInputs(h tsHandler) (signals []string, idVars map[string]bool) {
	idVars = map[string]bool{}
	req := firstParamName(h.params)
	if req == "" {
		return nil, idVars
	}
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			signals = append(signals, s)
		}
	}
	walkTS(h.body, func(n syntax.Node) {
		switch n.Type() {
		case "member_expression":
			// req.<source>.<leaf>
			if src, leaf, ok := requestMemberRead(n, req); ok {
				add("reads " + requestSourceLabels[src] + " " + req + "." + src + "." + leaf)
			}
		case "variable_declarator":
			name := field(n, "name", 0)
			val := field(n, "value", 1)
			if name == nil || val == nil {
				return
			}
			switch {
			case name.Type() == "object_pattern":
				// const { slug, id } = req.params
				if src, ok := requestWholeSource(val, req); ok {
					names := patternNames(name)
					add("reads " + requestSourceLabels[src] + "(s) via destructured " + req + "." + src + ": " + strings.Join(names, ", "))
					for _, nm := range names {
						idVars[nm] = true
					}
				}
			case name.Type() == "identifier" && isRequestIDAccess(unwrapAwait(val)):
				// const id = req.params.id
				idVars[string(name.Text())] = true
			}
		}
	})
	return signals, idVars
}

// nestParamSources maps a NestJS parameter decorator to the client-input source
// it injects. Route params and query strings are client-controlled by definition;
// the body carries client data too — all are id-input candidates (matched by
// structure, not by the parameter's name, so a non-`id` route param is not a blind
// spot — ADR 0005).
var nestParamSources = map[string]string{
	"Param": "route param",
	"Query": "query parameter",
	"Body":  "request body",
}

// collectNestInputs records the client-controlled inputs of a NestJS handler from
// its PARAMETER DECORATORS (@Param('id') id, @Body() dto, @Query() q) — there is
// no request object as in Express; each decorator injects the value directly into a
// named parameter. It binds each such parameter to idVars so the shared downstream
// (collectIndirectUses, isIDBearing, containsIDArg) links an id passed onward to its
// access, exactly as for the other frameworks.
func collectNestInputs(h tsHandler) (signals []string, idVars map[string]bool) {
	idVars = map[string]bool{}
	if h.params == nil {
		return nil, idVars
	}
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			signals = append(signals, s)
		}
	}
	for i := 0; i < h.params.NamedChildCount(); i++ {
		p := h.params.NamedChild(i)
		if p.Type() != "required_parameter" && p.Type() != "optional_parameter" {
			continue
		}
		dec := childOfType(p, "decorator")
		if dec == nil {
			continue
		}
		src, ok := nestParamSources[decoratorCallee(dec)]
		if !ok {
			continue
		}
		name := childOfType(p, "identifier")
		if name == nil {
			continue
		}
		idVars[string(name.Text())] = true
		if key := decoratorStringArg(dec); key != "" {
			add("reads " + src + " " + decoratorCallee(dec) + "('" + key + "') as " + string(name.Text()))
		} else {
			add("reads " + src + " via " + decoratorCallee(dec) + "() as " + string(name.Text()))
		}
	}
	return signals, idVars
}

// firstParamName returns the name of a handler's first formal parameter — the
// request object of an Express/Fastify handler (`req`, `request`).
func firstParamName(params syntax.Node) string { return paramName(params, 0) }

// paramName returns the name of the i-th formal parameter, unwrapping a typed
// parameter (`res: Response` → "res"). A destructured parameter has no single
// name and yields "".
func paramName(params syntax.Node, i int) string {
	if params == nil || i < 0 || i >= params.NamedChildCount() {
		return ""
	}
	p := params.NamedChild(i)
	switch p.Type() {
	case "identifier":
		return string(p.Text())
	case "required_parameter", "optional_parameter":
		if pat := field(p, "pattern", 0); pat != nil && pat.Type() == "identifier" {
			return string(pat.Text())
		}
	}
	return ""
}

// requestMemberRead reports whether node is `<req>.<source>.<leaf>` for the given
// request name and a known source (params/query/body), returning the source and
// the leaf key.
func requestMemberRead(node syntax.Node, req string) (source, leaf string, ok bool) {
	obj := field(node, "object", 0)
	prop := field(node, "property", 1)
	if obj == nil || prop == nil || obj.Type() != "member_expression" {
		return "", "", false
	}
	base := field(obj, "object", 0)
	mid := field(obj, "property", 1)
	if base == nil || mid == nil || base.Type() != "identifier" || string(base.Text()) != req {
		return "", "", false
	}
	src := string(mid.Text())
	if !requestSources[src] {
		return "", "", false
	}
	return src, string(prop.Text()), true
}

// requestWholeSource reports whether val reads a whole request source object —
// `req.params`, `req.query`, or `req.body` (optionally awaited) — for the given
// request name, returning the source.
func requestWholeSource(val syntax.Node, req string) (string, bool) {
	val = unwrapAwait(val)
	if val.Type() != "member_expression" {
		return "", false
	}
	obj := field(val, "object", 0)
	prop := field(val, "property", 1)
	if obj == nil || prop == nil || obj.Type() != "identifier" || string(obj.Text()) != req {
		return "", false
	}
	src := string(prop.Text())
	if !requestSources[src] {
		return "", false
	}
	return src, true
}
