package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

func authzItems(t *testing.T, p *typescript.Provider, path, src string) []findings.SurfaceItem {
	t.Helper()
	items, err := p.AnalyzeSurface(providers.SourceFile{Path: path, Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzeSurface: %v", err)
	}
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == "authz" {
			out = append(out, it)
		}
	}
	return out
}

// A handler guarded ONLY by a project-specific helper codefit does not know
// built-in: without registration known_authz_detected is false; registering the
// helper flips it to true and labels it as project-registered (traceability).
func TestProvider_RegisteredAuthzHelperFlipsFact(t *testing.T) {
	src := `
export async function POST(req: Request) {
  await requirePermission("admin");
  await prisma.setting.update({ where: { key: "x" }, data: { value: "y" } });
}`

	// Built-in only: requirePermission is unknown → no known authz detected.
	base := authzItems(t, typescript.New(), "app/admin/route.ts", src)
	if len(base) != 1 {
		t.Fatalf("want 1 authz item, got %d", len(base))
	}
	if base[0].StructuralFacts["known_authz_detected"] {
		t.Errorf("a custom helper must NOT be recognized by the built-in set, got known_authz_detected=true")
	}

	// With the project helper registered: the fact flips, and the signal labels it.
	reg := authzItems(t, typescript.New(typescript.WithAuthzHelpers([]string{"requirePermission"})), "app/admin/route.ts", src)
	if len(reg) != 1 || !reg[0].StructuralFacts["known_authz_detected"] {
		t.Fatalf("a registered helper must set known_authz_detected=true, got %+v", reg)
	}
	sig := strings.Join(reg[0].StructuralSignals, " || ")
	if !strings.Contains(sig, "requirePermission") || !strings.Contains(strings.ToLower(sig), "registered for this project") {
		t.Errorf("the signal must name the helper and label it project-registered, got %q", sig)
	}
}

// Safeguard 2 at the fact level: registering a helper flips known_authz_detected on
// an IDOR item too (the fact "a helper is present" is honest), but the IDOR item is
// still enumerated — the report layer keeps it actionable (ownership). Here we only
// assert the provider keeps emitting the IDOR item with the fact true.
func TestProvider_RegisteredHelperOnIDORStillEnumerates(t *testing.T) {
	src := `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  await requirePermission("orders:read");
  return Response.json(await prisma.order.findUnique({ where: { id: params.id } }));
}`
	p := typescript.New(typescript.WithAuthzHelpers([]string{"requirePermission"}))
	items, err := p.AnalyzeSurface(providers.SourceFile{Path: "app/orders/[id]/route.ts", Content: []byte(src)})
	if err != nil {
		t.Fatal(err)
	}
	var idor *findings.SurfaceItem
	for i := range items {
		if items[i].Category == "idor" {
			idor = &items[i]
		}
	}
	if idor == nil {
		t.Fatalf("the IDOR item must still be enumerated even with a registered helper present")
	}
	if !idor.StructuralFacts["known_authz_detected"] {
		t.Errorf("the registered helper must flip the fact on the IDOR item too, got %+v", idor.StructuralFacts)
	}
}
