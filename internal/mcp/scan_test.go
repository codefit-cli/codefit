package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/coverage"
	"github.com/codefit-cli/codefit/internal/mcp"
	"github.com/codefit-cli/codefit/internal/providers/registry"
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
	if resp.Manifest.Language != "typescript" || len(resp.Manifest.DeterministicProse) == 0 {
		t.Errorf("coverage manifest incomplete: %+v", resp.Manifest)
	}
	if resp.Derived {
		t.Error("typescript has a hand-written prose manifest — Derived must be false")
	}
	if _, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "cobol"}); err == nil {
		t.Error("unsupported language should error")
	}
}

// TestHandleCoverage_DerivedForLanguageWithNoProseManifest is R1: a
// registered, exposed language with no hand-written CoverageManifest() (go
// today) must still get a truthful, DERIVED answer from its Capability() —
// never the old "no coverage manifest for language" error. It must name the
// declared security rule ids and the surface categories it does NOT map,
// derived from surface.ProviderCategories, not a literal.
func TestHandleCoverage_DerivedForLanguageWithNoProseManifest(t *testing.T) {
	resp, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "go"})
	if err != nil {
		t.Fatalf("codefit-coverage must answer for a registered language with no prose manifest, got error: %v", err)
	}
	if !resp.Derived {
		t.Error("go has no CoverageManifest() — Derived must be true")
	}
	if resp.Manifest.Language != "go" {
		t.Errorf("Manifest.Language = %q, want %q", resp.Manifest.Language, "go")
	}
	if len(resp.Manifest.DeterministicProse) == 0 {
		t.Fatal("Deterministic must name go's declared security rule ids, got none")
	}
	joinedNotCovered := strings.Join(resp.Manifest.NotCoveredProse, "\n")
	for _, cat := range []string{"idor", "overfetch", "nplus1"} {
		if !strings.Contains(joinedNotCovered, cat) {
			t.Errorf("NotCovered does not name unmapped surface category %q: %v", cat, resp.Manifest.NotCoveredProse)
		}
	}
	if strings.Contains(strings.Join(resp.Manifest.ReasoningProse, "\n"), "idor") {
		t.Error("Reasoning must not claim idor is mapped for go — go maps only authz")
	}
}

// TestHandleCoverage_Go_NamesPRAC004AsPermanentlyExcluded is P1-4b's owed
// manifest entry, landing where R1 put it: codefit-coverage for "go" must
// name PRAC-004 in NotCovered, with its ADR 0056 reason, derived from
// golang.Provider.Capability().Practices.Excluded — not silently absent from
// the declared Practices list the way it was before this change.
func TestHandleCoverage_Go_NamesPRAC004AsPermanentlyExcluded(t *testing.T) {
	resp, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "go"})
	if err != nil {
		t.Fatalf("HandleCoverage(go): %v", err)
	}
	joined := strings.Join(resp.Manifest.NotCoveredProse, "\n")
	if !strings.Contains(joined, "PRAC-004") {
		t.Errorf("codefit-coverage for go must name PRAC-004 as permanently not covered, got NotCovered: %v", resp.Manifest.NotCoveredProse)
	}
	if !strings.Contains(joined, "ADR 0056") {
		t.Errorf("PRAC-004's NotCovered entry must carry its reason (ADR 0056), got: %v", resp.Manifest.NotCoveredProse)
	}
	declared := strings.Join(resp.Manifest.DeterministicProse, "\n")
	if strings.Contains(declared, "PRAC-004") {
		t.Error("PRAC-004 must not also appear in Deterministic — it is excluded, not declared")
	}
}

// TestC4_CoverageManifestCapabilityMatchesAssertion is C4: for every
// registered provider, Capability().CoverageManifest must be true if and
// only if the provider actually implements the optional CoverageManifest()
// method HandleCoverage's type assertion looks for. HandleCoverage's
// behavior is UNCHANGED by this change (it still type-asserts); C4 only adds
// the check that the declared fact and the runtime fact never disagree.
func TestC4_CoverageManifestCapabilityMatchesAssertion(t *testing.T) {
	for _, e := range registry.All() {
		t.Run(e.Canonical, func(t *testing.T) {
			p := e.New(nil)
			_, assertionOK := p.(interface{ CoverageManifest() coverage.Manifest })
			declared := p.Capability().CoverageManifest
			if declared != assertionOK {
				t.Errorf("%s: Capability().CoverageManifest = %v, but the type assertion HandleCoverage uses = %v — they must agree (C4)",
					e.Canonical, declared, assertionOK)
			}
		})
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
	if len(resp.Manifest.DeliveredElsewhereProse) == 0 {
		t.Fatal("codefit-coverage returned an EMPTY DeliveredElsewhere — the DB dimension records DB-201 there, " +
			"so the composition dropped it and the agent never sees the answer")
	}
	if !strings.Contains(strings.Join(resp.Manifest.DeliveredElsewhereProse, "\n"), "DB-201") {
		t.Errorf("DeliveredElsewhere does not name DB-201: %v", resp.Manifest.DeliveredElsewhereProse)
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
