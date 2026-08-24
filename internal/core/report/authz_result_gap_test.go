package report_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
)

// SPEC (issue #149) — a detected authz helper clears the access gap only when it
// actually DECIDED something.
//
// known_authz_detected=true clears the gap, so a handler that merely MENTIONS a
// guard left the actionable bucket even when the guard's result went nowhere.
// That is under-reporting, which audit-protocol's I3 calls unforgivable. The
// gate now also requires authz_result_used.
//
// The subtle half is ABSENCE. The Go provider omits known_authz_detected
// entirely when no helper set was searched, because "a false against an empty
// searched set would be the vacuous claim the spec forbids" (ADR 0067) — and it
// does not produce authz_result_used at all. An absent key reads as false in Go,
// so requiring the fact naively would make codefit assert "the result was not
// used" about a producer that never looked. The gap must therefore raise only
// when the fact is PRESENT and false.
func gapOf(t *testing.T, facts map[string]bool) (actionable bool, detail string) {
	t.Helper()
	const file = "app/x/route.ts"
	eps := report.AggregateEndpoints(nil, []findings.SurfaceItem{{
		ID: "authz-x", Category: "authz", File: file, Line: 4,
		StructuralFacts: facts, ReasonToReview: "is this intentional?",
	}})
	if len(eps) != 1 {
		t.Fatalf("want 1 endpoint, got %d", len(eps))
	}
	var d string
	for _, c := range eps[0].Concerns {
		d += c.Gap + " "
	}
	return eps[0].Actionable > 0, d
}

func TestAuthzGapRequiresTheGuardToDecideSomething(t *testing.T) {
	cases := []struct {
		name           string
		facts          map[string]bool
		wantActionable bool
		why            string
	}{
		{
			name:           "guard called and its result used",
			facts:          map[string]bool{"known_authz_detected": true, "authz_result_used": true, "local_access_detected": true},
			wantActionable: false,
			why:            "a guard that decides something answers the authz question — unchanged behaviour",
		},
		{
			name:           "guard called, result DISCARDED",
			facts:          map[string]bool{"known_authz_detected": true, "authz_result_used": false, "local_access_detected": true},
			wantActionable: true,
			why:            "the guard gates nothing here, so the access question is still open (issue #149)",
		},
		{
			name:           "no guard at all",
			facts:          map[string]bool{"known_authz_detected": false, "authz_result_used": false, "local_access_detected": true},
			wantActionable: true,
			why:            "unchanged behaviour",
		},
		{
			name:           "PRODUCER NEVER LOOKED: authz_result_used absent",
			facts:          map[string]bool{"known_authz_detected": true, "local_access_detected": true},
			wantActionable: false,
			why: "the Go provider emits no authz_result_used; treating its ABSENCE as false would make " +
				"codefit assert something about a producer that never looked (ADR 0067)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actionable, detail := gapOf(t, tc.facts)
			if actionable != tc.wantActionable {
				t.Errorf("actionable = %v, want %v — %s\nconcern gaps: %s", actionable, tc.wantActionable, tc.why, detail)
			}
		})
	}
}
