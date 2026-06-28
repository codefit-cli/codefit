package typescript

import (
	"regexp"
	"sort"
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
type idorQuery struct{ recognized map[string]bool }

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

func (q idorQuery) Enumerate(root syntax.Node, file string) []findings.SurfaceItem {
	if root == nil {
		return nil
	}
	var out []findings.SurfaceItem
	for _, h := range auditTargets(root, file) {
		if item, ok := idorItem(h, file, q.recognized); ok {
			out = append(out, item)
		}
	}
	return out
}

// tsHandler is an auditable entry point — a Next.js route handler OR a Server
// Action. kind distinguishes them ("route" | "action"); the downstream mold
// (Prisma detection, the authz-helper signal, the frontier to the agent) is
// shared. method holds the HTTP verb for a route handler and the function name
// for a Server Action; params is the formal_parameters node (Server Actions only
// — their client input is the arguments, not req.params/query). body is the only
// scope the signals are searched in — searching wider would let authz in a called
// helper masquerade as authz in the entry.
type tsHandler struct {
	kind    string
	method  string
	params  syntax.Node
	body    syntax.Node
	line    int
	snippet string
}

// auditTargets returns every auditable entry in this file: Next.js route handlers
// (HTTP-method exports under app/**/route.ts) AND Next.js Server Actions ("use
// server", detected by shape, in any file). Route handlers and Server Actions
// feed the same per-category item builders; only discovery and the id-input mold
// differ (ADR 0005 — enumerate by shape, never by filename).
func auditTargets(root syntax.Node, file string) []tsHandler {
	var out []tsHandler
	if isNextRouteFile(file) {
		walkTS(root, func(n syntax.Node) {
			if n.Type() == "export_statement" {
				out = append(out, handlersIn(n)...)
			}
		})
	}
	return append(out, serverActionsIn(root)...)
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
					out = append(out, tsHandler{kind: "route", method: string(name.Text()), body: body, line: line, snippet: snippet})
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
						out = append(out, tsHandler{kind: "route", method: string(name.Text()), body: body, line: line, snippet: snippet})
					}
				}
			}
		}
	}
	return out
}

// idorItem builds the surface item for one handler, or reports ok=false when the
// handler does not exhibit an incoming identifier that reaches a resource. Per
// the frontier principle (ADR 0005): enumerate if a client id-input is present
// AND the id reaches a resource — either a local Prisma access (FINITE,
// enumerated) OR the id leaves the body as a call argument (INFINITE, handed to
// the agent with an honest signal). An id that is read but goes nowhere is not
// surface.
func idorItem(h tsHandler, file string, recognized map[string]bool) (findings.SurfaceItem, bool) {
	idInputs, idVars := collectInputs(h)
	accesses := dedupe(collectPrismaAccesses(h.body))
	indirect := collectIndirectUses(h.body, idVars)
	if len(idInputs) == 0 || (len(accesses) == 0 && len(indirect) == 0) {
		return findings.SurfaceItem{}, false
	}
	authz := dedupe(collectAuthzCalls(h.body, recognized))

	signals := []string{
		"Receives a client-controlled identifier: " + strings.Join(idInputs, "; "),
		accessSignal(accesses, indirect),
		authzSignal(authz, recognized),
	}
	return findings.SurfaceItem{
		Category:          "idor",
		File:              file,
		Line:              h.line,
		Snippet:           h.snippet,
		StructuralSignals: signals,
		StructuralFacts: map[string]bool{
			"local_access_detected": len(accesses) > 0,
			// known_authz_detected: whether a known authz/ownership helper was seen
			// in the body. False on an id→resource handler is the actionable access
			// gap. Shared with authz so the synthesis classifies the gap as a fact.
			"known_authz_detected": len(authz) > 0,
			// server_action: a structural fact (not a judgment) — this entry is a
			// Next.js Server Action ("use server"), not a route handler. It lets the
			// agent weigh the heightened risk (actions are POST endpoints devs often
			// do not treat as endpoints) and the baseline filter by entry kind.
			"server_action": h.kind == "action",
		},
		ReasonToReview: "Does this handler verify that the authenticated caller is allowed to access " +
			"the specific resource named by the incoming identifier, before reading or modifying it?",
	}, true
}

