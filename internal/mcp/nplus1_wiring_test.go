package mcp_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// item 15 — codefit-scan-all reaches N+1 for free, through the same wiring
// idor/authz/overfetch already use (registration in surfaceQueries() is the
// only thing this change adds at the provider level): a handler with a query
// inside a loop lands in Actionable with an efficiency gap, never silently
// dropped and never resolved_clean (see the load-bearing core test,
// TestAggregateEndpoints_NPlus1OnlyEndpoint_IsActionable_NotResolvedClean).
func TestScanAll_NPlus1FlowsToEndpointBuckets(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "app/reports/route.ts", `
export async function GET() {
  const session = await getServerSession();
  for (const id of ids) {
    await prisma.report.findUnique({ where: { id } });
  }
  return Response.json({ ok: true });
}`)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Actionable.Count != 1 || len(resp.Actionable.Endpoints) != 1 {
		t.Fatalf("the N+1 handler must be one actionable endpoint, got count=%d rendered=%d: %+v",
			resp.Actionable.Count, len(resp.Actionable.Endpoints), resp)
	}
	ep := resp.Actionable.Endpoints[0]
	// The named summary must NAME the nplus1 category and its gap kind — an agent
	// that cannot see the category in the summary cannot decide to fetch it.
	if !slices.Contains(ep.Categories, "nplus1") {
		t.Errorf("the named endpoint must list the nplus1 category, got %+v", ep.Categories)
	}
	if len(ep.Gaps) == 0 {
		t.Errorf("the named endpoint must carry the KIND of gap it has, got %+v", ep)
	}
	var sawNPlus1 bool
	for _, c := range fetchConcerns(t, root, ep.File, ep.Line) {
		if c.Category == "nplus1" {
			sawNPlus1 = true
			if c.Gap == "" {
				t.Errorf("the nplus1 concern must carry a non-empty gap kind, got %+v", c)
			}
		}
	}
	if !sawNPlus1 {
		t.Fatalf("scan-all must reach the nplus1 concern with no new scan-all-level plumbing, got %+v", ep)
	}
	if resp.ResolvedClean.Count != 0 {
		t.Errorf("an endpoint carrying an N+1 gap must never be resolved_clean, got %+v", resp.ResolvedClean)
	}
}

// item 16 — codefit-scan-endpoint (the on-demand single-endpoint tool) reaches
// the N+1 item for a handler containing a query-in-loop, the same standalone
// reachability every other endpoint-surface category already has.
func TestScanEndpoint_NPlus1Reachable(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "app/reports/route.ts", `
export async function GET() {
  for (const id of ids) {
    await prisma.report.findUnique({ where: { id } });
  }
}`)

	resp, err := mcp.HandleScanEndpoint(mcp.ScanEndpointRequest{
		Root: root, Language: "typescript", File: "app/reports/route.ts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Found {
		t.Fatalf("scan-endpoint must find the file's endpoint, got %+v", resp)
	}
	var sawNPlus1 bool
	for _, ep := range resp.Endpoints {
		for _, c := range ep.Concerns {
			if c.Category == "nplus1" {
				sawNPlus1 = true
			}
		}
	}
	if !sawNPlus1 {
		t.Fatalf("scan-endpoint must return the nplus1 concern for this handler, got %+v", resp.Endpoints)
	}
}

// item 18 — locked negative test (design §8's explicit decision): N+1 does
// NOT echo in scan-all's DB section. The DB section is a pure function of the
// parsed SCHEMA (dbsensor.Audit over db.Schema); it never merges in the TS
// security-sensor's surface (which is where N+1 lives). Guards against future
// drift toward silently duplicating the item list in two report sections.
func TestScanAllDBSection_DoesNotEchoNPlus1(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "app/reports/route.ts", `
export async function GET() {
  for (const id of ids) {
    await prisma.report.findUnique({ where: { id } });
  }
}`)
	if err := os.MkdirAll(filepath.Join(root, "prisma"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prisma", "schema.prisma"), []byte(dbSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(dbYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("the DB section must be measured for this fixture, got %+v", resp.DB)
	}
	for _, it := range resp.DB.Surface {
		if it.Category == "nplus1" {
			t.Errorf("N+1 must NEVER appear in scan-all's DB section (endpoint buckets only), got %+v", it)
		}
	}
	for _, f := range resp.DB.Findings {
		if f.ID == "NPLUS1" || f.Dimension == "nplus1" {
			t.Errorf("N+1 must NEVER appear in scan-all's DB section, got %+v", f)
		}
	}
	// The N+1 item is confirmed present in the endpoint buckets (not silently
	// dropped) — the OTHER half of the "endpoint buckets only" contract.
	var sawInActionable bool
	for _, ep := range resp.Actionable.Endpoints {
		if slices.Contains(ep.Categories, "nplus1") {
			sawInActionable = true
		}
	}
	if !sawInActionable {
		t.Fatalf("N+1 must appear in the endpoint buckets even when a DB section is also present, got %+v", resp.Actionable)
	}
}
