package mcp_test

import (
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
