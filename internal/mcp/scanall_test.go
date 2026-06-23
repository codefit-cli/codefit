package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// codefit-scan-all runs the deterministic sensor and the surface queries and
// returns the per-endpoint synthesis: one handler's deterministic finding and its
// surface concerns grouped together, ordered deterministic → confirmed → frontier,
// with the affirm/ask distinction preserved.
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
	if len(resp.Endpoints) != 1 {
		t.Fatalf("the handler's concerns must group into ONE endpoint, got %d", len(resp.Endpoints))
	}
	ep := resp.Endpoints[0]
	if len(ep.Concerns) < 2 {
		t.Fatalf("endpoint should carry the deterministic SQL finding AND surface concerns, got %d", len(ep.Concerns))
	}
	// First concern is the deterministic one — it AFFIRMS (codefit asserts).
	if ep.Concerns[0].Certainty != report.Deterministic || !ep.Concerns[0].Affirms {
		t.Errorf("first concern must be the affirmed deterministic finding, got %+v", ep.Concerns[0])
	}
	if ep.Concerns[0].Confidence != 1.0 {
		t.Errorf("deterministic concern must have confidence 1.0, got %v", ep.Concerns[0].Confidence)
	}
	// At least one surface concern, which ASKS (does not affirm).
	sawAsk := false
	for _, c := range ep.Concerns[1:] {
		if c.Source == "surface" && !c.Affirms {
			sawAsk = true
		}
	}
	if !sawAsk {
		t.Error("the endpoint must carry surface concerns that ask (not affirm)")
	}
	if resp.Summary.Endpoints != 1 || resp.Summary.DeterministicFindings < 1 {
		t.Errorf("summary should count the endpoint and the deterministic finding, got %+v", resp.Summary)
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
