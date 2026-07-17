package mcp_test

import (
	"encoding/json"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/mcp"
)

const idorRoute = `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  const u = await prisma.user.findUnique({ where: { id: params.id } });
  return Response.json(u);
}`

// codefit-surface-idor: enumerates the IDOR surface and returns it in the §11
// JSON contract (id, category, file, line, snippet, structural_signals,
// reason_to_review).
func TestHandleSurfaceIDOR(t *testing.T) {
	resp, err := mcp.HandleSurfaceIDOR(mcp.SurfaceIDORRequest{
		Files: []mcp.FileInput{
			{Path: "app/users/[id]/route.ts", Content: idorRoute},
			{Path: "lib/helpers.ts", Content: idorRoute}, // not a route file → ignored
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Surface) != 1 {
		t.Fatalf("want 1 IDOR surface item (only the route file), got %d", len(resp.Surface))
	}
	it := resp.Surface[0]
	if it.ID == "" || it.Category != "idor" || len(it.StructuralSignals) == 0 {
		t.Errorf("surface item incomplete: %+v", it)
	}

	// JSON contract keys.
	data, _ := json.Marshal(resp)
	var m struct {
		Surface []map[string]any `json:"surface"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "category", "file", "line", "snippet", "structural_signals", "reason_to_review"} {
		if _, ok := m.Surface[0][key]; !ok {
			t.Errorf("surface JSON missing key %q: %s", key, data)
		}
	}
}

// codefit-surface-authz: enumerates the broken-authorization surface (handlers
// doing something sensitive) in the same §11 JSON contract.
func TestHandleSurfaceAuthz(t *testing.T) {
	resp, err := mcp.HandleSurfaceAuthz(mcp.SurfaceIDORRequest{
		Files: []mcp.FileInput{
			{Path: "app/reports/route.ts", Content: `
export async function GET() { return Response.json(await prisma.report.findMany()); }`},
			{Path: "app/health/route.ts", Content: `
export async function GET() { return Response.json({ ok: true }); }`}, // not sensitive
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Surface) != 1 {
		t.Fatalf("want 1 authz item (only the data-touching handler), got %d", len(resp.Surface))
	}
	if resp.Surface[0].Category != "authz" || resp.Surface[0].ID == "" {
		t.Errorf("authz surface item incomplete: %+v", resp.Surface[0])
	}
}

// codefit-surface-authz orders the actionable items (no known authz detected)
// FIRST, without reducing the list — the complete enumeration is preserved, only
// reordered, so the findings surface to the top instead of being buried.
func TestHandleSurfaceAuthzOrdersUncheckedFirst(t *testing.T) {
	resp, err := mcp.HandleSurfaceAuthz(mcp.SurfaceIDORRequest{
		Files: []mcp.FileInput{
			{Path: "app/checked/route.ts", Content: `
export async function GET() {
  const s = await getServerSession();
  return Response.json(await prisma.a.findMany());
}`},
			{Path: "app/unchecked/route.ts", Content: `
export async function GET() { return Response.json(await prisma.b.findMany()); }`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Surface) != 2 {
		t.Fatalf("ordering must not drop items; want 2, got %d", len(resp.Surface))
	}
	if resp.Surface[0].StructuralFacts["known_authz_detected"] {
		t.Errorf("the unchecked handler (no known authz) must be ordered first, got %s first",
			resp.Surface[0].File)
	}
	if !resp.Surface[1].StructuralFacts["known_authz_detected"] {
		t.Errorf("the checked handler must be ordered after the unchecked one")
	}
}

// codefit-surface-overfetch orders by STRUCTURAL CERTAINTY, without filtering:
//
//	1st  local find, no select  (structurally confirmed over-fetch — actionable)
//	2nd  local find, with select (limited)
//	3rd  frontier (service — codefit cannot see the find; honest, kept last)
//
// All items are preserved; only ordered by what codefit can assert with most
// certainty. This is the unchecked-first principle applied to the local/frontier
// axis (ADR 0005) — never severity.
func TestHandleSurfaceOverfetchOrdersByCertainty(t *testing.T) {
	resp, err := mcp.HandleSurfaceOverfetch(mcp.SurfaceIDORRequest{
		Files: []mcp.FileInput{
			{Path: "app/frontier/route.ts", Content: `
export async function GET() { return Response.json(await ThingService.getAll()); }`},
			{Path: "app/limited/route.ts", Content: `
export async function GET() { return Response.json(await prisma.a.findMany({ select: { id: true } })); }`},
			{Path: "app/confirmed/route.ts", Content: `
export async function GET() { return Response.json(await prisma.b.findMany()); }`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Surface) != 3 {
		t.Fatalf("ordering must not drop items; want 3, got %d", len(resp.Surface))
	}
	// 1st: confirmed (local, no select).
	if !resp.Surface[0].StructuralFacts["local_access_detected"] || resp.Surface[0].StructuralFacts["field_limiting_detected"] {
		t.Errorf("position 0 must be the confirmed over-fetch (local, no select), got %s", resp.Surface[0].File)
	}
	// 2nd: local with select.
	if !resp.Surface[1].StructuralFacts["local_access_detected"] || !resp.Surface[1].StructuralFacts["field_limiting_detected"] {
		t.Errorf("position 1 must be the limited local find, got %s", resp.Surface[1].File)
	}
	// 3rd: frontier (kept, not filtered).
	if resp.Surface[2].StructuralFacts["local_access_detected"] {
		t.Errorf("position 2 must be the frontier (local_access_detected=false), got %s", resp.Surface[2].File)
	}
}

// End-to-end: enumerate → the agent confirms the item by its id → codefit
// integrates the verdict into a probabilistic, anchored finding.
func TestSurfaceConfirmRoundTrip(t *testing.T) {
	enum, err := mcp.HandleSurfaceIDOR(mcp.SurfaceIDORRequest{
		Files: []mcp.FileInput{{Path: "app/users/[id]/route.ts", Content: idorRoute}},
	})
	if err != nil {
		t.Fatal(err)
	}
	it := enum.Surface[0]

	conf := mcp.HandleConfirmSurface(mcp.ConfirmSurfaceRequest{
		Confirmations: []surface.Confirmation{{
			SurfaceID:  it.ID, // the id codefit handed out — recomputed and validated
			Category:   it.Category,
			File:       it.File,
			Line:       it.Line,
			Verdict:    surface.VerdictVulnerable,
			Reasoning:  "params.id flows into the where clause with no ownership check",
			Confidence: 0.85,
			Severity:   "high",
		}},
	})
	if len(conf.Findings) != 1 {
		t.Fatalf("a confirmed-vulnerable item must produce 1 finding, got %d", len(conf.Findings))
	}
	f := conf.Findings[0]
	if !f.Probabilistic || f.Confidence >= 1.0 {
		t.Errorf("must be probabilistic with confidence < 1.0, got %+v", f)
	}
	if f.File != it.File || f.Line != it.Line {
		t.Errorf("finding must anchor to the surface location, got %s:%d", f.File, f.Line)
	}
}

// A verdict whose id does not recompute is rejected, never integrated.
func TestConfirmSurfaceRejectsBadID(t *testing.T) {
	resp := mcp.HandleConfirmSurface(mcp.ConfirmSurfaceRequest{
		Confirmations: []surface.Confirmation{{
			SurfaceID: "tampered", Category: "idor", File: "app/x/route.ts", Line: 2,
			Verdict: surface.VerdictVulnerable, Confidence: 0.9,
		}},
	})
	if len(resp.Findings) != 0 {
		t.Errorf("a tampered id must not become a finding, got %d", len(resp.Findings))
	}
	if len(resp.Invalid) != 1 {
		t.Errorf("the tampered confirmation must be reported as invalid, got %d", len(resp.Invalid))
	}
}

// codefit-surface-nplus1 orders by STRUCTURAL CERTAINTY, without filtering
// (ADR 0005): local+awaited(+nested) ranks first, local+Promise.all-wrapped
// (concurrent) next, the cross-function frontier ranked LAST — honest, never
// dropped. Mirrors codefit-surface-overfetch's ordering discipline.
func TestHandleSurfaceNPlus1OrdersByCertainty(t *testing.T) {
	resp, err := mcp.HandleSurfaceNPlus1(mcp.SurfaceIDORRequest{
		Files: []mcp.FileInput{
			{Path: "app/frontier/route.ts", Content: `
export async function GET() {
  for (const id of ids) { await userService.findById(id); }
}`},
			{Path: "app/concurrent/route.ts", Content: `
export async function GET() {
  await Promise.all(ids.map(id => prisma.a.findUnique({ where: { id } })));
}`},
			{Path: "app/sequential/route.ts", Content: `
export async function GET() {
  for (const id of ids) { await prisma.b.findUnique({ where: { id } }); }
}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Surface) != 3 {
		t.Fatalf("ordering must not drop items; want 3, got %d", len(resp.Surface))
	}
	// 1st: local + sequential await — the most certain, worst shape.
	if !resp.Surface[0].StructuralFacts["local_access_detected"] || !resp.Surface[0].StructuralFacts["awaited_in_loop"] {
		t.Errorf("position 0 must be the local sequentially-awaited query, got %+v", resp.Surface[0])
	}
	// 2nd: local + concurrent (Promise.all).
	if !resp.Surface[1].StructuralFacts["local_access_detected"] || !resp.Surface[1].StructuralFacts["promise_all_wrapped"] {
		t.Errorf("position 1 must be the local Promise.all-wrapped query, got %+v", resp.Surface[1])
	}
	// 3rd: frontier — kept, not filtered, ranked last.
	if resp.Surface[2].StructuralFacts["local_access_detected"] {
		t.Errorf("position 2 must be the frontier (local_access_detected=false), got %+v", resp.Surface[2])
	}

	// JSON contract keys (§11 surface contract).
	data, _ := json.Marshal(resp)
	var m struct {
		Surface []map[string]any `json:"surface"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "category", "file", "line", "snippet", "structural_signals", "reason_to_review"} {
		if _, ok := m.Surface[0][key]; !ok {
			t.Errorf("surface JSON missing key %q: %s", key, data)
		}
	}
}
