package report_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
)

// surface item helper.
func surf(cat, file string, line int, facts map[string]bool, signals ...string) findings.SurfaceItem {
	return findings.SurfaceItem{
		ID: cat + "-x", Category: cat, File: file, Line: line,
		StructuralSignals: signals, StructuralFacts: facts,
		ReasonToReview: "is this intentional?",
	}
}

// TestAggregateGroupsByEndpoint: a handler with a deterministic finding, an IDOR
// surface item, an authz surface item (all at/after the handler line), and an
// over-fetch item are grouped into ONE endpoint, ordered deterministic →
// confirmed → frontier, with the affirm/ask distinction preserved.
func TestAggregateGroupsByEndpointAndCertainty(t *testing.T) {
	file := "app/tasks/process/route.ts"
	fs := []findings.Finding{
		{ID: "SEC-010", Dimension: "security", Severity: "high", File: file, Line: 14,
			Title: "SQL injection", Description: "inline concat", Confidence: 1.0},
	}
	surface := []findings.SurfaceItem{
		surf("authz", file, 10, map[string]bool{"known_authz_detected": false, "local_access_detected": true}, "Handler GET accesses data via prisma.x.findMany"),
		surf("idor", file, 10, map[string]bool{"local_access_detected": true}, "Receives id"),
		surf("overfetch", file, 16, map[string]bool{"local_access_detected": false, "field_limiting_detected": false}, "Serializes the result of UserService.getAll"),
	}

	eps := report.AggregateEndpoints(fs, surface)
	if len(eps) != 1 {
		t.Fatalf("the four concerns are one handler → one endpoint, got %d endpoints", len(eps))
	}
	ep := eps[0]
	if ep.File != file || ep.Line != 10 {
		t.Errorf("endpoint should anchor to the handler line 10, got %s:%d", ep.File, ep.Line)
	}
	if ep.Method != "GET" {
		t.Errorf("method should be derived from the authz signal, got %q", ep.Method)
	}
	if len(ep.Concerns) != 4 {
		t.Fatalf("want 4 concerns grouped, got %d", len(ep.Concerns))
	}
	// Ordered deterministic → confirmed → frontier.
	wantOrder := []report.CertaintyLevel{
		report.Deterministic, report.SurfaceConfirmed, report.SurfaceConfirmed, report.SurfaceFrontier,
	}
	for i, w := range wantOrder {
		if ep.Concerns[i].Certainty != w {
			t.Errorf("concern[%d] certainty = %q, want %q", i, ep.Concerns[i].Certainty, w)
		}
	}
	// The deterministic concern AFFIRMS; the surface ones ASK.
	if !ep.Concerns[0].Affirms {
		t.Error("the deterministic concern must affirm (codefit asserts)")
	}
	for i := 1; i < 4; i++ {
		if ep.Concerns[i].Affirms {
			t.Errorf("surface concern[%d] must ask, not affirm", i)
		}
	}
	// IDOR refines authz when both are present.
	var idorC *report.Concern
	for i := range ep.Concerns {
		if ep.Concerns[i].Category == "idor" {
			idorC = &ep.Concerns[i]
		}
	}
	if idorC == nil || !idorC.RefinesAuthz {
		t.Error("the IDOR concern should be marked as refining authz")
	}
}

// Endpoints are ordered by ACTIONABLE structural facts, hardest gap first
// (affirmed deterministic → missing access control → over-exposure), then by
// certain-concern count — NOT by how instrumented an endpoint is, and never by
// severity. The real findings come first, not the most-instrumented endpoints.
func TestAggregateOrdersByActionableGap(t *testing.T) {
	// "protected" — a fully checked, heavily instrumented handler whose only
	// actionable fact is an over-fetch (no select). 3 concerns, all certain.
	protectedFile := "app/protected/route.ts"
	// "unguarded" — a single concern, but a MISSING access check (no authz).
	unguardedFile := "app/unguarded/route.ts"
	// "vuln" — an affirmed deterministic finding.
	vulnFile := "app/vuln/route.ts"

	fs := []findings.Finding{
		{ID: "SEC-010", Dimension: "security", File: vulnFile, Line: 6, Title: "SQL injection", Confidence: 1.0},
	}
	surface := []findings.SurfaceItem{
		// protected: authz checked (permission answered), over-fetch no select (an
		// exposure gap only). No IDOR concern — an IDOR with a local access is always
		// an actionable access gap (ownership unverifiable), so it could not stand in
		// for a "protected" endpoint under the corrected model.
		surf("authz", protectedFile, 4, map[string]bool{"known_authz_detected": true, "local_access_detected": true}, "Handler GET accesses data via prisma.p.findMany"),
		surf("overfetch", protectedFile, 7, map[string]bool{"local_access_detected": true, "field_limiting_detected": false}, "serializes prisma.p"),
		// unguarded: a sensitive handler with NO authz detected — the access gap.
		surf("authz", unguardedFile, 4, map[string]bool{"known_authz_detected": false, "local_access_detected": true}, "Handler GET accesses data via prisma.u.findMany"),
		// vuln also carries an authz concern (checked).
		surf("authz", vulnFile, 4, map[string]bool{"known_authz_detected": true, "local_access_detected": true}, "Handler POST mutates state via prisma.v.create"),
	}

	eps := report.AggregateEndpoints(fs, surface)
	if len(eps) != 3 {
		t.Fatalf("want 3 endpoints, got %d", len(eps))
	}
	// vuln (affirmed deterministic) first.
	if eps[0].File != vulnFile {
		t.Errorf("the affirmed deterministic endpoint must rank first, got %s", eps[0].File)
	}
	// unguarded (missing access control, 1 concern) must rank ABOVE protected
	// (checked, 3 certain concerns, only an over-exposure gap).
	posUnguarded, posProtected := indexOf(eps, unguardedFile), indexOf(eps, protectedFile)
	if posUnguarded > posProtected {
		t.Errorf("the unguarded handler (missing access check) must rank above the protected-but-instrumented one; got unguarded@%d protected@%d", posUnguarded, posProtected)
	}
}

// TestGapCounts_EfficiencyRankedLast: an endpoint whose only actionable gap is
// efficiency (N+1) must rank BELOW an endpoint whose only actionable gap is
// exposure (over-fetch) — an N+1 must never outrank an access-control-adjacent
// gap in the summary, mirroring ADR 0006's exposure-vs-access rationale.
func TestGapCounts_EfficiencyRankedLast(t *testing.T) {
	exposureFile := "app/exposed/route.ts"
	efficiencyFile := "app/nplus1/route.ts"

	surface := []findings.SurfaceItem{
		surf("overfetch", exposureFile, 5, map[string]bool{"local_access_detected": true, "field_limiting_detected": false}, "serializes prisma.p"),
		surf("nplus1", efficiencyFile, 5, map[string]bool{"local_access_detected": true}, "query inside for...of loop"),
	}
	eps := report.AggregateEndpoints(nil, surface)
	if len(eps) != 2 {
		t.Fatalf("want 2 endpoints, got %d", len(eps))
	}
	posExposure, posEfficiency := indexOf(eps, exposureFile), indexOf(eps, efficiencyFile)
	if posExposure > posEfficiency {
		t.Errorf("exposure gap must rank ABOVE efficiency (N+1) gap; got exposure@%d efficiency@%d", posExposure, posEfficiency)
	}
}

func indexOf(eps []report.EndpointReport, file string) int {
	for i := range eps {
		if eps[i].File == file {
			return i
		}
	}
	return -1
}
