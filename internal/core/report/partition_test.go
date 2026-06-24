package report_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
)

// ClassifyEndpoints splits aggregated endpoints into three buckets, one per
// resolution level — all by facts codefit already computes, no new judgment:
//   - actionable    : resolved locally AND has a gap (CertainConcerns>0, Actionable>0) → full detail.
//   - resolved_clean: resolved locally, NO gap (CertainConcerns>0, Actionable==0)      → named + verification fact.
//   - frontier      : not resolved locally (CertainConcerns==0)                        → named.
//
// resolved_clean and frontier are epistemologically OPPOSITE: codefit VERIFIED the
// controls locally vs codefit could NOT conclude. They must never be flattened.
func TestClassifyEndpoints_ThreeBuckets(t *testing.T) {
	gapFile := "app/admin/tasks/route.ts"      // local access, no authz → access gap
	cleanFile := "app/users/[id]/route.ts"     // local access, authz present → no gap
	frontierFile := "app/admin/audit/route.ts" // operation left the body → frontier

	surface := []findings.SurfaceItem{
		surf("idor", gapFile, 10,
			map[string]bool{"local_access_detected": true, "known_authz_detected": false},
			"reads params.id", "accesses prisma.task.findUnique"),
		surf("idor", cleanFile, 10,
			map[string]bool{"local_access_detected": true, "known_authz_detected": true},
			"reads params.id", "accesses prisma.user.findUnique",
			"An authorization helper call was detected in the handler body: getServerSession"),
		surf("authz", frontierFile, 5,
			map[string]bool{"local_access_detected": false, "known_authz_detected": false},
			"Handler POST calls AuditService.record"),
	}
	eps := report.AggregateEndpoints(nil, surface)

	actionable, clean, frontier := report.ClassifyEndpoints(eps)

	if len(actionable) != 1 || actionable[0].File != gapFile {
		t.Fatalf("actionable must be the endpoint with a gap, got %+v", actionable)
	}
	if len(actionable[0].Concerns) == 0 || len(actionable[0].Concerns[0].Signals) == 0 {
		t.Errorf("actionable must carry FULL concern detail, got %+v", actionable[0])
	}

	if len(clean) != 1 || clean[0].File != cleanFile {
		t.Fatalf("resolved_clean must be the locally-checked endpoint with no gap, got %+v", clean)
	}
	// The verification fact AFFIRMS what codefit checked — not an absence.
	if clean[0].Verification == "" {
		t.Fatalf("resolved_clean must carry a verification fact, got %+v", clean[0])
	}
	low := strings.ToLower(clean[0].Verification)
	if !strings.Contains(low, "verified") || !strings.Contains(low, "authorization") {
		t.Errorf("verification fact must affirm the authz check codefit saw, got %q", clean[0].Verification)
	}
	if !strings.Contains(low, "no gap") {
		t.Errorf("verification fact must state no gap was found, got %q", clean[0].Verification)
	}

	if len(frontier) != 1 || frontier[0].File != frontierFile {
		t.Fatalf("frontier must be the only not-resolved-locally endpoint, got %+v", frontier)
	}
}

// resolved_clean entries are NAMED, not detailed: no signals/reason embedded, just
// file/method + the verification fact. Keeps the response small.
func TestClassifyEndpoints_ResolvedCleanIsNamedNotDetailed(t *testing.T) {
	file := "app/things/[id]/route.ts"
	surface := []findings.SurfaceItem{
		surf("idor", file, 10,
			map[string]bool{"local_access_detected": true, "known_authz_detected": true},
			"reads params.id", "An authorization helper call was detected in the handler body: auth"),
		surf("overfetch", file, 12,
			map[string]bool{"local_access_detected": true, "field_limiting_detected": true},
			"Serializes the result of prisma.thing.findUnique", "The query limits the returned fields with a select/omit clause"),
	}
	eps := report.AggregateEndpoints(nil, surface)
	actionable, clean, frontier := report.ClassifyEndpoints(eps)

	if len(actionable) != 0 || len(frontier) != 0 {
		t.Fatalf("a fully-checked endpoint is resolved_clean only, got actionable=%d frontier=%d", len(actionable), len(frontier))
	}
	if len(clean) != 1 {
		t.Fatalf("want one resolved_clean endpoint, got %d", len(clean))
	}
	// Both controls verified → the fact mentions authz AND field-limiting.
	low := strings.ToLower(clean[0].Verification)
	if !strings.Contains(low, "authorization") || !strings.Contains(low, "field selection") {
		t.Errorf("verification must affirm both controls codefit checked, got %q", clean[0].Verification)
	}
}

// All-clean project: every endpoint resolved locally with no gap. actionable empty,
// resolved_clean has them all, frontier empty. Honest: codefit checked, controls
// present — not "nothing found".
func TestClassifyEndpoints_AllClean(t *testing.T) {
	surface := []findings.SurfaceItem{
		surf("idor", "app/a/[id]/route.ts", 10,
			map[string]bool{"local_access_detected": true, "known_authz_detected": true},
			"reads params.id", "authz detected: getServerSession"),
		surf("idor", "app/b/[id]/route.ts", 10,
			map[string]bool{"local_access_detected": true, "known_authz_detected": true},
			"reads params.id", "authz detected: requireAuth"),
	}
	eps := report.AggregateEndpoints(nil, surface)
	actionable, clean, frontier := report.ClassifyEndpoints(eps)
	if len(actionable) != 0 || len(frontier) != 0 || len(clean) != 2 {
		t.Fatalf("all-clean: want actionable=0 clean=2 frontier=0, got %d/%d/%d", len(actionable), len(clean), len(frontier))
	}
}

// A frontier-only endpoint stays in frontier even if it carries an access-gap
// signal (no authz on the escaped operation): CertainConcerns==0 wins. codefit did
// not resolve it locally, so it is named for the agent to follow — never actionable.
func TestClassifyEndpoints_FrontierWithGapStaysFrontier(t *testing.T) {
	surface := []findings.SurfaceItem{
		surf("authz", "app/x/route.ts", 5,
			map[string]bool{"local_access_detected": false, "known_authz_detected": false},
			"Handler POST calls XService.do"),
	}
	eps := report.AggregateEndpoints(nil, surface)
	actionable, clean, frontier := report.ClassifyEndpoints(eps)
	if len(actionable) != 0 || len(clean) != 0 || len(frontier) != 1 {
		t.Fatalf("frontier-with-gap must stay frontier, got actionable=%d clean=%d frontier=%d", len(actionable), len(clean), len(frontier))
	}
}
