package golang_test

import (
	"os"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
)

func analyzeSurface(t *testing.T, path, src string) []findings.SurfaceItem {
	t.Helper()
	items, err := golang.New().AnalyzeSurface(providers.SourceFile{Path: path, Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzeSurface error: %v", err)
	}
	return items
}

func surfaceOf(items []findings.SurfaceItem, category string) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == category {
			out = append(out, it)
		}
	}
	return out
}

func TestSurfaceEnumeratesHTTPHandlers(t *testing.T) {
	src := `package x
import "net/http"
func ListPlants(w http.ResponseWriter, r *http.Request) {}
func helper(s string) string { return s }
func GetPlant(w http.ResponseWriter, r *http.Request) {}
`
	authz := surfaceOf(analyzeSurface(t, "routes.go", src), "authz")
	if len(authz) != 2 {
		t.Fatalf("want 2 authz surface items (the two handlers), got %d: %+v", len(authz), authz)
	}
	for _, it := range authz {
		if it.File != "routes.go" || it.Line == 0 {
			t.Errorf("surface item missing location: %+v", it)
		}
		if it.ReasonToReview == "" {
			t.Errorf("surface item must tell the agent what to verify: %+v", it)
		}
	}
}

func TestSurfaceIgnoresNonHandlers(t *testing.T) {
	src := `package x
func add(a, b int) int { return a + b }
func main() {}
`
	if items := analyzeSurface(t, "x.go", src); len(items) != 0 {
		t.Errorf("non-handler code should map no surface, got %+v", items)
	}
}

func TestSurfaceHandlerNeedsBothParams(t *testing.T) {
	// A function taking only http.ResponseWriter (no *http.Request) is not the
	// stdlib handler shape and must not be enumerated.
	src := `package x
import "net/http"
func notAHandler(w http.ResponseWriter) {}
`
	if authz := surfaceOf(analyzeSurface(t, "x.go", src), "authz"); len(authz) != 0 {
		t.Errorf("a one-param func is not an HTTP handler; got %+v", authz)
	}
}

func TestSurfaceParseError(t *testing.T) {
	_, err := golang.New().AnalyzeSurface(providers.SourceFile{Path: "bad.go", Content: []byte("not go")})
	if err == nil {
		t.Error("AnalyzeSurface should error on unparseable source")
	}
}

// readFixture loads the committed real-handler fixture (spec requirement: "A
// committed real-handler fixture exercises the Go surface producer end-to-end")
// so denial/helper detection is driven through the real parser, not a string
// literal.
func readFixture(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile("testdata/handlers.go")
	if err != nil {
		t.Fatalf("reading testdata/handlers.go: %v", err)
	}
	return src
}

func authzByFuncName(items []findings.SurfaceItem) map[string]findings.SurfaceItem {
	out := map[string]findings.SurfaceItem{}
	for _, it := range surfaceOf(items, "authz") {
		out[strings.TrimPrefix(it.Snippet, "func ")] = it
	}
	return out
}

