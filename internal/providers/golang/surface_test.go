package golang_test

import (
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
