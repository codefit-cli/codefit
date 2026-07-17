package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// nplus1Surface runs AnalyzeSurface and filters to the "nplus1" category —
// same helper shape as idorSurface/overfetchSurface.
func nplus1Surface(t *testing.T, path, src string) []findings.SurfaceItem {
	t.Helper()
	items, err := typescript.New().AnalyzeSurface(providers.SourceFile{Path: path, Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzeSurface(%s): %v", path, err)
	}
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == "nplus1" {
			out = append(out, it)
		}
	}
	return out
}

// item 1: a Prisma call inside a for...of loop is enumerated, local, with the
// loop kind named in the signals.
func TestNPlus1_ForOfLoop_Detected(t *testing.T) {
	items := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  for (const id of ids) {
    await prisma.user.findUnique({ where: { id } });
  }
}`)
	if len(items) != 1 {
		t.Fatalf("a query inside a for...of loop must be enumerated, got %d: %+v", len(items), items)
	}
	it := items[0]
	if !it.StructuralFacts["local_access_detected"] {
		t.Errorf("a local Prisma call must set local_access_detected=true, got %+v", it.StructuralFacts)
	}
	if !it.StructuralFacts["awaited_in_loop"] {
		t.Errorf("an awaited call directly in the loop body must set awaited_in_loop=true, got %+v", it.StructuralFacts)
	}
	if !strings.Contains(strings.ToLower(signalsJoined(it)), "for...of") {
		t.Errorf("signals must name the loop kind (for...of), got %q", signalsJoined(it))
	}
	assertFactsNotJudgments(t, it)
}

// item 2: .forEach and .map callback iteration are detected, each with its own
// loop-kind signal.
func TestNPlus1_ForEachAndMap_Detected(t *testing.T) {
	forEach := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  ids.forEach(id => prisma.user.findUnique({ where: { id } }));
}`)
	if len(forEach) != 1 {
		t.Fatalf("a query inside .forEach must be enumerated, got %d", len(forEach))
	}
	if !strings.Contains(signalsJoined(forEach[0]), ".forEach(") {
		t.Errorf("signals must name .forEach as the loop kind, got %q", signalsJoined(forEach[0]))
	}

	mapCall := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  ids.map(id => prisma.user.findUnique({ where: { id } }));
}`)
	if len(mapCall) != 1 {
		t.Fatalf("a query inside .map must be enumerated, got %d", len(mapCall))
	}
	if !strings.Contains(signalsJoined(mapCall[0]), ".map(") {
		t.Errorf("signals must name .map as the loop kind, got %q", signalsJoined(mapCall[0]))
	}
}

// item 3: while and do...while variants are detected.
func TestNPlus1_WhileAndDoWhile_Detected(t *testing.T) {
	whileLoop := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  let i = 0;
  while (i < ids.length) {
    await prisma.user.findUnique({ where: { id: ids[i] } });
    i++;
  }
}`)
	if len(whileLoop) != 1 {
		t.Fatalf("a query inside a while loop must be enumerated, got %d", len(whileLoop))
	}
	if !strings.Contains(signalsJoined(whileLoop[0]), "while") {
		t.Errorf("signals must name while as the loop kind, got %q", signalsJoined(whileLoop[0]))
	}

	doWhile := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  let i = 0;
  do {
    await prisma.user.findUnique({ where: { id: ids[i] } });
    i++;
  } while (i < ids.length);
}`)
	if len(doWhile) != 1 {
		t.Fatalf("a query inside a do...while loop must be enumerated, got %d", len(doWhile))
	}
	if !strings.Contains(signalsJoined(doWhile[0]), "do...while") {
		t.Errorf("signals must name do...while as the loop kind, got %q", signalsJoined(doWhile[0]))
	}
}

// item 4: ADR 0005 — a loop over a literal array is STILL enumerated, never
// filtered by shape; its signals name the literal array source and count so
// the agent dismisses it at a glance.
func TestNPlus1_LiteralArrayLoop_StillEnumerated(t *testing.T) {
	items := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  [1,2,3].forEach(id => prisma.user.findUnique({ where: { id } }));
}`)
	if len(items) != 1 {
		t.Fatalf("a loop over a literal array must still be enumerated (ADR 0005), got %d", len(items))
	}
	sig := signalsJoined(items[0])
	if !strings.Contains(sig, "literal array") || !strings.Contains(sig, "3") {
		t.Errorf("signals must name the literal-array source with its element count, got %q", sig)
	}
}

// item 5: a loop calling a service/repository function (no local Prisma call)
// is still enumerated — the cross-function frontier, never silently dropped —
// with local_access_detected=false and the callee named.
func TestNPlus1_ServiceCallFrontier_Declared(t *testing.T) {
	items := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  for (const id of ids) {
    await userService.findById(id);
  }
}`)
	if len(items) != 1 {
		t.Fatalf("a service call inside a loop must be enumerated (the frontier), got %d", len(items))
	}
	it := items[0]
	if it.StructuralFacts["local_access_detected"] {
		t.Errorf("a non-Prisma call must set local_access_detected=false, got %+v", it.StructuralFacts)
	}
	if !strings.Contains(signalsJoined(it), "userService.findById") {
		t.Errorf("signals must name the frontier callee, got %q", signalsJoined(it))
	}
}

// item 6: the boundary — every QUERY inside a loop, not every loop. A loop
// with no call at all inside it produces zero items.
func TestNPlus1_NoQueryInLoop_NotEnumerated(t *testing.T) {
	items := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  let total = 0;
  for (const id of ids) {
    total += id;
  }
  return Response.json({ total });
}`)
	if len(items) != 0 {
		t.Fatalf("a loop with no query call must produce zero items, got %d: %+v", len(items), items)
	}
}