// accessSignal phrases the resource-access fact. A local Prisma access is named
// directly. Otherwise the id leaves the body indirectly — codefit does not chase
// where to (ADR 0005). The frontier signal states the limit as an AFFIRMATION
// (codefit does not follow calls, so this access is an UNRESOLVED candidate, not
// an absence) and makes following the data its own instruction, so the agent
// pursues it instead of discarding it as a false positive.
func accessSignal(accesses, indirect []string) string {
	if len(accesses) > 0 {
		return "Accesses a resource via a Prisma client method: " + strings.Join(accesses, ", ")
	}
	return "The identifier flows into " + strings.Join(indirect, ", ") + ", outside this handler. " +
		"codefit does not follow calls across functions, so this resource access is NOT verified here — " +
		"it runs in a service/repository layer. Follow the identifier there to determine whether ownership " +
		"is checked before the resource is read or modified."
}

// collectInputs returns the client-controlled identifier inputs (signals) and the
// local variable names bound to them (idVars), dispatching on the entry kind: a
// route handler reads them from the request (params/query/body idioms), a Server
// Action receives them as its arguments (or a FormData). The two molds differ
// only in where the input enters; the downstream access/authz logic is shared.
func collectInputs(h tsHandler) (signals []string, idVars map[string]bool) {
	if h.kind == "action" {
		return collectActionInputs(h)
	}
	return collectIDInputs(h.body)
}

// collectIDInputs records the client-controlled identifier inputs present in the
// body as facts, covering the FINITE set of Next 15/16 input idioms (ADR 0005):
// route params (params.id AND `const {id} = await params`), query params
// (req.nextUrl.searchParams AND `const {searchParams} = new URL(req.url)`, with
// .get(key)), and the request body (req.json()/formData()). It also returns the
// local variable names bound to those ids, so indirect use can be detected.
func collectIDInputs(body syntax.Node) (signals []string, idVars map[string]bool) {
	idVars = map[string]bool{}
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			signals = append(signals, s)
		}
	}
	var queryKeys []string

	walkTS(body, func(n syntax.Node) {
		switch n.Type() {
		case "member_expression":
			obj := field(n, "object", 0)
			prop := field(n, "property", 1)
			if obj == nil || prop == nil {
				return
			}
			p := string(prop.Text())
			switch {
			case obj.Type() == "identifier" && string(obj.Text()) == "params" &&
				(p == "id" || idSuffix.MatchString(p)):
				add("reads route param params." + p)
			case p == "searchParams":
				add("reads query parameters (searchParams)")
			case (p == "json" || p == "formData") && obj.Type() == "identifier" &&
				(string(obj.Text()) == "req" || string(obj.Text()) == "request"):
				add("reads the request body via " + string(obj.Text()) + "." + p + "()")
			}
		case "variable_declarator":
			name := field(n, "name", 0)
			val := field(n, "value", 1)
			if name == nil || val == nil {
				return
			}
			switch {
			case name.Type() == "object_pattern" && isParamsSource(val):
				names := patternNames(name)
				add("reads route param(s) via destructured params: " + strings.Join(names, ", "))
				for _, nm := range names {
					idVars[nm] = true
				}
			case name.Type() == "object_pattern" && isBodySource(val):
				names := patternNames(name)
				add("reads request body field(s): " + strings.Join(names, ", "))
				for _, nm := range names {
					idVars[nm] = true
				}
			case name.Type() == "object_pattern" && patternHas(name, "searchParams"):
				add("reads query parameters (destructured searchParams)")
			case name.Type() == "identifier" && isIDBearing(val, idVars):
				idVars[string(name.Text())] = true
			}
		case "call_expression":
			if k, ok := searchParamKey(n); ok {
				queryKeys = append(queryKeys, k)
			}
		}
	})
	if len(queryKeys) > 0 {
		add("query parameter keys read: " + strings.Join(dedupe(queryKeys), ", "))
	}
	return signals, idVars
}

