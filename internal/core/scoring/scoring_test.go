package scoring_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

func f(dim findings.Dimension, sev findings.Severity) findings.Finding {
	return findings.Finding{Dimension: dim, Severity: sev}
}

// dim returns the score for a measured dimension, failing if it is nil (not
// measured) when the test expected it measured.
func dim(t *testing.T, s scoring.ScoreSummary, d findings.Dimension) int {
	t.Helper()
	p, ok := s.ByDimension[d]
	if !ok || p == nil {
		t.Fatalf("dimension %q expected measured, got nil/missing", d)
	}
	return *p
}

func TestDimensionScorePenalties(t *testing.T) {
	cases := []struct {
		name string
		in   []findings.Finding
		want int
	}{
		{"none", nil, 100},
		{"one critical", []findings.Finding{f("security", "critical")}, 80},
		{"critical+high", []findings.Finding{f("security", "critical"), f("security", "high")}, 70},
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

func TestComputeReportsOnlyMeasuredDimensions(t *testing.T) {
	measured := []findings.Dimension{findings.DimensionSecurity}
	in := []findings.Finding{f("security", "critical")}
	s := scoring.Compute(measured, in, scoring.DefaultWeights())

	if got := dim(t, s, findings.DimensionSecurity); got != 80 {
		t.Errorf("security = %d, want 80", got)
	}
	// Unmeasured dimensions must be nil (not 100).
	for _, d := range []findings.Dimension{
		findings.DimensionReview, findings.DimensionDB,
		findings.DimensionComplexity, findings.DimensionTests,
	} {
		if p := s.ByDimension[d]; p != nil {
			t.Errorf("dimension %q should be nil (not measured), got %d", d, *p)
		}
	}
	// Global is re-normalized over measured weights only: just security → 80.
	if s.Global != 80 {
		t.Errorf("Global = %d, want 80 (only security measured)", s.Global)
	}
}

func TestComputeRenormalizesGlobalOverMeasured(t *testing.T) {
	measured := []findings.Dimension{findings.DimensionSecurity, findings.DimensionReview}
	in := []findings.Finding{f("security", "critical")} // security 80, review clean 100
	s := scoring.Compute(measured, in, scoring.DefaultWeights())
	// (80*35 + 100*20) / (35+20) = 4800/55 = 87
	if s.Global != 87 {
		t.Errorf("Global = %d, want 87 (re-normalized over security+review)", s.Global)
	}
}

func TestComputeNotMeasuredSerializesNull(t *testing.T) {
	s := scoring.Compute([]findings.Dimension{findings.DimensionSecurity}, nil, scoring.DefaultWeights())
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"complexity":null`) {
		t.Errorf("an unmeasured dimension must serialize as null:\n%s", data)
	}
}

func TestComputeNoMeasuredDimensions(t *testing.T) {
	s := scoring.Compute(nil, nil, scoring.DefaultWeights())
	if s.Global != 0 {
		t.Errorf("Global with nothing measured = %d, want 0", s.Global)
	}
	for d, p := range s.ByDimension {
		if p != nil {
			t.Errorf("dimension %q should be nil, got %d", d, *p)
		}
	}
}

func TestComputeSkipsSuppressedAndBaselined(t *testing.T) {
	suppressed := f("security", "critical")
	suppressed.Suppressed = &findings.ConsentRecord{AcceptedBy: "x"}
	baselined := f("security", "critical")
	baselined.Baselined = true

	s := scoring.Compute([]findings.Dimension{findings.DimensionSecurity},
		[]findings.Finding{suppressed, baselined}, scoring.DefaultWeights())
	if got := dim(t, s, findings.DimensionSecurity); got != 100 {
		t.Errorf("suppressed/baselined must not lower the score, got %d", got)
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
