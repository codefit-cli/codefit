package surface_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// TestStableID: the id is a pure function of (file, line, category) — the same
// inputs always yield the same id (so the confirmation step can recompute it
// statelessly), and different inputs yield different ids.
func TestStableID(t *testing.T) {
	a := surface.StableID("app/users/[id]/route.ts", 12, "idor")
	if a == "" {
		t.Fatal("StableID must not be empty")
	}
	if b := surface.StableID("app/users/[id]/route.ts", 12, "idor"); a != b {
		t.Errorf("StableID must be deterministic: %q != %q", a, b)
	}
	// Any component change → different id.
	for _, other := range []string{
		surface.StableID("app/orders/[id]/route.ts", 12, "idor"),
		surface.StableID("app/users/[id]/route.ts", 13, "idor"),
		surface.StableID("app/users/[id]/route.ts", 12, "authz"),
	} {
		if other == a {
			t.Errorf("StableID collision: %q", a)
		}
	}
}

// fakeQuery returns fixed items (ignoring the AST) so Run's orchestration and
// id-stamping can be tested without a parser.
type fakeQuery struct{ items []findings.SurfaceItem }

func (q fakeQuery) Enumerate(_ syntax.Node, _ string) []findings.SurfaceItem { return q.items }

func TestRunStampsStableIDs(t *testing.T) {
	q := fakeQuery{items: []findings.SurfaceItem{
		{Category: "idor", File: "app/users/[id]/route.ts", Line: 7},
	}}
	got := surface.Run([]surface.Query{q}, nil, "app/users/[id]/route.ts")
	if len(got) != 1 {
		t.Fatalf("want 1 item, got %d", len(got))
	}
	want := surface.StableID("app/users/[id]/route.ts", 7, "idor")
	if got[0].ID != want {
		t.Errorf("Run must stamp the stable id: got %q want %q", got[0].ID, want)
	}
}

func conf(verdict surface.Verdict, sev findings.Severity, confidence float64) surface.Confirmation {
	file, line, cat := "app/users/[id]/route.ts", 12, "idor"
	return surface.Confirmation{
		SurfaceID:  surface.StableID(file, line, cat),
		Category:   cat,
		File:       file,
		Line:       line,
		Verdict:    verdict,
		Reasoning:  "the handler reads params.id and queries prisma without an ownership check",
		Confidence: confidence,
		Severity:   sev,
	}
}

func TestIntegrateVulnerableProducesAnchoredProbabilisticFinding(t *testing.T) {
	in := surface.Integrate([]surface.Confirmation{conf(surface.VerdictVulnerable, findings.SeverityHigh, 0.8)})
	if len(in.Findings) != 1 {
		t.Fatalf("vulnerable verdict must produce 1 finding, got %d", len(in.Findings))
	}
	f := in.Findings[0]
	if !f.Probabilistic {
		t.Error("surface finding must be Probabilistic")
	}
	if f.Confidence != 0.8 {
		t.Errorf("confidence should carry the agent's value, got %v", f.Confidence)
	}
	if f.Reasoning == "" {
		t.Error("agent reasoning must be recorded on the finding")
	}
	if f.Dimension != findings.DimensionSecurity {
		t.Errorf("idor finding dimension = %q", f.Dimension)
	}
	// Anchored to the surface item (file/line/category).
	if f.File != "app/users/[id]/route.ts" || f.Line != 12 {
		t.Errorf("finding must anchor to the surface location, got %s:%d", f.File, f.Line)
	}
}

func TestIntegrateNeverEmitsConfidenceOne(t *testing.T) {
	in := surface.Integrate([]surface.Confirmation{conf(surface.VerdictVulnerable, findings.SeverityHigh, 1.0)})
	if len(in.Findings) != 1 {
		t.Fatal("expected 1 finding")
	}
	if c := in.Findings[0].Confidence; c >= 1.0 {
		t.Errorf("a probabilistic finding must never have confidence >= 1.0, got %v", c)
	}
}

