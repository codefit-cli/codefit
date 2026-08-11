// Package handlers is a committed fixture exercising the Go surface producer
// through the real go/ast parser (internal/providers/golang/surface_test.go) —
// not a string literal embedded in a _test.go file, the exact gap that let the
// StructuralFacts regression ship undetected.
//
// codefit's own self-audit (internal/sensors/security/selfaudit_test.go) walks
// every *.go file in the repository as production, and "testdata" is not
// excluded in .codefit.yaml — the "testdata" directory name is only ignored by
// the go TOOL (build/vet/test), not by codefit's own filesystem walk. This file
// therefore deliberately triggers NO SEC-rule: no hardcoded-secret-shaped
// assignment, no math/rand feeding a security-named value, no weak hash, no
// string-concatenated SQL/exec argument, no inline os.Getenv.
package handlers

import "net/http"

// ListWidgets calls a project authz helper and denies with 403 when it
// refuses — both facts true.
func ListWidgets(w http.ResponseWriter, r *http.Request) {
	if !requirePermission(r, "widgets:read") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Write([]byte("ok"))
}

// GetWidget calls no authz helper and never denies — both facts false.
func GetWidget(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

// CreateWidget calls no authz helper but denies with 401 — helper false,
// denial true.
func CreateWidget(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// UpdateWidget calls a project authz helper but never writes a denial response
// of its own — helper true, denial false.
func UpdateWidget(w http.ResponseWriter, r *http.Request) {
	requirePermission(r, "widgets:write")
	w.Write([]byte("updated"))
}

// requirePermission stands in for a project-specific authz helper the test
// registers via WithAuthzHelpers; codefit matches it by NAME, not by resolving
// what it actually does (no go/types).
func requirePermission(r *http.Request, scope string) bool {
	return r.Header.Get("X-Scope") == scope
}

// formatSnippet is NOT an HTTP handler (wrong parameter shape) and must
// contribute no surface item.
func formatSnippet(s string) string {
	return s
}