// TestSurfaceAuthzDenialResponseDetected drives testdata/handlers.go through the
// real AnalyzeSurface and checks authz_denial_response_detected — the ALWAYS
// present fact (D2) — across all four true/false combinations the fixture was
// built to cover.
func TestSurfaceAuthzDenialResponseDetected(t *testing.T) {
	src := readFixture(t)
	items, err := golang.New().AnalyzeSurface(providers.SourceFile{Path: "testdata/handlers.go", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	byName := authzByFuncName(items)

	cases := []struct {
		name string
		want bool
	}{
		{"ListWidgets", true},   // helper + 403 denial
		{"GetWidget", false},    // no helper, no denial
		{"CreateWidget", true},  // no helper, 401 denial
		{"UpdateWidget", false}, // helper, no denial
	}
	for _, tc := range cases {
		it, ok := byName[tc.name]
		if !ok {
			t.Fatalf("no authz item for handler %s, got items: %+v", tc.name, items)
		}
		if got := it.StructuralFacts["authz_denial_response_detected"]; got != tc.want {
			t.Errorf("%s: authz_denial_response_detected = %v, want %v (facts=%+v)",
				tc.name, got, tc.want, it.StructuralFacts)
		}
	}
}

// TestSurfaceKnownAuthzDetected_AbsentWithoutHelpers is spec requirement
// "Go's authz item states only what go/ast body-scanning actually established":
// with no project helpers registered, known_authz_detected must be ABSENT, never
// a vacuous "false" against an empty searched set.
func TestSurfaceKnownAuthzDetected_AbsentWithoutHelpers(t *testing.T) {
	items := authzByFuncName(analyzeSurface(t, "testdata/handlers.go", string(readFixture(t))))
	for name, it := range items {
		if _, present := it.StructuralFacts["known_authz_detected"]; present {
			t.Errorf("%s: known_authz_detected must be ABSENT with no helpers configured, got %+v",
				name, it.StructuralFacts)
		}
	}
}

// TestSurfaceKnownAuthzDetected_WithRegisteredHelper drives the fixture through
// a provider constructed with WithAuthzHelpers and checks the fact flips exactly
// for the two handlers that call the registered helper.
func TestSurfaceKnownAuthzDetected_WithRegisteredHelper(t *testing.T) {
	src := readFixture(t)
	p := golang.New(golang.WithAuthzHelpers([]string{"requirePermission"}))
	items, err := p.AnalyzeSurface(providers.SourceFile{Path: "testdata/handlers.go", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	byName := authzByFuncName(items)

	cases := []struct {
		name string
		want bool
	}{
		{"ListWidgets", true},
		{"GetWidget", false},
		{"CreateWidget", false},
		{"UpdateWidget", true},
	}
	for _, tc := range cases {
		it, ok := byName[tc.name]
		if !ok {
			t.Fatalf("no authz item for handler %s", tc.name)
		}
		got, present := it.StructuralFacts["known_authz_detected"]
		if !present {
			t.Fatalf("%s: known_authz_detected must be PRESENT once a helper is registered", tc.name)
		}
		if got != tc.want {
			t.Errorf("%s: known_authz_detected = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestSurfaceAuthzSignals_NameHelperAndDeclareScope checks the signal prose:
// the true case names the helper by name; the false case (with helpers
// registered) declares the body-only scope AND the middleware/route-
// registration limit — never silently claiming that layer was checked (spec
// requirement "A fact go/ast cannot establish is a declared limit").
func TestSurfaceAuthzSignals_NameHelperAndDeclareScope(t *testing.T) {
	src := readFixture(t)
	p := golang.New(golang.WithAuthzHelpers([]string{"requirePermission"}))
	items, err := p.AnalyzeSurface(providers.SourceFile{Path: "testdata/handlers.go", Content: src})
	if err != nil {
		t.Fatal(err)
	}
	byName := authzByFuncName(items)

	detected := strings.Join(byName["ListWidgets"].StructuralSignals, " || ")
	if !strings.Contains(detected, "requirePermission") {
		t.Errorf("detected-case signal must name the helper, got %q", detected)
	}

	notDetected := strings.Join(byName["GetWidget"].StructuralSignals, " || ")
	if !strings.Contains(notDetected, "handler body") {
		t.Errorf("not-detected signal must state it only inspected the body, got %q", notDetected)
	}
	if !strings.Contains(notDetected, "middleware") {
		t.Errorf("not-detected signal must declare the route-registration/middleware limit, got %q", notDetected)
	}
}

// TestSurfaceAuthzSignals_NoHelpersConfigured checks the no-helpers-at-all case
// (the default: internal/mcp/surface.go and scanall.go's providerFor call
// e.New(nil)): the signal must say codefit cannot check for a known call,
// rather than silently omitting any mention of authz helpers.
func TestSurfaceAuthzSignals_NoHelpersConfigured(t *testing.T) {
	src := `package x
import "net/http"
func H(w http.ResponseWriter, r *http.Request) {}
`
	authz := surfaceOf(analyzeSurface(t, "x.go", src), "authz")
	if len(authz) != 1 {
		t.Fatalf("want 1 authz item, got %d", len(authz))
	}
	got := strings.Join(authz[0].StructuralSignals, " || ")
	if !strings.Contains(got, "no project authz helpers are registered") {
		t.Errorf("signal must state no helpers are registered, got %q", got)
	}
	if _, present := authz[0].StructuralFacts["known_authz_detected"]; present {
		t.Errorf("known_authz_detected must be absent, got %+v", authz[0].StructuralFacts)
	}
}
