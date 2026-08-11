package golang

import (
	"go/ast"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// denialStatuses are the net/http status constants that indicate the handler is
// refusing the request (401/403). Used to detect a body-written denial response.
var denialStatuses = map[string]bool{
	"StatusUnauthorized": true,
	"StatusForbidden":    true,
}

// surfaceItems maps the auditable structural surface of a parsed Go file. Today
// it enumerates HTTP handlers as "authz" surface (every protectable handler the
// agent should verify enforces authentication/authorization). More categories
// (IDOR, over-fetching) are added as the Go provider grows.
//
// StructuralFacts states only what a go/ast body scan can honestly establish —
// no go/types, no following calls across functions:
//   - "known_authz_detected" is present ONLY when helpers is non-empty (a
//     false against an empty searched set would be the vacuous claim the spec
//     forbids); when present, it is true iff the handler body calls one of the
//     project's registered authz helpers (matched by NAME, not resolved).
//   - "authz_denial_response_detected" is ALWAYS present: whether the body
//     writes a 401/403 denial response. This is what keeps the item honest and
//     non-hollow in the common default case — internal/mcp/surface.go and
//     scanall.go's providerFor both pass nil/empty helpers today, so "no
//     helper configured" is the usual path, not the corner.
//
// Declared limit: this inspects ONLY the handler's own function body. It does
// NOT see route-registration or middleware/wrapper authorization (mux.Use, chi
// middleware, RequireAuth(handler)) — nothing here, in StructuralSignals or
// otherwise, claims that layer was checked and found absent (see
// docs/decisions/0067-every-surface-producer-emits-non-nil-structural-facts.md).
func surfaceItems(p *parsed, helpers map[string]bool) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, decl := range p.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			continue
		}
		writer, hasWriter, hasRequest := httpHandlerParams(p, fn.Type.Params)
		if !hasWriter || !hasRequest {
			continue
		}
		out = append(out, findings.SurfaceItem{
			Category:          "authz",
			File:              p.path,
			Line:              p.line(fn.Pos()),
			Snippet:           "func " + fn.Name.Name,
			StructuralSignals: authzSignals(p, fn, writer, helpers),
			StructuralFacts:   authzFacts(p, fn, writer, helpers),
			ReasonToReview:    "HTTP handler — verify it enforces authentication and authorization before accessing resources.",
		})
	}
	return out
}

// authzFacts computes the queryable facts for one handler body (D2).
func authzFacts(p *parsed, fn *ast.FuncDecl, writer string, helpers map[string]bool) map[string]bool {
	facts := map[string]bool{
		"authz_denial_response_detected": detectsDenialResponse(p, fn.Body, writer),
	}
	if len(helpers) > 0 {
		facts["known_authz_detected"] = len(authzHelperCalls(fn.Body, helpers)) > 0
	}
	return facts
}

// authzSignals phrases the same facts as verifiable syntactic statements, honest
// about what was searched (the body only) and what was found — never a
// judgment, and never silent about the middleware/route-registration limit.
func authzSignals(p *parsed, fn *ast.FuncDecl, writer string, helpers map[string]bool) []string {
	var out []string
	switch {
	case len(helpers) == 0:
		out = append(out, "no project authz helpers are registered — codefit cannot check for a known "+
			"authz call (register one with codefit-baseline-register-authz-helper)")
	default:
		if names := authzHelperCalls(fn.Body, helpers); len(names) > 0 {
			out = append(out, "the body calls the registered authz helper(s): "+strings.Join(names, ", "))
		} else {
			out = append(out, "no call to a registered authz helper was detected in the handler body — "+
				"codefit only inspects the body, not route registration or middleware")
		}
	}
	if detectsDenialResponse(p, fn.Body, writer) {
		out = append(out, "the body writes a 401/403 denial response")
	} else {
		out = append(out, "the body writes no 401/403 denial response")
	}
	return out
}

// authzHelperCalls returns the deduplicated, registered authz helper names
// called anywhere in body — matched by the callee's NAME (a bare identifier, or
// the .Sel of a selector expression), never resolved via go/types.
func authzHelperCalls(body *ast.BlockStmt, helpers map[string]bool) []string {
	if body == nil || len(helpers) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name, ok := authzHelperCallee(call, helpers); ok && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		return true
	})
	return out
}

// authzHelperCallee reports the registered helper name a call's callee matches,
// if any.
func authzHelperCallee(call *ast.CallExpr, helpers map[string]bool) (string, bool) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		if helpers[fn.Name] {
			return fn.Name, true
		}
	case *ast.SelectorExpr:
		if helpers[fn.Sel.Name] {
			return fn.Sel.Name, true
		}
	}
	return "", false
}

// detectsDenialResponse reports whether body writes a 401/403 denial response:
// net/http.Error(w, msg, http.StatusUnauthorized|StatusForbidden), or
// <writer>.WriteHeader(http.StatusUnauthorized|StatusForbidden) on the
// handler's own writer parameter (writer, read off the signature — never
// hardcoded to "w").
func detectsDenialResponse(p *parsed, body *ast.BlockStmt, writer string) bool {
	if body == nil || writer == "" {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch {
		case isNetHTTP(p, sel, "Error") && len(call.Args) >= 3 && isDenialStatusArg(call.Args[2]):
			found = true
		case sel.Sel.Name == "WriteHeader" && isWriterIdent(sel.X, writer) &&
			len(call.Args) >= 1 && isDenialStatusArg(call.Args[0]):
			found = true
		}
		return true
	})
	return found
}

// isWriterIdent reports whether expr is a bare identifier matching name — used
// to confirm a WriteHeader call is on the handler's own writer parameter.
func isWriterIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

// isDenialStatusArg reports whether expr is a net/http.Status(Unauthorized|Forbidden)
// selector.
func isDenialStatusArg(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	return ok && denialStatuses[sel.Sel.Name]
}

// httpHandlerParams reports whether a parameter list matches the stdlib handler
// shape (http.ResponseWriter, *http.Request) and, when it does, the writer
// parameter's name (needed to recognize <writer>.WriteHeader(...) as this
// handler's own denial response — empty when the parameter is unnamed).
func httpHandlerParams(p *parsed, params *ast.FieldList) (writer string, hasWriter, hasRequest bool) {
	for _, field := range params.List {
		switch t := field.Type.(type) {
		case *ast.SelectorExpr:
			if isNetHTTP(p, t, "ResponseWriter") {
				hasWriter = true
				if len(field.Names) > 0 {
					writer = field.Names[0].Name
				}
			}
		case *ast.StarExpr:
			if sel, ok := t.X.(*ast.SelectorExpr); ok && isNetHTTP(p, sel, "Request") {
				hasRequest = true
			}
		}
	}
	return writer, hasWriter, hasRequest
}

// isNetHTTP reports whether sel is net/http.<name>.
func isNetHTTP(p *parsed, sel *ast.SelectorExpr, name string) bool {
	return sel.Sel.Name == name && p.selectorPackagePath(sel) == "net/http"
}
