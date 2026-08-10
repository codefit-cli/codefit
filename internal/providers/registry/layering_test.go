package registry_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestLayering_ProvidersRootNeverImportsRegistryOrConcreteProviders is C0's
// first assertion: internal/providers (root) must never import its own
// registry subpackage, nor the concrete golang/typescript packages. Directory
// nesting creates no import edge in Go — the one edge that WOULD break the
// layering rule is internal/providers importing internal/providers/registry,
// so this is checked directly, not assumed. Cloned from the existing
// go-list-deps idiom at internal/core/dbrules/layering_test.go:18-34,
// including its testing.Short() skip.
func TestLayering_ProvidersRootNeverImportsRegistryOrConcreteProviders(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to `go list`; skipped in -short")
	}
	out, err := exec.Command("go", "list", "-deps", "github.com/codefit-cli/codefit/internal/providers").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps internal/providers: %v\n%s", err, out)
	}
	deps := string(out)
	for _, forbidden := range []string{
		"internal/providers/registry",
		"internal/providers/golang",
		"internal/providers/typescript",
	} {
		if strings.Contains(deps, forbidden) {
			t.Errorf("internal/providers (root) must never import %s (layering violation), deps:\n%s", forbidden, deps)
		}
	}
}

// TestLayering_CoreSurfaceNeverImportsProviders is C0's second assertion: the
// providers -> core/surface edge is strictly one-directional. core/surface
// owns the vocabulary because the vocabulary names no provider; the moment it
// imports internal/providers (in any form, including the root), that
// direction is violated.
func TestLayering_CoreSurfaceNeverImportsProviders(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to `go list`; skipped in -short")
	}
	out, err := exec.Command("go", "list", "-deps", "github.com/codefit-cli/codefit/internal/core/surface").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps internal/core/surface: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "internal/providers") {
		t.Errorf("internal/core/surface must never import internal/providers (layering violation), deps:\n%s", out)
	}
}

// TestLayering_NoCorePackageImportsRegistry is C0's third assertion: no
// package under internal/core/... may import internal/providers/registry —
// the registry NAMES providers (it is the capability/exposure table), and the
// core must stay ignorant of every concrete provider it maps.
func TestLayering_NoCorePackageImportsRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to `go list`; skipped in -short")
	}
	listOut, err := exec.Command("go", "list", "github.com/codefit-cli/codefit/internal/core/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list internal/core/...: %v\n%s", err, listOut)
	}
	pkgs := strings.Fields(strings.TrimSpace(string(listOut)))
	if len(pkgs) == 0 {
		t.Fatal("go list internal/core/... returned no packages — the probe itself is broken")
	}
	for _, pkg := range pkgs {
		out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		if strings.Contains(string(out), "internal/providers/registry") {
			t.Errorf("%s must never import internal/providers/registry (layering violation), deps:\n%s", pkg, out)
		}
	}
}