// item 7: two Prisma call sites in the same loop are two distinct items
// (StableID is (file, line, category) — never merged).
func TestNPlus1_TwoQueriesSameLoop_TwoDistinctItems(t *testing.T) {
	items := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  for (const id of ids) {
    await prisma.user.findUnique({ where: { id } });
    await prisma.profile.findUnique({ where: { userId: id } });
  }
}`)
	if len(items) != 2 {
		t.Fatalf("two query call sites in the same loop must be two distinct items, got %d: %+v", len(items), items)
	}
	if items[0].Line == items[1].Line {
		t.Errorf("the two items must anchor at their own distinct lines, got %d and %d", items[0].Line, items[1].Line)
	}
}

// item 8: a query nested under two enclosing loops is exactly ONE item — no
// per-nesting-level duplication — with nested_loop=true.
func TestNPlus1_NestedLoop_OneItemPerQuery(t *testing.T) {
	items := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  for (const id of ids) {
    for (const sub of subIds) {
      await prisma.user.findUnique({ where: { id: sub } });
    }
  }
}`)
	if len(items) != 1 {
		t.Fatalf("a query under two nested loops must be exactly one item, got %d: %+v", len(items), items)
	}
	if !items[0].StructuralFacts["nested_loop"] {
		t.Errorf("a query under a nested loop must set nested_loop=true, got %+v", items[0].StructuralFacts)
	}
}

// item 9: a Promise.all(xs.map(...)) shape is still enumerated (still N
// distinct round-trips); promise_all_wrapped=true and awaited_in_loop reflects
// the concurrent (not sequential-await) shape.
func TestNPlus1_PromiseAllWrapped_StillEnumerated(t *testing.T) {
	items := nplus1Surface(t, "app/users/route.ts", `
export async function GET() {
  await Promise.all(ids.map(id => prisma.user.findUnique({ where: { id } })));
}`)
	if len(items) != 1 {
		t.Fatalf("a Promise.all-wrapped map query must still be enumerated, got %d: %+v", len(items), items)
	}
	it := items[0]
	if !it.StructuralFacts["promise_all_wrapped"] {
		t.Errorf("promise_all_wrapped must be true, got %+v", it.StructuralFacts)
	}
	if it.StructuralFacts["awaited_in_loop"] {
		t.Errorf("a concurrent Promise.all query is not the sequential-await shape, got %+v", it.StructuralFacts)
	}
}

// item 10: a loop outside an audited handler (module scope / a seed script —
// no route handler, Server Action, Express/Fastify/NestJS registration, or
// "use server" marker) is not enumerated — auditTargets gating, the same
// boundary idor/authz/overfetch already share.
func TestNPlus1_OutsideAuditedHandler_NotEnumerated(t *testing.T) {
	items := nplus1Surface(t, "scripts/seed.ts", `
for (const id of ids) {
  prisma.user.findUnique({ where: { id } });
}`)
	if len(items) != 0 {
		t.Fatalf("a loop outside an audited handler must not be enumerated, got %d: %+v", len(items), items)
	}
}

// item 14 — the anchor invariant (test-locked per design §8): every handler
// that emits an N+1 item also emits an authz anchor, because authz's own
// collectPrismaAccesses/collectAppCalls walk the WHOLE body (including inside
// loops) — the same Prisma/service call that produces the N+1 item also makes
// the handler "sensitive" for authz. This is asserted end-to-end via
// AggregateEndpoints, over every fixture shape used above (local, frontier,
// nested, Promise.all). If a real case is found where this does NOT hold, the
// fix belongs at the anchor level (authz), never by dropping the N+1 item.
func TestAnchorInvariant_EveryNPlus1HandlerHasAuthzAnchor(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"local for...of", `
export async function GET() {
  for (const id of ids) {
    await prisma.user.findUnique({ where: { id } });
  }
}`},
		{"frontier service call", `
export async function GET() {
  for (const id of ids) {
    await userService.findById(id);
  }
}`},
		{"nested loop", `
export async function GET() {
  for (const id of ids) {
    for (const sub of subIds) {
      await prisma.user.findUnique({ where: { id: sub } });
    }
  }
}`},
		{"promise.all wrapped", `
export async function GET() {
  await Promise.all(ids.map(id => prisma.user.findUnique({ where: { id } })));
}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items, err := typescript.New().AnalyzeSurface(providers.SourceFile{Path: "app/x/route.ts", Content: []byte(c.src)})
			if err != nil {
				t.Fatalf("AnalyzeSurface: %v", err)
			}
			hasNPlus1 := false
			for _, it := range items {
				if it.Category == "nplus1" {
					hasNPlus1 = true
				}
			}
			if !hasNPlus1 {
				t.Fatalf("fixture %q produced no nplus1 item; nothing to anchor-check", c.name)
			}
			eps := report.AggregateEndpoints(nil, items)
			anchoredAtHandler := false
			for _, ep := range eps {
				if ep.Line == 0 {
					continue // module-scope bin — not a real handler anchor
				}
				hasAuthz, hasNPlus1Concern := false, false
				for _, concern := range ep.Concerns {
					if concern.Category == "authz" {
						hasAuthz = true
					}
					if concern.Category == "nplus1" {
						hasNPlus1Concern = true
					}
				}
				if hasNPlus1Concern {
					if !hasAuthz {
						t.Errorf("endpoint at line %d carries an nplus1 concern with NO authz anchor — fix belongs at the anchor level, never by dropping the N+1 item: %+v", ep.Line, ep)
					}
					anchoredAtHandler = true
				}
			}
			if !anchoredAtHandler {
				t.Errorf("fixture %q: the nplus1 item never landed on a real handler anchor (line>0), got endpoints: %+v", c.name, eps)
			}
		})
	}
}
