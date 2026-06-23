package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

func authzSurface(t *testing.T, path, src string) []findings.SurfaceItem {
	t.Helper()
	items, err := typescript.New().AnalyzeSurface(providers.SourceFile{Path: path, Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzeSurface(%s): %v", path, err)
	}
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == "authz" {
			out = append(out, it)
		}
	}
	return out
}

// A handler that touches data (Prisma read) with NO authz in the body is the
// authz finding case — like tasks/process was for IDOR. It must be enumerated,
// regardless of whether it receives an id, and the authz signal must honestly
// report that no known check was detected.
func TestAuthz_SensitiveNoAuthz(t *testing.T) {
	items := authzSurface(t, "app/reports/route.ts", `
export async function GET() {
  const all = await prisma.report.findMany();
  return Response.json(all);
}`)
	if len(items) != 1 {
		t.Fatalf("a data-touching handler with no authz must be enumerated, got %d", len(items))
	}
	sig := strings.ToLower(signalsJoined(items[0]))
	if !strings.Contains(sig, "no known authorization") && !strings.Contains(sig, "no call to a known authorization") {
		t.Errorf("authz signal must honestly report no check detected, got %q", sig)
	}
	if !strings.Contains(sig, "prisma.report.findmany") {
		t.Errorf("operation signal must name the data access, got %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// A mutation (Prisma write) with authz present → enumerated; the operation signal
// says it mutates state, the authz signal reports the check was detected.
func TestAuthz_MutationWithAuthz(t *testing.T) {
	items := authzSurface(t, "app/posts/route.ts", `
export async function POST(req: Request) {
  const session = await getServerSession();
  const body = await req.json();
  return Response.json(await prisma.post.create({ data: body }));
}`)
	if len(items) != 1 {
		t.Fatalf("a mutation handler must be enumerated, got %d", len(items))
	}
	sig := strings.ToLower(signalsJoined(items[0]))
	if !strings.Contains(sig, "mutat") {
		t.Errorf("operation signal must state it mutates state, got %q", sig)
	}
	if !strings.Contains(sig, "getserversession") || !strings.Contains(sig, "was detected") {
		t.Errorf("authz signal must report getServerSession was detected, got %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// Indirect: a sensitive operation through a service (no local Prisma). Enumerated
// with the honest frontier signal — codefit does not chase the service.
func TestAuthz_IndirectViaService(t *testing.T) {
	items := authzSurface(t, "app/billing/route.ts", `
export async function POST(req: Request) {
  const body = await req.json();
  return Response.json(await BillingService.charge(body));
}`)
	if len(items) != 1 {
		t.Fatalf("an indirect sensitive operation must be enumerated, got %d", len(items))
	}
	sig := strings.ToLower(signalsJoined(items[0]))
	if !strings.Contains(sig, "service") && !strings.Contains(sig, "billingservice.charge") {
		t.Errorf("operation signal must honestly name the indirect call, got %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// A handler that neither touches data nor mutates (a health check) is NOT authz
// surface — the filter is "touches data / mutates" (a fact), not "should be
// public" (a judgment).
func TestAuthz_HealthCheckNotEnumerated(t *testing.T) {
	items := authzSurface(t, "app/health/route.ts", `
export async function GET() {
  return Response.json({ ok: true });
}`)
	if len(items) != 0 {
		t.Errorf("a handler that touches no data must not be authz surface, got %+v", items)
	}
}

// Do NOT filter by route name (ADR 0005): a sensitive handler whose path has no
// "admin" segment must still be enumerated.
func TestAuthz_NotFilteredByRouteName(t *testing.T) {
	items := authzSurface(t, "app/public/things/route.ts", `
export async function DELETE(req: Request) {
  const { searchParams } = new URL(req.url);
  await prisma.thing.delete({ where: { id: searchParams.get('id') } });
  return Response.json({ ok: true });
}`)
	if len(items) != 1 {
		t.Errorf("a sensitive handler on a non-admin path must still be enumerated, got %d", len(items))
	}
}

func TestAuthz_OnlyNextRouteFiles(t *testing.T) {
	src := `export async function GET() { return Response.json(await prisma.x.findMany()); }`
	for _, path := range []string{"lib/x.ts", "app/x/page.tsx"} {
		if items := authzSurface(t, path, src); len(items) != 0 {
			t.Errorf("%s is not a route file; must not enumerate authz, got %d", path, len(items))
		}
	}
}
