package typescript

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// authzQuery enumerates the broken-authorization surface of a Next.js App Router
// file: every route handler that does something SENSITIVE — touches data or
// mutates state — so the agent can verify the caller is allowed to. It reuses
// the IDOR mold wholesale (handlers, Prisma detection, the known authz-helper
// signal, the frontier to the agent); only the Fase-2 condition differs. IDOR
// asks "is ownership of the resource checked?" and needs an id reaching a
// resource; authz asks "is the caller permitted?" and only needs a sensitive
// operation — it is broader, and does NOT require an id. It implements
// surface.Query.
type authzQuery struct{}

// prismaWriteMethods are the Prisma operations that mutate state (vs read).
var prismaWriteMethods = map[string]bool{
	"create": true, "createMany": true, "createManyAndReturn": true,
	"update": true, "updateMany": true, "updateManyAndReturn": true,
	"upsert": true, "delete": true, "deleteMany": true,
}

// frameworkCallees are calls that are NOT a sensitive operation: response
// building, logging, URL/JSON utilities, and the auth helpers themselves. Used
// to decide whether an indirect call is application code (a possible service/
// repository access) rather than framework noise. This is NOT a service-name
// filter — it excludes obvious framework calls, the remaining application calls
// are reported honestly without claiming they ARE a service.
var frameworkCallees = map[string]bool{
	"NextResponse": true, "Response": true, "NextAuth": true, "console": true,
	"URL": true, "Headers": true, "JSON": true, "Math": true, "Date": true,
	"Number": true, "String": true, "Boolean": true, "Array": true, "Object": true,
	"Promise": true, "fetch": true, "redirect": true, "parseInt": true,
	"parseFloat": true, "isNaN": true, "setTimeout": true, "structuredClone": true,
	"Error": true,
}

// frameworkMethods are method names that are accessors/utilities, not a resource
// access or mutation.
var frameworkMethods = map[string]bool{
	"json": true, "text": true, "get": true, "set": true, "has": true,
	"stringify": true, "map": true, "filter": true, "forEach": true, "find": true,
	"reduce": true, "includes": true, "split": true, "join": true, "slice": true,
	"toISOString": true, "toString": true, "then": true, "catch": true,
	"format": true, "flatten": true, "keys": true, "values": true, "entries": true,
	"formData": true, "next": true, "redirect": true,
}

func (authzQuery) Enumerate(root syntax.Node, file string) []findings.SurfaceItem {
	if root == nil || !isNextRouteFile(file) {
		return nil
	}
	var out []findings.SurfaceItem
	walkTS(root, func(n syntax.Node) {
		if n.Type() != "export_statement" {
			return
		}
		for _, h := range handlersIn(n) {
			if item, ok := authzItem(h, file); ok {
				out = append(out, item)
			}
		}
	})
	return out
}

// authzItem builds the surface item for one handler, or reports ok=false when the
// handler does nothing sensitive (touches no data and mutates nothing). Per the
// frontier principle (ADR 0005): a sensitive operation is a local Prisma access
// (read or write — FINITE, enumerated) OR an indirect application call (the
// operation may be in a service/repository layer — INFINITE, handed to the
// agent with an honest signal).
func authzItem(h tsHandler, file string) (findings.SurfaceItem, bool) {
	accesses := dedupe(collectPrismaAccesses(h.body))
	indirect := collectAppCalls(h.body)
	if len(accesses) == 0 && len(indirect) == 0 {
		return findings.SurfaceItem{}, false
	}
	authz := dedupe(collectAuthzCalls(h.body))

	signals := []string{
		operationSignal(h.method, accesses, indirect),
		authzSignal(authz),
	}
	return findings.SurfaceItem{
		Category:          "authz",
		File:              file,
		Line:              h.line,
		Snippet:           h.snippet,
		StructuralSignals: signals,
		StructuralFacts:   map[string]bool{"known_authz_detected": len(authz) > 0},
		ReasonToReview: "Should this endpoint be public, or must it verify that the caller is " +
			"permitted before performing this operation — and is the absence of a detected " +
			"authorization check here intentional?",
	}, true
}

// operationSignal states, as a fact, what sensitive operation the handler
// performs: reads and/or mutates data via Prisma, or — when no local access is
// visible — that it calls application code where the operation may live.
func operationSignal(method string, accesses, indirect []string) string {
	if len(accesses) > 0 {
		var reads, writes []string
		for _, a := range accesses {
			if isPrismaWriteAccess(a) {
				writes = append(writes, a)
			} else {
				reads = append(reads, a)
			}
		}
		parts := []string{"Handler " + method}
		if len(writes) > 0 {
			parts = append(parts, "mutates state via "+strings.Join(writes, ", "))
		}
		if len(reads) > 0 {
			parts = append(parts, "accesses data via "+strings.Join(reads, ", "))
		}
		return strings.Join(parts, " ")
	}
	return "Handler " + method + " calls application code that may touch data or mutate state: " +
		strings.Join(indirect, ", ") + " — the operation may be in a service/repository layer (follow the call to confirm)"
}

// isPrismaWriteAccess reports whether a recorded access string
// (client.model.method) ends in a write method.
func isPrismaWriteAccess(access string) bool {
	parts := strings.Split(access, ".")
	return len(parts) > 0 && prismaWriteMethods[parts[len(parts)-1]]
}

// collectAppCalls records calls into application code — non-Prisma, non-validation,
// non-framework calls. These are the indirect sensitive-operation candidates (a
// service/repository call). codefit does not identify them as services by name or
// shape; it only reports that the handler calls them, and the agent follows.
func collectAppCalls(body syntax.Node) []string {
	var out []string
	walkTS(body, func(n syntax.Node) {
		if n.Type() != "call_expression" || isPrismaCall(n) || isValidationCall(n) || isFrameworkCall(n) {
			return
		}
		if c := calleeName(n); c != "" {
			out = append(out, c)
		}
	})
	return dedupe(out)
}

// isFrameworkCall reports whether a call is an obvious framework/utility call
// (not a sensitive operation): a known framework callee, a known auth helper, or
// an accessor/utility method.
func isFrameworkCall(call syntax.Node) bool {
	fn := field(call, "function", 0)
	if fn == nil {
		return false
	}
	switch fn.Type() {
	case "identifier":
		name := string(fn.Text())
		return frameworkCallees[name] || authzHelperSet[name]
	case "member_expression":
		obj := field(fn, "object", 0)
		prop := field(fn, "property", 1)
		if obj != nil && obj.Type() == "identifier" && frameworkCallees[string(obj.Text())] {
			return true
		}
		if prop != nil && frameworkMethods[string(prop.Text())] {
			return true
		}
	}
	return false
}
