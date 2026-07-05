package baseline_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
)

// secScope is the category set the security sensor owns — the scope of a
// security-only run.
func secScope() map[string]bool {
	return map[string]bool{"security": true, "idor": true, "authz": true, "overfetch": true}
}

func hasItem(items []baseline.Item, fp string) bool {
	for _, it := range items {
		if it.FP == fp {
			return true
		}
	}
	return false
}

func inGone(res baseline.DiffResult, fp string) bool {
	for _, it := range res.Gone {
		if it.FP == fp {
			return true
		}
	}
	return false
}

// THE central case: an item whose sensor did NOT run this pass must not be marked
// gone — it is carried forward untouched, absent from the delta.
func TestDiff_ForeignItem_SensorDidNotRun_NotGone(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "secfp", Category: "security", File: "a.ts"},
		{FP: "dbfp", Category: "db-fk-no-index", File: "schema.prisma"}, // foreign, sensor did not run
	}}
	// Only security ran: it observes its item; scope is security categories only.
	res := baseline.Diff(prev,
		[]baseline.Observed{{FP: "secfp", Category: "security", File: "a.ts", Affirms: true}},
		secScope())

	if inGone(res, "dbfp") {
		t.Error("foreign db item must NOT be gone when its sensor did not run")
	}
	if _, ok := res.State["dbfp"]; ok {
		t.Error("foreign item must be absent from the delta (State)")
	}
	if !hasItem(res.Next.Items, "dbfp") {
		t.Error("foreign item must be carried forward verbatim to Next.Items")
	}
	if res.Counts.Gone != 0 {
		t.Errorf("Gone count = %d, want 0", res.Counts.Gone)
	}
}

// The other direction: when the sensor DID run and its item is not observed, it IS gone.
func TestDiff_ForeignItem_SensorRan_IsGone(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "dbfp", Category: "db-fk-no-index", File: "schema.prisma"},
	}}
	scope := secScope()
	scope["db-fk-no-index"] = true // the db sensor ran this pass too
	res := baseline.Diff(prev, nil, scope)
	if !inGone(res, "dbfp") {
		t.Error("an in-scope item that is not observed IS gone")
	}
}

// Fail-safe: an empty scope means nothing is gone (a caller that forgets scope
// never corrupts — it only under-reports).
func TestDiff_EmptyScope_NothingGone(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "a", Category: "security", File: "a.ts"},
		{FP: "b", Category: "overfetch", File: "b.ts"},
	}}
	res := baseline.Diff(prev, nil, map[string]bool{})
	if res.Counts.Gone != 0 {
		t.Errorf("empty scope: Gone = %d, want 0", res.Counts.Gone)
	}
	if len(res.Next.Items) != 2 {
		t.Errorf("empty scope: all prev items carried forward, got %d of 2", len(res.Next.Items))
	}
}

// No-regression: with full security scope over a security-only baseline, the delta
// is exactly today's (known / new / gone).
func TestDiff_SecurityOnly_Unchanged(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "known", Category: "overfetch", File: "a.ts"},
		{FP: "goneItem", Category: "authz", File: "b.ts"},
	}}
	res := baseline.Diff(prev,
		[]baseline.Observed{
			{FP: "known", Category: "overfetch", File: "a.ts"},
			{FP: "new", Category: "idor", File: "c.ts"},
		},
		secScope())

	if res.State["known"] != baseline.StateKnown {
		t.Errorf("known state = %q, want known", res.State["known"])
	}
	if res.State["new"] != baseline.StateNew {
		t.Errorf("new state = %q, want new", res.State["new"])
	}
	if !inGone(res, "goneItem") {
		t.Error("an in-scope unobserved item must be gone (authz item)")
	}
	if res.Counts.Gone != 1 {
		t.Errorf("Gone = %d, want 1", res.Counts.Gone)
	}
}

// The "changed" pairing (same file+category) still works within scope.
func TestDiff_ChangedPairing_WithinScope(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "old", Category: "overfetch", File: "a.ts"},
	}}
	res := baseline.Diff(prev,
		[]baseline.Observed{{FP: "newfp", Category: "overfetch", File: "a.ts"}},
		secScope())
	if res.State["newfp"] != baseline.StateChanged {
		t.Errorf("state = %q, want changed (paired with the gone item at same file+category)", res.State["newfp"])
	}
}
