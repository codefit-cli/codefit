package scoring_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

func f(dim findings.Dimension, sev findings.Severity) findings.Finding {
	return findings.Finding{Dimension: dim, Severity: sev}
}

func TestDimensionScoreNoFindings(t *testing.T) {
	if got := scoring.DimensionScore(nil); got != 100 {
		t.Errorf("no findings should score 100, got %d", got)
	}
}

func TestDimensionScorePenalties(t *testing.T) {
	cases := []struct {
		name string
		in   []findings.Finding
		want int
	}{
		{"one critical", []findings.Finding{f("security", "critical")}, 80},
		{"one high", []findings.Finding{f("security", "high")}, 90},
		{"critical+high", []findings.Finding{f("security", "critical"), f("security", "high")}, 70},
		{"medium+low", []findings.Finding{f("review", "medium"), f("review", "low")}, 93},
		{"info is free", []findings.Finding{f("review", "info")}, 100},
		{"clamped at zero", []findings.Finding{
			f("security", "critical"), f("security", "critical"), f("security", "critical"),
			f("security", "critical"), f("security", "critical"), f("security", "critical"),
		}, 0},
	}
	for _, tc := range cases {
		if got := scoring.DimensionScore(tc.in); got != tc.want {
			t.Errorf("%s: DimensionScore = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestComputeGlobalWeighted(t *testing.T) {
	// security: one critical -> 80; every other dimension: 100.
	in := []findings.Finding{f("security", "critical")}
	s := scoring.Compute(in, scoring.DefaultWeights())
	// (35*80 + 20*100 + 20*100 + 15*100 + 10*100) / 100 = 93
	if s.Global != 93 {
		t.Errorf("Global = %d, want 93", s.Global)
	}
	if s.ByDimension[findings.DimensionSecurity] != 80 {
		t.Errorf("security dimension = %d, want 80", s.ByDimension[findings.DimensionSecurity])
	}
	if s.ByDimension[findings.DimensionReview] != 100 {
		t.Errorf("review dimension = %d, want 100", s.ByDimension[findings.DimensionReview])
	}
}

func TestComputeSkipsSuppressedAndBaselined(t *testing.T) {
	suppressed := f("security", "critical")
	suppressed.Suppressed = &findings.ConsentRecord{AcceptedBy: "x"}
	baselined := f("security", "critical")
	baselined.Baselined = true

	s := scoring.Compute([]findings.Finding{suppressed, baselined}, scoring.DefaultWeights())
	if s.ByDimension[findings.DimensionSecurity] != 100 {
		t.Errorf("suppressed/baselined findings must not lower the score, got %d",
			s.ByDimension[findings.DimensionSecurity])
	}
}

func TestIsBlocked(t *testing.T) {
	critSec := f("security", "critical")
	suppressed := f("security", "critical")
	suppressed.Suppressed = &findings.ConsentRecord{AcceptedBy: "x"}
	baselined := f("security", "critical")
	baselined.Baselined = true

	cases := []struct {
		name string
		in   []findings.Finding
		want bool
	}{
		{"critical security blocks", []findings.Finding{critSec}, true},
		{"suppressed does not block", []findings.Finding{suppressed}, false},
		{"baselined does not block", []findings.Finding{baselined}, false},
		{"critical non-security does not block", []findings.Finding{f("db", "critical")}, false},
		{"high security does not block", []findings.Finding{f("security", "high")}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		if got := scoring.IsBlocked(tc.in); got != tc.want {
			t.Errorf("%s: IsBlocked = %v, want %v", tc.name, got, tc.want)
		}
	}
}
