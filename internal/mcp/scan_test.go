package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// codefit-scan-security: runs the deterministic + surface analysis over a project
// and returns the flat findings and surface (the §11 contract).
func TestHandleScanSecurity(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "app/x/route.ts", `
export async function GET(req: Request) {
  const { searchParams } = new URL(req.url);
  return Response.json(await prisma.thing.findMany({ where: { id: searchParams.get('id') } }));
}`)
	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Surface) == 0 {
		t.Error("a route with an id→resource handler should map surface")
	}
}

// codefit-coverage: returns the coverage manifest for the language.
func TestHandleCoverage(t *testing.T) {
	resp, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Manifest.Language != "typescript" || len(resp.Manifest.Deterministic) == 0 {
		t.Errorf("coverage manifest incomplete: %+v", resp.Manifest)
	}
	if _, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "cobol"}); err == nil {
		t.Error("unsupported language should error")
	}
}

// TestHandleCoverage_ServesDeliveredElsewhere: the manifest's THIRD answer has to
// reach the AGENT, not just the source file. A manifest entry that no tool
// surfaces is a comment, not a capability — and this bucket exists precisely
// because an agent that searches for DB-201, finds nothing and concludes N+1 is
// uncovered is wrong.
//
// Asserted at the two levels the agent actually meets: the handler's response
// (what codefit-coverage returns) and the JSON the MCP layer serializes it to
// (what the agent reads). The JSON assertion is not redundant — an unexported
// field, or a `json:"-"`, would satisfy the first check and vanish from the
// second.
func TestHandleCoverage_ServesDeliveredElsewhere(t *testing.T) {
	resp, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Manifest.DeliveredElsewhere) == 0 {
		t.Fatal("codefit-coverage returned an EMPTY DeliveredElsewhere — the DB dimension records DB-201 there, " +
			"so the composition dropped it and the agent never sees the answer")
	}
	if !strings.Contains(strings.Join(resp.Manifest.DeliveredElsewhere, "\n"), "DB-201") {
		t.Errorf("DeliveredElsewhere does not name DB-201: %v", resp.Manifest.DeliveredElsewhere)
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "DeliveredElsewhere") || !strings.Contains(string(raw), "DB-201") {
		t.Errorf("the serialized coverage response does not carry DeliveredElsewhere/DB-201 — the agent reads "+
			"this JSON, not the struct:\n%s", raw)
	}
}