// collectIndirectUses records non-Prisma calls that receive an id as an argument
// — the id leaving the body (ADR 0005). codefit does NOT identify the callee as
// a service by name or shape; it only reports that the id is passed to it. The
// agent follows the data from there.
func collectIndirectUses(body syntax.Node, idVars map[string]bool) []string {
	var out []string
	walkTS(body, func(n syntax.Node) {
		// A Prisma access is the local case; a schema validation (parse/safeParse)
		// is not a resource access — neither is an indirect access callee.
		if n.Type() != "call_expression" || isPrismaCall(n) || isValidationCall(n) {
			return
		}
		args := field(n, "arguments", 1)
		if args == nil {
			return
		}
		if containsIDArg(args, idVars) {
			if c := calleeName(n); c != "" {
				out = append(out, c)
			}
		}
	})
	return dedupe(out)
}

// isValidationCall reports whether a call is a schema validation (X.parse(...) /
// X.safeParse(...)). Passing an id to a validator is not a resource access, so it
// must not be reported as an indirect access callee (calibration after Bitácora).
func isValidationCall(call syntax.Node) bool {
	fn := field(call, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return false
	}
	p := field(fn, "property", 1)
	return p != nil && (string(p.Text()) == "parse" || string(p.Text()) == "safeParse")
}

// containsIDArg reports whether an id-bearing expression is a DIRECT argument of
// a call — it descends through wrappers (objects, awaits, members) but stops at
// nested call_expressions, since an id used inside a nested call is consumed by
// THAT call, not its enclosing one (so `Response.json(getById(id))` attributes
// the id to getById, not Response.json).
func containsIDArg(args syntax.Node, idVars map[string]bool) bool {
	found := false
	var visit func(n syntax.Node, depth int)
	visit = func(n syntax.Node, depth int) {
		if n == nil || found {
			return
		}
		if isIDBearing(n, idVars) {
			found = true
			return
		}
		if depth > 0 && n.Type() == "call_expression" {
			return // a nested call consumes the id itself; do not attribute upward
		}
		for i := 0; i < n.NamedChildCount(); i++ {
			visit(n.NamedChild(i), depth+1)
		}
	}
	visit(args, 0)
	return found
}

// isParamsSource reports whether val reads the route params object: `await
// params`, `await ctx.params`, `params`, or `ctx.params`.
func isParamsSource(val syntax.Node) bool {
	if val.Type() == "await_expression" && val.NamedChildCount() == 1 {
		val = val.NamedChild(0)
	}
	switch val.Type() {
	case "identifier":
		return string(val.Text()) == "params"
	case "member_expression":
		p := field(val, "property", 1)
		return p != nil && string(p.Text()) == "params"
	}
	return false
}

// isBodySource reports whether val reads the request body: `await req.json()`,
// `req.json()`, `await req.formData()`, etc.
func isBodySource(val syntax.Node) bool {
	if val.Type() == "await_expression" && val.NamedChildCount() == 1 {
		val = val.NamedChild(0)
	}
	if val.Type() != "call_expression" {
		return false
	}
	fn := field(val, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return false
	}
	obj := field(fn, "object", 0)
	prop := field(fn, "property", 1)
	return obj != nil && prop != nil && obj.Type() == "identifier" &&
		(string(obj.Text()) == "req" || string(obj.Text()) == "request") &&
		(string(prop.Text()) == "json" || string(prop.Text()) == "formData")
}

// isIDBearing reports whether node is an id-bearing expression: an id variable,
// a `params.<id>` access, or a `searchParams.get(...)` call.
func isIDBearing(node syntax.Node, idVars map[string]bool) bool {
	switch node.Type() {
	case "identifier":
		return idVars[string(node.Text())]
	case "member_expression":
		obj := field(node, "object", 0)
		prop := field(node, "property", 1)
		return obj != nil && prop != nil && obj.Type() == "identifier" &&
			string(obj.Text()) == "params" &&
			(string(prop.Text()) == "id" || idSuffix.MatchString(string(prop.Text())))
	case "call_expression":
		_, ok := searchParamKey(node)
		return ok
	}
	return false
}

// searchParamKey returns the key string of a `searchParams.get("key")` call.
func searchParamKey(call syntax.Node) (string, bool) {
	fn := field(call, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return "", false
	}
	prop := field(fn, "property", 1)
	obj := field(fn, "object", 0)
	if prop == nil || obj == nil || string(prop.Text()) != "get" || !isSearchParamsRef(obj) {
		return "", false
	}
	args := field(call, "arguments", 1)
	if args == nil || args.NamedChildCount() == 0 || args.NamedChild(0).Type() != "string" {
		return "", false
	}
	return strings.Trim(string(args.NamedChild(0).Text()), `"'`+"`"), true
}

