package registry_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// TestGoEntry_ConsumesAuthzHelpers locks D3: the "go" entry in registry.table
// must stop discarding the authzHelpers parameter — the same wiring
// TypeScript's entry already has. Before this change, New ignored the slice
// entirely (`func(_ []string) providers.LanguageProvider { return golang.New() }`),
// so a project helper registered via codefit-baseline-register-authz-helper
// never reached the Go provider through codefit-scan-security/codefit-scan-all
// (internal/mcp/scanall.go's providerForLanguage threads authzHelpers into
// e.New(authzHelpers)).
func TestGoEntry_ConsumesAuthzHelpers(t *testing.T) {
	e, ok := registry.ByName("go")
	if !ok {
		t.Fatal(`registry.ByName("go") not found`)
	}
	src := `package x

import "net/http"

func H(w http.ResponseWriter, r *http.Request) {
	requirePermission(r)
	http.Error(w, "no", http.StatusForbidden)
}

func requirePermission(r *http.Request) bool { return true }
`
	p := e.New([]string{"requirePermission"})
	items, err := p.AnalyzeSurface(providers.SourceFile{Path: "x.go", Content: []byte(src)})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range items {
		if it.Category == "authz" && it.StructuralFacts["known_authz_detected"] {
			found = true
		}
	}
	if !found {
		t.Fatalf(`registry.ByName("go").New([...helpers]) must produce known_authz_detected=true `+
			"when the handler calls a registered helper, got %+v", items)
	}
}
