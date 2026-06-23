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

// Endpoints are ordered by the count of CERTAIN concerns (deterministic +
// confirmed), not by severity — more structural facts → higher.
func TestAggregateOrdersEndpointsByFactCount(t *testing.T) {
	a := "app/a/route.ts" // 1 confirmed concern
	b := "app/b/route.ts" // 1 deterministic + 1 confirmed = 2 certain
	fs := []findings.Finding{
		{ID: "SEC-052", Dimension: "security", File: b, Line: 6, Title: "weak crypto", Confidence: 1.0},
	}
	surface := []findings.SurfaceItem{
		surf("authz", a, 4, map[string]bool{"known_authz_detected": false, "local_access_detected": true}, "Handler GET accesses data via prisma.a.findMany"),
		surf("authz", b, 4, map[string]bool{"known_authz_detected": false, "local_access_detected": true}, "Handler POST mutates state via prisma.b.create"),
	}
	eps := report.AggregateEndpoints(fs, surface)
	if len(eps) != 2 {
		t.Fatalf("want 2 endpoints, got %d", len(eps))
	}
	if eps[0].File != b {
		t.Errorf("endpoint b (2 certain concerns) must rank first, got %s", eps[0].File)
	}
	if eps[0].CertainConcerns != 2 || eps[1].CertainConcerns != 1 {
		t.Errorf("certain-concern counts = %d, %d; want 2, 1", eps[0].CertainConcerns, eps[1].CertainConcerns)
	}
}