// isSearchParamsRef reports whether node refers to a searchParams object — the
// identifier `searchParams` or a `*.searchParams` member access.
func isSearchParamsRef(node syntax.Node) bool {
	if node.Type() == "identifier" {
		return string(node.Text()) == "searchParams"
	}
	if node.Type() == "member_expression" {
		p := field(node, "property", 1)
		return p != nil && string(p.Text()) == "searchParams"
	}
	return false
}

// isPrismaCall reports whether a call is a <client>.<model>.<prismaMethod>(...)
// access — the same shape collectPrismaAccesses records.
func isPrismaCall(call syntax.Node) bool {
	fn := field(call, "function", 0)
	if fn == nil || fn.Type() != "member_expression" {
		return false
	}
	method := field(fn, "property", 1)
	clientModel := field(fn, "object", 0)
	return method != nil && clientModel != nil && prismaMethods[string(method.Text())] &&
		clientModel.Type() == "member_expression"
}

// calleeName returns a readable name for a call's callee (an identifier or the
// `object.property` of a member expression).
func calleeName(call syntax.Node) string {
	fn := field(call, "function", 0)
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return string(fn.Text())
	case "member_expression":
		obj := field(fn, "object", 0)
		prop := field(fn, "property", 1)
		if obj != nil && prop != nil && obj.Type() == "identifier" {
			return string(obj.Text()) + "." + string(prop.Text())
		}
		if prop != nil {
			return string(prop.Text())
		}
	}
	return ""
}

// patternNames returns the local names bound by an object_pattern (shorthand
// `{id}` → "id"; aliased `{id: noteId}` → "noteId").
func patternNames(pattern syntax.Node) []string {
	var out []string
	for i := 0; i < pattern.NamedChildCount(); i++ {
		c := pattern.NamedChild(i)
		switch c.Type() {
		case "shorthand_property_identifier_pattern":
			out = append(out, string(c.Text()))
		case "pair_pattern":
			if v := field(c, "value", 1); v != nil && v.Type() == "identifier" {
				out = append(out, string(v.Text()))
			}
		}
	}
	return out
}

// patternHas reports whether an object_pattern binds the given shorthand name.
func patternHas(pattern syntax.Node, name string) bool {
	for i := 0; i < pattern.NamedChildCount(); i++ {
		c := pattern.NamedChild(i)
		if c.Type() == "shorthand_property_identifier_pattern" && string(c.Text()) == name {
			return true
		}
		if c.Type() == "pair_pattern" {
			if k := field(c, "key", 0); k != nil && string(k.Text()) == name {
				return true
			}
		}
	}
	return false
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
func collectAuthzCalls(body syntax.Node, recognized map[string]bool) []string {
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
		// Built-in (NextAuth-style) OR a helper the project registered (ADR 0013).
		if authzHelperSet[name] || recognized[name] {
			out = append(out, name)
		}
	})
	return out
}

// authzSignal phrases the authz fact: either which known helper was detected, or
// that none was — with the searched set declared. A project-registered helper is
// labelled as such (traceability: the agent/human sees WHY codefit now recognizes
// it). Never a judgment.
func authzSignal(found []string, recognized map[string]bool) string {
	if len(found) > 0 {
		labelled := make([]string, len(found))
		for i, n := range found {
			if recognized[n] && !authzHelperSet[n] {
				labelled[i] = n + " (registered for this project)"
			} else {
				labelled[i] = n
			}
		}
		return "An authorization helper call was detected in the body: " + strings.Join(labelled, ", ")
	}
	searched := authzHelpers
	if extra := registeredOnly(recognized); len(extra) > 0 {
		searched = append(append([]string{}, authzHelpers...), extra...)
	}
	return "No call to a known authorization helper was detected in the body (searched: " +
		strings.Join(searched, ", ") + ")"
}

// registeredOnly returns the recognized helper names that are NOT built-in, sorted
// for a stable signal.
func registeredOnly(recognized map[string]bool) []string {
	var out []string
	for n := range recognized {
		if !authzHelperSet[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
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