func TestIntegrateSeverityFromAgentWithFallback(t *testing.T) {
	// Agent-provided severity is respected (a medical-record IDOR is critical).
	in := surface.Integrate([]surface.Confirmation{conf(surface.VerdictVulnerable, findings.SeverityCritical, 0.9)})
	if in.Findings[0].Severity != findings.SeverityCritical {
		t.Errorf("agent severity must be respected, got %q", in.Findings[0].Severity)
	}
	// No severity provided → class default (idor → high).
	in = surface.Integrate([]surface.Confirmation{conf(surface.VerdictVulnerable, "", 0.7)})
	if in.Findings[0].Severity != findings.SeverityHigh {
		t.Errorf("missing severity should fall back to the idor default (high), got %q", in.Findings[0].Severity)
	}
}

func TestIntegrateNotVulnerableAndUncertainAreRecordedNotFound(t *testing.T) {
	in := surface.Integrate([]surface.Confirmation{
		conf(surface.VerdictNotVulnerable, "", 0.9),
		conf(surface.VerdictUncertain, "", 0.4),
	})
	if len(in.Findings) != 0 {
		t.Errorf("not_vulnerable/uncertain must not produce findings, got %d", len(in.Findings))
	}
	if len(in.Dismissed) != 1 {
		t.Errorf("not_vulnerable must be recorded for traceability, got %d", len(in.Dismissed))
	}
	if len(in.Uncertain) != 1 {
		t.Errorf("uncertain must be flagged for human review, got %d", len(in.Uncertain))
	}
}

func TestIntegrateInvalidSurfaceIDIsRejected(t *testing.T) {
	c := conf(surface.VerdictVulnerable, findings.SeverityHigh, 0.8)
	c.SurfaceID = "deadbeef" // does not recompute from (file, line, category)
	in := surface.Integrate([]surface.Confirmation{c})
	if len(in.Findings) != 0 {
		t.Errorf("a confirmation whose id does not recompute must not become a finding, got %d", len(in.Findings))
	}
	if len(in.Invalid) != 1 {
		t.Errorf("the mismatched confirmation must be recorded as Invalid, got %d", len(in.Invalid))
	}
}

// TestFindingFrom_DimensionForNPlus1IsDB: an agent-confirmed surface item with
// category "nplus1" must be stamped findings.DimensionDB, not the hard-coded
// findings.DimensionSecurity default that findingFrom used before dimensionFor
// existed — an agent-confirmed N+1 must never be reported as a SECURITY finding.
func TestFindingFrom_DimensionForNPlus1IsDB(t *testing.T) {
	file, line, cat := "app/reports/route.ts", 20, "nplus1"
	c := surface.Confirmation{
		SurfaceID:  surface.StableID(file, line, cat),
		Category:   cat,
		File:       file,
		Line:       line,
		Verdict:    surface.VerdictVulnerable,
		Reasoning:  "the handler awaits a Prisma call inside a for...of loop over an array from the database",
		Confidence: 0.8,
	}
	in := surface.Integrate([]surface.Confirmation{c})
	if len(in.Findings) != 1 {
		t.Fatalf("vulnerable verdict must produce 1 finding, got %d", len(in.Findings))
	}
	if got := in.Findings[0].Dimension; got != findings.DimensionDB {
		t.Errorf("nplus1 finding dimension = %q, want %q", got, findings.DimensionDB)
	}
}

// idor/authz/overfetch confirmations must keep DimensionSecurity — dimensionFor's
// default must not regress for the existing categories.
func TestFindingFrom_DimensionDefaultsToSecurity(t *testing.T) {
	in := surface.Integrate([]surface.Confirmation{conf(surface.VerdictVulnerable, findings.SeverityHigh, 0.8)})
	if got := in.Findings[0].Dimension; got != findings.DimensionSecurity {
		t.Errorf("idor finding dimension = %q, want %q (unchanged default)", got, findings.DimensionSecurity)
	}
}

func TestSummaryReportsCompleteAudit(t *testing.T) {
	in := surface.Integration{
		Findings:  []findings.Finding{{}},
		Dismissed: []surface.Confirmation{{}, {}},
		Uncertain: []surface.Confirmation{{}},
	}
	s := surface.Summary("idor", 7, in)
	for _, want := range []string{"7", "1", "2", "1"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary %q missing count %q", s, want)
		}
	}
}
