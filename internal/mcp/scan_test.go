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
	if resp.Language != "typescript" || len(resp.Index) == 0 {
		t.Errorf("coverage answer incomplete: %d entries for %q", len(resp.Index), resp.Language)
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
	if resp.Language != "go" {
		t.Errorf("Language = %q, want %q", resp.Language, "go")
	}
	byID := indexByID(resp)
	if len(byID) == 0 {
		t.Fatal("vacuum: the derived floor indexed nothing, so every lookup below would fail for the wrong reason")
	}
	if e, ok := byID["SEC-001"]; !ok || e.Status != coverage.StatusDeterministic {
		t.Fatalf("the derived floor must name go's declared security rule ids as deterministic entries; SEC-001 = %+v (present=%v)", e, ok)
	}
	// Each unmapped surface category is an entry of its OWN, keyed off the locked
	// surface.ProviderCategories vocabulary. Asking by id is what an agent can
	// actually do: a category named only inside some other entry's sentence is
	// not something it can look up.
	for _, cat := range []string{"idor", "overfetch", "nplus1"} {
		id := "surface." + cat
		e, ok := byID[id]
		if !ok {
			t.Errorf("no entry %q — the derived floor must declare the surface categories go does NOT map, by id", id)
			continue
		}
		if e.Status != coverage.StatusNotCovered {
			t.Errorf("%s is not mapped for go and its entry says %q, not %q", id, e.Status, coverage.StatusNotCovered)
		}
	}
	// The other side of the same fact: go DOES map authz, so its entry must be
	// the reasoning one. Without this, an answer that called everything
	// not-covered would satisfy the loop above.
	if e, ok := byID["surface.authz"]; !ok || e.Status != coverage.StatusReasoning {
		t.Errorf("go maps the authz surface, so surface.authz must be a reasoning entry; got %+v (present=%v)", e, ok)
	}
}

// indexByID is the response's index keyed by id — the lookup an agent performs
// when it reads the index and then asks for one entry by name. The controls
// below ask their questions of ONE NAMED ENTRY rather than of every claim
// joined together, so an answer that moved a fact onto some other entry no
// longer satisfies them.
func indexByID(resp mcp.CoverageResponse) map[string]coverage.IndexEntry {
	byID := make(map[string]coverage.IndexEntry, len(resp.Index))
	for _, e := range resp.Index {
		byID[e.ID] = e
	}
	return byID
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
	e, ok := indexByID(resp)["PRAC-004"]
	if !ok {
		t.Fatal("codefit-coverage for go must carry PRAC-004 as an entry of its own — an exclusion an agent " +
			"cannot name is one it cannot ask about")
	}
	if e.Status != coverage.StatusNotCovered {
		t.Errorf("PRAC-004 is excluded, not declared: its entry says %q", e.Status)
	}
	// The reason rides on THAT entry's own claim, never on a sibling's — the same
	// welding ADR 0075 requires of SEC-001's declared limit.
	if !strings.Contains(e.Claim, "ADR 0056") {
		t.Errorf("PRAC-004's not-covered entry must carry its reason (ADR 0056), got: %s", e.Claim)
	}
	for _, other := range resp.Index {
		if other.ID != "PRAC-004" && other.Status == coverage.StatusDeterministic && strings.Contains(other.Claim, "PRAC-004") {
			t.Errorf("PRAC-004 also appears on %s's declared claim — it is excluded, not declared", other.ID)
		}
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
	var delivered []string
	for _, e := range resp.Index {
		if e.Status == coverage.StatusDeliveredElsewhere {
			delivered = append(delivered, e.ID)
		}
	}
	if len(delivered) == 0 {
		t.Fatal("codefit-coverage returned an EMPTY delivered-elsewhere bucket — the DB dimension records DB-201 " +
			"there, so the composition dropped it and the agent never sees the answer")
	}

	// The index must SHOW the third answer exists — an agent that never sees the
	// status never asks. The promised id itself still lives in the entry's prose
	// until the per-rule split gives it an id of its own, so DB-201 is checked
	// where it actually is: resolved through the same tool call the agent makes.
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), string(coverage.StatusDeliveredElsewhere)) {
		t.Errorf("the serialized coverage response never names the delivered-elsewhere status — the agent reads "+
			"this JSON, not the struct:\n%.400s", raw)
	}

	full, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "typescript", Detail: delivered})
	if err != nil {
		t.Fatalf("resolving the delivered-elsewhere entries: %v", err)
	}
	if len(full.Unrecognized) != 0 {
		t.Fatalf("delivered-elsewhere ids the index advertised did not resolve: %v", full.Unrecognized)
	}
	var prose []string
	for _, e := range full.Detail {
		prose = append(prose, coverage.ProseOf(e))
	}
	if !strings.Contains(strings.Join(prose, "\n"), "DB-201") {
		t.Errorf("no delivered-elsewhere entry names DB-201; ids were %v", delivered)
	}
	rawDetail, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawDetail), "DB-201") {
		t.Error("DB-201 does not survive into the JSON the agent reads")
	}
}
