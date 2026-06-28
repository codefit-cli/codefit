package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// codefit-scan-all returns an ACTIONABLE summary, not the raw item dump: the
// endpoints codefit resolved locally (≥1 deterministic or surface_confirmed
// concern) go WHOLE into Actionable; the frontier-only endpoints are NAMED in
// FrontierPending and fetched on demand. This keeps the response small enough not
// to truncate (ADR 0008).
func TestHandleScanAll(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "app/things/route.ts", `
export async function GET(req: Request) {
  const { searchParams } = new URL(req.url);
  db.query("SELECT * FROM t WHERE x = " + searchParams.get('x'));
  return Response.json(await prisma.thing.findMany());
}`)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	// A locally-resolved endpoint (deterministic SQL finding + local prisma access)
	// is actionable and carries ALL its concerns together.
	if len(resp.Actionable) != 1 {
		t.Fatalf("the resolved handler must be ONE actionable endpoint, got %d", len(resp.Actionable))
	}
	ep := resp.Actionable[0]
	if len(ep.Concerns) < 2 {
		t.Fatalf("actionable endpoint should carry the deterministic finding AND surface concerns, got %d", len(ep.Concerns))
	}
	if ep.Concerns[0].Certainty != report.Deterministic || !ep.Concerns[0].Affirms {
		t.Errorf("first concern must be the affirmed deterministic finding, got %+v", ep.Concerns[0])
	}
	if ep.Concerns[0].Confidence != 1.0 {
		t.Errorf("deterministic concern must have confidence 1.0, got %v", ep.Concerns[0].Confidence)
	}
	if resp.Summary.Endpoints != 1 || resp.Summary.DeterministicFindings < 1 {
		t.Errorf("summary should count the endpoint and the deterministic finding, got %+v", resp.Summary)
	}
}

