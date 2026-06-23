package typescript

import (
	"regexp"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// idorQuery enumerates the IDOR surface of a Next.js App Router file: every
// route handler that BOTH receives a client-controlled identifier AND accesses a
// resource (Prisma), regardless of whether the two are connected. It does NOT
// follow the data from the id to the where clause — that is the agent's job. It
// enumerates by structural PRESENCE, accepting over-enumeration; completeness
// beats noise (PRD §10, ADR 0004). It implements surface.Query.
type idorQuery struct{}

// httpMethods are the Next App Router route handler exports.
var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

// prismaMethods is the Prisma Client model-query method set (verified against
// the Prisma Client API reference). Detection is by SHAPE — <x>.<model>.<method>
// where method is one of these — not by the client being named "prisma", so an
// aliased client (db, this.prisma, an imported db) is not an invisible blind
// spot. We accept over-enumeration (a non-Prisma <x>.<y>.update slips in); the
// agent discards it. A missed access would be an invisible vulnerability — the
// asymmetry favors enumerating more.
var prismaMethods = map[string]bool{
	"findUnique": true, "findUniqueOrThrow": true, "findFirst": true, "findFirstOrThrow": true,
	"findMany": true, "create": true, "createMany": true, "createManyAndReturn": true,
	"update": true, "updateMany": true, "updateManyAndReturn": true, "upsert": true,
	"delete": true, "deleteMany": true, "count": true, "aggregate": true,
	"groupBy": true, "findRaw": true, "aggregateRaw": true,
}

// authzHelpers is the KNOWN, DECLARED set of authentication/authorization calls
// the signal looks for. The signal states what it searched and whether it was
// found — never whether authorization is correct.
var authzHelpers = []string{
	"getServerSession", "auth", "getToken",
	"requireAuth", "checkPermission", "authorize", "can", "verify", "ensureOwner",
}

var authzHelperSet = func() map[string]bool {
	m := make(map[string]bool, len(authzHelpers))
	for _, h := range authzHelpers {
		m[h] = true
	}
	return m
}()

var idSuffix = regexp.MustCompile(`Id$`)

func (idorQuery) Enumerate(root syntax.Node, file string) []findings.SurfaceItem {
	if root == nil || !isNextRouteFile(file) {
		return nil
	}
	var out []findings.SurfaceItem
	walkTS(root, func(n syntax.Node) {
		if n.Type() != "export_statement" {
			return
		}
		for _, h := range handlersIn(n) {
			if item, ok := idorItem(h, file); ok {
				out = append(out, item)
			}
		}
	})
	return out
}

// tsHandler is an exported route handler: its HTTP method, its body subtree (the
// only scope the signals are searched in — searching wider would let authz in a
// called helper masquerade as authz in the handler), and its location.
type tsHandler struct {
	body    syntax.Node
	line    int
	snippet string
}

// handlersIn extracts the route handlers declared by an export_statement, in
// both forms: `export async function GET(...)` and `export const GET = ...`.
func handlersIn(exp syntax.Node) []tsHandler {
	var out []tsHandler
	line := exp.StartLine()
	snippet := firstLine(string(exp.Text()))
	for i := 0; i < exp.NamedChildCount(); i++ {
		child := exp.NamedChild(i)
		switch child.Type() {
		case "function_declaration", "generator_function_declaration":
			name := field(child, "name", 0)
			if name != nil && httpMethods[string(name.Text())] {
				if body := bodyBlock(child); body != nil {
					out = append(out, tsHandler{body: body, line: line, snippet: snippet})
				}
			}
		case "lexical_declaration", "variable_declaration":
			for j := 0; j < child.NamedChildCount(); j++ {
				vd := child.NamedChild(j)
				if vd.Type() != "variable_declarator" {
					continue
				}
				name := field(vd, "name", 0)
				val := field(vd, "value", 1)
				if name == nil || val == nil || !httpMethods[string(name.Text())] {
					continue
				}
				if val.Type() == "arrow_function" || val.Type() == "function_expression" {
					if body := bodyBlock(val); body != nil {
						out = append(out, tsHandler{body: body, line: line, snippet: snippet})
					}
				}
			}
		}
	}
	return out
}

// idorItem builds the surface item for one handler, or reports ok=false when the
// handler does not exhibit BOTH an incoming identifier and a resource access.
func idorItem(h tsHandler, file string) (findings.SurfaceItem, bool) {
	idInputs := dedupe(collectIDInputs(h.body))
	accesses := dedupe(collectPrismaAccesses(h.body))
	if len(idInputs) == 0 || len(accesses) == 0 {
		return findings.SurfaceItem{}, false
	}
	authz := dedupe(collectAuthzCalls(h.body))

	signals := []string{
		"Receives a client-controlled identifier: " + strings.Join(idInputs, ", "),
		"Accesses a resource via a Prisma client method: " + strings.Join(accesses, ", "),
		authzSignal(authz),
	}
	return findings.SurfaceItem{
		Category:          "idor",
		File:              file,
		Line:              h.line,
		Snippet:           h.snippet,
		StructuralSignals: signals,
		ReasonToReview: "Does this handler verify that the authenticated caller is allowed to access " +
			"the specific resource named by the incoming identifier, before reading or modifying it?",
	}, true
}

// collectIDInputs records the client-controlled identifier inputs present in the
// body, as facts: params.<x>, nextUrl.searchParams, req.json().
func collectIDInputs(body syntax.Node) []string {
	var out []string
	walkTS(body, func(n syntax.Node) {
		if n.Type() != "member_expression" {
			return
		}
		obj := field(n, "object", 0)
		prop := field(n, "property", 1)
		if obj == nil || prop == nil {
			return
		}
		propText := string(prop.Text())
		switch {
		case obj.Type() == "identifier" && string(obj.Text()) == "params" &&
			(propText == "id" || idSuffix.MatchString(propText)):
			out = append(out, "reads params."+propText)
		case propText == "searchParams":
			out = append(out, "reads a query parameter via nextUrl.searchParams")
		case propText == "json" && obj.Type() == "identifier" &&
			(string(obj.Text()) == "req" || string(obj.Text()) == "request"):
			out = append(out, "reads the request body via req.json()")
		}
	})
	return out
}

// collectPrismaAccesses records resource accesses by SHAPE: a call whose callee
// is <client>.<model>.<method> where method is a known Prisma method. <client>
// is any identifier or member expression (db, prisma, this.prisma, ctx.db); the
// fact names the real client seen, not an assumed "prisma".
func collectPrismaAccesses(body syntax.Node) []string {
	var out []string
	walkTS(body, func(n syntax.Node) {
		if n.Type() != "call_expression" {
			return
		}
		fn := field(n, "function", 0) // expect <client>.<model>.<method>
		if fn == nil || fn.Type() != "member_expression" {
			return
		}
		method := field(fn, "property", 1)
		clientModel := field(fn, "object", 0) // expect member_expression <client>.<model>
		if method == nil || clientModel == nil || !prismaMethods[string(method.Text())] ||
			clientModel.Type() != "member_expression" {
			return
		}
		client := field(clientModel, "object", 0) // any identifier or member expression
		model := field(clientModel, "property", 1)
		if client == nil || model == nil {
			return
		}
		out = append(out, string(client.Text())+"."+string(model.Text())+"."+string(method.Text()))
	})
	return out
}

// collectAuthzCalls records calls to KNOWN authz helpers found in the body. It
// searches the body only — a helper called from the body that itself calls
// getServerSession is NOT followed, so it is honestly reported as not detected.
func collectAuthzCalls(body syntax.Node) []string {
	var out []string
	walkTS(body, func(n syntax.Node) {
		if n.Type() != "call_expression" {
			return
		}
		fn := field(n, "function", 0)
		if fn == nil {
			return
		}
		var name string
		switch fn.Type() {
		case "identifier":
			name = string(fn.Text())
		case "member_expression":
			if prop := field(fn, "property", 1); prop != nil {
				name = string(prop.Text())
			}
		}
		if authzHelperSet[name] {
			out = append(out, name)
		}
	})
	return out
}

// authzSignal phrases the authz fact: either which known helper was detected, or
// that none was — with the searched set declared. Never a judgment.
func authzSignal(found []string) string {
	if len(found) > 0 {
		return "An authorization helper call was detected in the handler body: " + strings.Join(found, ", ")
	}
	return "No call to a known authorization helper was detected in the handler body (searched: " +
		strings.Join(authzHelpers, ", ") + ")"
}

// --- small AST helpers ---

// walkTS visits n and every named descendant (pre-order).
func walkTS(n syntax.Node, visit func(syntax.Node)) {
	if n == nil {
		return
	}
	visit(n)
	for i := 0; i < n.NamedChildCount(); i++ {
		walkTS(n.NamedChild(i), visit)
	}
}

// field returns the child under a grammar field name, falling back to a
// positional named child when the parser does not expose the field.
func field(n syntax.Node, name string, pos int) syntax.Node {
	if c := n.ChildByField(name); c != nil {
		return c
	}
	if pos < n.NamedChildCount() {
		return n.NamedChild(pos)
	}
	return nil
}

// bodyBlock returns a function/arrow body statement_block.
func bodyBlock(fn syntax.Node) syntax.Node {
	if b := fn.ChildByField("body"); b != nil {
		return b
	}
	for i := 0; i < fn.NamedChildCount(); i++ {
		if c := fn.NamedChild(i); c.Type() == "statement_block" {
			return c
		}
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// isNextRouteFile reports whether path is a Next.js App Router route handler:
// a route.{ts,tsx,js,jsx} file somewhere under an app/ directory.
func isNextRouteFile(path string) bool {
	segs := strings.Split(strings.TrimPrefix(path, "./"), "/")
	if len(segs) == 0 {
		return false
	}
	base := segs[len(segs)-1]
	if base != "route.ts" && base != "route.tsx" && base != "route.js" && base != "route.jsx" {
		return false
	}
	for _, s := range segs[:len(segs)-1] {
		if s == "app" {
			return true
		}
	}
	return false
}
