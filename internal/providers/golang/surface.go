package golang

import (
	"go/ast"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// surfaceItems maps the auditable structural surface of a parsed Go file. Today
// it enumerates HTTP handlers as "authz" surface (every protectable handler the
// agent should verify enforces authentication/authorization). More categories
// (IDOR, over-fetching) are added as the Go provider grows.
func surfaceItems(p *parsed) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, decl := range p.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			continue
		}
		if isHTTPHandler(p, fn.Type.Params) {
			out = append(out, findings.SurfaceItem{
				Category:       "authz",
				File:           p.path,
				Line:           p.line(fn.Pos()),
				Snippet:        "func " + fn.Name.Name,
				ReasonToReview: "HTTP handler — verify it enforces authentication and authorization before accessing resources.",
			})
		}
	}
	return out
}

// isHTTPHandler reports whether a parameter list matches the stdlib handler
// shape: an http.ResponseWriter and a *http.Request.
func isHTTPHandler(p *parsed, params *ast.FieldList) bool {
	var hasWriter, hasRequest bool
	for _, field := range params.List {
		switch t := field.Type.(type) {
		case *ast.SelectorExpr:
			if isNetHTTP(p, t, "ResponseWriter") {
				hasWriter = true
			}
		case *ast.StarExpr:
			if sel, ok := t.X.(*ast.SelectorExpr); ok && isNetHTTP(p, sel, "Request") {
				hasRequest = true
			}
		}
	}
	return hasWriter && hasRequest
}

// isNetHTTP reports whether sel is net/http.<name>.
func isNetHTTP(p *parsed, sel *ast.SelectorExpr, name string) bool {
	return sel.Sel.Name == name && p.selectorPackagePath(sel) == "net/http"
}