// A project mixing a resolved endpoint and a frontier-only endpoint: the resolved
// one is detailed in Actionable; the frontier-only one is only NAMED in
// FrontierPending, with a Note pointing at codefit-scan-endpoint.
func TestHandleScanAll_FrontierNamedNotDetailed(t *testing.T) {
	root := t.TempDir()
	// Resolved: local prisma access, no authz → surface_confirmed, actionable.
	mustWrite(t, root, "app/tasks/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.task.findUnique({ where: { id: params.id } }));
}`)
	// Frontier-only: the operation leaves the handler body via a service call.
	mustWrite(t, root, "app/admin/audit/route.ts", `
export async function POST(req: Request) {
  const body = await req.json();
  return Response.json(await AuditService.record(body));
}`)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Actionable) != 1 || !strings.Contains(resp.Actionable[0].File, "app/tasks/") {
		t.Fatalf("the resolved tasks endpoint must be the only actionable one, got %+v", resp.Actionable)
	}
	if resp.FrontierPending.Count != 1 || len(resp.FrontierPending.Endpoints) != 1 {
		t.Fatalf("the frontier-only endpoint must be named once in frontier_pending, got %+v", resp.FrontierPending)
	}
	fe := resp.FrontierPending.Endpoints[0]
	if !strings.Contains(fe.File, "app/admin/audit/") {
		t.Errorf("frontier_pending must name the admin/audit file, got %q", fe.File)
	}
	if len(fe.Categories) == 0 {
		t.Errorf("the frontier entry must name its categories, got %+v", fe)
	}
	if !strings.Contains(resp.FrontierPending.Note, "codefit-scan-endpoint") {
		t.Errorf("the note must tell the agent how to fetch detail, got %q", resp.FrontierPending.Note)
	}

	// The response must be SMALL: the frontier entry carries no concern detail
	// (signals/reason), so a project with many frontier endpoints does not bloat
	// the response. Assert the named entry has no embedded concern detail.
	data, _ := json.Marshal(resp.FrontierPending.Endpoints[0])
	if strings.Contains(string(data), "structural_signals") || strings.Contains(string(data), "reason_to_review") {
		t.Errorf("frontier entries must be NAMED, not detailed, got %s", data)
	}
}

// An endpoint codefit resolved locally and found clean (authz present + select
// present → no gap) goes to resolved_clean: named with a verification fact, NOT in
// actionable and NOT in frontier. The verification fact affirms what codefit
// checked — distinct from a frontier "could not conclude".
func TestHandleScanAll_ResolvedCleanBucket(t *testing.T) {
	root := t.TempDir()
	// A sensitive handler with NO client identifier (so no IDOR/ownership question),
	// authz present, and a field-limited serialization → both the authz and the
	// over-fetch controls are answered, no gap → resolved_clean. An IDOR endpoint
	// could not be resolved_clean: ownership is unverifiable from structure.
	mustWrite(t, root, "app/stats/route.ts", `
export async function GET() {
  const session = await getServerSession();
  return Response.json(await prisma.stats.findMany({ select: { count: true } }));
}`)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Actionable) != 0 {
		t.Fatalf("a fully-checked endpoint must not be actionable, got %+v", resp.Actionable)
	}
	if resp.FrontierPending.Count != 0 {
		t.Fatalf("a locally-resolved endpoint must not be frontier, got %+v", resp.FrontierPending)
	}
	if resp.ResolvedClean.Count != 1 || len(resp.ResolvedClean.Endpoints) != 1 {
		t.Fatalf("the checked-clean endpoint must be in resolved_clean, got %+v", resp.ResolvedClean)
	}
	v := strings.ToLower(resp.ResolvedClean.Endpoints[0].Verification)
	if !strings.Contains(v, "verified") || !strings.Contains(v, "no gap") {
		t.Errorf("resolved_clean entry must carry a verification fact, got %q", resp.ResolvedClean.Endpoints[0].Verification)
	}
	if !strings.Contains(strings.ToLower(resp.ResolvedClean.Note), "positive check") {
		t.Errorf("resolved_clean note must frame it as a positive check, not an absence, got %q", resp.ResolvedClean.Note)
	}
	// Named, not detailed: no embedded concern signals on the entry.
	data, _ := json.Marshal(resp.ResolvedClean.Endpoints[0])
	if strings.Contains(string(data), "structural_signals") {
		t.Errorf("resolved_clean entries must be named, not detailed, got %s", data)
	}
}

// All-frontier project: codefit concluded nothing locally. Actionable is empty and
// every endpoint is in frontier_pending. The Note must communicate this as "could
// not conclude locally", NOT as a clean/empty result — same principle as the
// frontier signal wording: absence of actionables is not "clean".
func TestHandleScanAll_AllFrontier(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "app/a/route.ts", `
export async function POST(req: Request) {
  const body = await req.json();
  return Response.json(await AService.do(body));
}`)
	mustWrite(t, root, "app/b/route.ts", `
export async function POST(req: Request) {
  const body = await req.json();
  return Response.json(await BService.run(body));
}`)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Actionable) != 0 {
		t.Fatalf("an all-frontier project must have no actionable endpoints, got %d", len(resp.Actionable))
	}
	if resp.FrontierPending.Count != 2 {
		t.Fatalf("both endpoints must be named as pending, got %d", resp.FrontierPending.Count)
	}
	note := strings.ToLower(resp.FrontierPending.Note)
	// Must say codefit concluded nothing locally — and must NOT read as "clean".
	if !strings.Contains(note, "nothing locally") && !strings.Contains(note, "concluded nothing") {
		t.Errorf("all-frontier note must say codefit concluded nothing locally, got %q", resp.FrontierPending.Note)
	}
	if !strings.Contains(note, "not a clean") {
		t.Errorf("all-frontier note must state this is NOT a clean result, got %q", resp.FrontierPending.Note)
	}
}

// codefit-scan-endpoint re-analyses one file on demand and returns its endpoints'
// FULL concerns. Stateless: it re-runs the static analysis, it does not retrieve
// anything stored. A frontier-only file (not in scan-all's actionable set) yields
// its complete detail here.
func TestHandleScanEndpoint_FrontierFileDetail(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "app/admin/audit/route.ts", `
export async function POST(req: Request) {
  const body = await req.json();
  return Response.json(await AuditService.record(body));
}`)

	resp, err := mcp.HandleScanEndpoint(mcp.ScanEndpointRequest{
		Root: root, Language: "typescript", File: "app/admin/audit/route.ts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Found || len(resp.Endpoints) == 0 {
		t.Fatalf("scan-endpoint must return the file's endpoints, got %+v", resp)
	}
	// Full concern detail is present (signals + reason), unlike the named frontier entry.
	var sawSignals bool
	for _, ep := range resp.Endpoints {
		for _, c := range ep.Concerns {
			if len(c.Signals) > 0 && c.Question != "" {
				sawSignals = true
			}
		}
	}
	if !sawSignals {
		t.Errorf("scan-endpoint must return full concern detail (signals + reason), got %+v", resp.Endpoints)
	}
}

// scan-endpoint on a file with nothing to audit returns found=false honestly, not
// an error and not a fabricated concern.
func TestHandleScanEndpoint_NotFound(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "app/health/route.ts", `
export async function GET() {
  return Response.json({ ok: true });
}`)

	resp, err := mcp.HandleScanEndpoint(mcp.ScanEndpointRequest{
		Root: root, Language: "typescript", File: "app/health/route.ts",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Found || len(resp.Endpoints) != 0 {
		t.Errorf("a file with no auditable concerns must report found=false, got %+v", resp)
	}
}

func mustWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
