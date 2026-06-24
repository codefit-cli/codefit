package report_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
)

// PartitionByResolution splits aggregated endpoints by the FACT codefit already
// computes: did it conclude locally (≥1 deterministic or surface_confirmed concern,
// i.e. CertainConcerns>0) or is every concern at the frontier (the data left the
// handler body, local_access_detected=false). The first set goes WHOLE into the
// summary; the second is only NAMED on-demand. The criterion is local_access_
// detected, not an arbitrary cut.
func TestPartitionByResolution(t *testing.T) {
	// A confirmed endpoint: an IDOR with a local Prisma access (local_access_detected
	// =true) and no authz → it is actionable, goes whole.
	confirmedFile := "app/tasks/process/route.ts"
	// A frontier-only endpoint: an authz item whose operation left the body
	// (local_access_detected=false) → named only, on-demand.
	frontierFile := "app/admin/audit/route.ts"

	surface := []findings.SurfaceItem{
		surf("idor", confirmedFile, 10,
			map[string]bool{"local_access_detected": true, "known_authz_detected": false},
			"reads params.id", "accesses prisma.task.findUnique"),
		surf("authz", frontierFile, 5,
			map[string]bool{"local_access_detected": false, "known_authz_detected": false},
			"Handler POST calls AuditService.record"),
	}
	eps := report.AggregateEndpoints(nil, surface)

	actionable, frontier := report.PartitionByResolution(eps)

	if len(actionable) != 1 || actionable[0].File != confirmedFile {
		t.Fatalf("the confirmed endpoint must be actionable (whole), got %+v", actionable)
	}
	// The actionable endpoint keeps its full concerns (signals + reason), not a stub.
	if len(actionable[0].Concerns) == 0 || len(actionable[0].Concerns[0].Signals) == 0 {
		t.Errorf("actionable endpoint must carry full concern detail, got %+v", actionable[0])
	}

	if len(frontier) != 1 || frontier[0].File != frontierFile {
		t.Fatalf("the frontier-only endpoint must be named in the pending list, got %+v", frontier)
	}
	// Named, not detailed: file + categories, no signals/reason on the frontier entry.
	if len(frontier[0].Categories) != 1 || frontier[0].Categories[0] != "authz" {
		t.Errorf("frontier entry must name its categories, got %+v", frontier[0])
	}
}

// An endpoint that carries BOTH a confirmed concern and a frontier concern goes
// WHOLE into actionable (the agent reasons the endpoint, not loose concerns) — its
// frontier concern travels with it, it is NOT split off into frontier_pending.
func TestPartitionKeepsFrontierConcernsWithConfirmedEndpoint(t *testing.T) {
	file := "app/notes/[id]/route.ts"
	surface := []findings.SurfaceItem{
		surf("idor", file, 10,
			map[string]bool{"local_access_detected": true, "known_authz_detected": false},
			"reads params.id", "accesses prisma.note.findUnique"),
		surf("overfetch", file, 12,
			map[string]bool{"local_access_detected": false, "field_limiting_detected": false},
			"Serializes the result of NoteService.enrich"),
	}
	eps := report.AggregateEndpoints(nil, surface)

	actionable, frontier := report.PartitionByResolution(eps)
	if len(actionable) != 1 {
		t.Fatalf("a mixed endpoint must be actionable, got %d", len(actionable))
	}
	if len(frontier) != 0 {
		t.Fatalf("a mixed endpoint must NOT also appear in frontier_pending, got %+v", frontier)
	}
	if len(actionable[0].Concerns) != 2 {
		t.Errorf("the actionable endpoint must carry BOTH concerns (confirmed + frontier), got %d", len(actionable[0].Concerns))
	}
}

// All-frontier project: nothing concluded locally. Actionable is empty; every
// endpoint is named in frontier_pending. This is a valid, honest result — the
// caller must communicate it as "could not conclude locally", not "clean".
func TestPartitionAllFrontier(t *testing.T) {
	surface := []findings.SurfaceItem{
		surf("authz", "app/a/route.ts", 5,
			map[string]bool{"local_access_detected": false, "known_authz_detected": false},
			"Handler POST calls AService.do"),
		surf("authz", "app/b/route.ts", 5,
			map[string]bool{"local_access_detected": false, "known_authz_detected": false},
			"Handler GET calls BService.read"),
	}
	eps := report.AggregateEndpoints(nil, surface)

	actionable, frontier := report.PartitionByResolution(eps)
	if len(actionable) != 0 {
		t.Fatalf("all-frontier project must yield no actionable endpoints, got %d", len(actionable))
	}
	if len(frontier) != 2 {
		t.Fatalf("all-frontier project must name every endpoint as pending, got %d", len(frontier))
	}
}
