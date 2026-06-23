package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

func overfetchSurface(t *testing.T, path, src string) []findings.SurfaceItem {
	t.Helper()
	items, err := typescript.New().AnalyzeSurface(providers.SourceFile{Path: path, Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzeSurface(%s): %v", path, err)
	}
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == "overfetch" {
			out = append(out, it)
		}
	}
	return out
}

// The central case: serializing the result of an inline Prisma find with NO
// select/omit returns all columns — potential over-fetch. field_limiting_detected
// must be false, and the signals must name the model as a fact.
func TestOverfetch_InlineFindNoSelect(t *testing.T) {
	items := overfetchSurface(t, "app/users/route.ts", `
export async function GET() {
  return Response.json(await prisma.user.findMany());
}`)
	if len(items) != 1 {
		t.Fatalf("a serialized find with no select must be enumerated, got %d", len(items))
	}
	it := items[0]
	if it.StructuralFacts["field_limiting_detected"] {
		t.Errorf("a find with no select/omit must have field_limiting_detected=false, got %+v", it.StructuralFacts)
	}
	sig := strings.ToLower(signalsJoined(it))
	if !strings.Contains(sig, "user") || !strings.Contains(sig, "select") {
		t.Errorf("signals must name the model and the absence of select/omit, got %q", sig)
	}
	assertFactsNotJudgments(t, it)
}

// over-fetch reuses local_access_detected (the IDOR fact): true when the
// serialized value is a local Prisma find, false at the frontier (a service).
// The two facts together let the tool order by structural certainty.
func TestOverfetch_LocalAccessDetectedFact(t *testing.T) {
	local := overfetchSurface(t, "app/users/route.ts", `
export async function GET() { return Response.json(await prisma.user.findMany()); }`)
	if len(local) != 1 || !local[0].StructuralFacts["local_access_detected"] {
		t.Errorf("a local find serialization must set local_access_detected=true, got %+v", local)
	}
	frontier := overfetchSurface(t, "app/users/route.ts", `
export async function GET() { return Response.json(await UserService.getAll()); }`)
	if len(frontier) != 1 || frontier[0].StructuralFacts["local_access_detected"] {
		t.Errorf("a frontier serialization must set local_access_detected=false, got %+v", frontier)
	}
}

// An inline find WITH select limits fields → field_limiting_detected=true (still
// enumerated, lower priority).
func TestOverfetch_InlineFindWithSelect(t *testing.T) {
	items := overfetchSurface(t, "app/users/route.ts", `
export async function GET() {
  return Response.json(await prisma.user.findMany({ select: { id: true, name: true } }));
}`)
	if len(items) != 1 {
		t.Fatalf("a serialized find with select must still be enumerated, got %d", len(items))
	}
	if !items[0].StructuralFacts["field_limiting_detected"] {
		t.Errorf("a find with select must have field_limiting_detected=true, got %+v", items[0].StructuralFacts)
	}
	assertFactsNotJudgments(t, items[0])
}

// omit also counts as field limiting.
func TestOverfetch_OmitCountsAsLimiting(t *testing.T) {
	items := overfetchSurface(t, "app/users/route.ts", `
export async function GET() {
  return Response.json(await prisma.user.findFirst({ omit: { passwordHash: true } }));
}`)
	if len(items) != 1 || !items[0].StructuralFacts["field_limiting_detected"] {
		t.Errorf("omit must set field_limiting_detected=true, got %+v", items)
	}
}

// A find bound to a variable, then serialized — local case (one hop), the select
// is checked on the find.
func TestOverfetch_VariableFromFind(t *testing.T) {
	items := overfetchSurface(t, "app/users/route.ts", `
export async function GET() {
  const users = await prisma.user.findMany();
  return NextResponse.json(users);
}`)
	if len(items) != 1 || items[0].StructuralFacts["field_limiting_detected"] {
		t.Fatalf("a serialized prisma-bound variable with no select is over-fetch, got %+v", items)
	}
	assertFactsNotJudgments(t, items[0])
}

// Frontier: the serialized value comes from a service, not a local find. codefit
// cannot see the find — it enumerates with an honest signal and the agent follows.
func TestOverfetch_ServiceFrontier(t *testing.T) {
	items := overfetchSurface(t, "app/users/route.ts", `
export async function GET() {
  return Response.json(await UserService.getAll());
}`)
	if len(items) != 1 {
		t.Fatalf("a serialized service result must be enumerated as frontier, got %d", len(items))
	}
	if items[0].StructuralFacts["field_limiting_detected"] {
		t.Errorf("frontier: no limiting detected locally → field_limiting_detected=false, got %+v", items[0].StructuralFacts)
	}
	sig := strings.ToLower(signalsJoined(items[0]))
	if !strings.Contains(sig, "not a local") && !strings.Contains(sig, "follow") {
		t.Errorf("frontier signal must be honest about not seeing the find, got %q", sig)
	}
	if strings.Contains(sig, "accesses a resource via a prisma") {
		t.Errorf("no local prisma here — must not claim one: %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// Serializing a plain object / non-domain value is not over-fetch surface.
func TestOverfetch_NonDomainNotEnumerated(t *testing.T) {
	for _, src := range []string{
		`export async function GET() { return Response.json({ ok: true }); }`,
		`export async function GET() { return Response.json({ count: 5 }); }`,
		`export async function GET() { const items = await prisma.x.findMany(); return Response.json(items.map(i => ({ id: i.id }))); }`,
	} {
		if items := overfetchSurface(t, "app/x/route.ts", src); len(items) != 0 {
			t.Errorf("non-domain / field-limited serialization must not enumerate, got %+v for %s", items, src)
		}
	}
}

// Do NOT filter by model name (ADR 0005): a non-"sensitive-looking" model is
// enumerated the same as User.
func TestOverfetch_NotFilteredByModelName(t *testing.T) {
	items := overfetchSurface(t, "app/widgets/route.ts", `
export async function GET() {
  return Response.json(await prisma.widget.findMany());
}`)
	if len(items) != 1 {
		t.Errorf("a non-sensitive-looking model must still be enumerated, got %d", len(items))
	}
}

func TestOverfetch_OnlyNextRouteFiles(t *testing.T) {
	src := `export async function GET() { return Response.json(await prisma.user.findMany()); }`
	for _, path := range []string{"lib/x.ts", "app/x/page.tsx"} {
		if items := overfetchSurface(t, path, src); len(items) != 0 {
			t.Errorf("%s is not a route file; must not enumerate overfetch, got %d", path, len(items))
		}
	}
}
