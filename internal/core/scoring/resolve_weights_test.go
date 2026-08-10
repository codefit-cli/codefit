package scoring_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

// ResolveWeights decides WHICH weight map a scan-all run should use: the
// user's cfg.Report.ScoreWeights (config.Validate already rejected one that
// does not sum to 100) when it names at least one dimension, DefaultWeights()
// otherwise. It does not itself decide correctness of Compute — that is
// exercised end-to-end in internal/mcp (roadmap P1-2): a resolver test that
// only checks "the map I handed it comes back" would prove nothing about the
// real handler actually consuming it, so these two tests exercise the two
// PURE-LOGIC edges (empty -> defaults, non-empty -> converted) that are safe
// to unit-test without duplicating the integration coverage.
func TestResolveWeights_EmptyUserMapReturnsDefaults(t *testing.T) {
	got := scoring.ResolveWeights(nil)
	want := scoring.DefaultWeights()
	if len(got) != len(want) {
		t.Fatalf("ResolveWeights(nil) has %d entries, want %d (DefaultWeights): %v", len(got), len(want), got)
	}
	for d, w := range want {
		if got[d] != w {
			t.Errorf("ResolveWeights(nil)[%s] = %d, want %d", d, got[d], w)
		}
	}
}

// A non-empty user map is CONVERTED (string keys -> findings.Dimension) and
// carries only the dimensions the user actually named — it is not merged
// with or padded by DefaultWeights. Two dimensions, deliberately not naming
// every dimension core/findings declares, is the partial-map shape P1-2 makes
// reachable.
func TestResolveWeights_NonEmptyUserMapIsConvertedNotMerged(t *testing.T) {
	got := scoring.ResolveWeights(map[string]int{"security": 80, "db": 20})
	if len(got) != 2 {
		t.Fatalf("ResolveWeights(partial map) has %d entries, want 2 (no padding from defaults): %v", len(got), got)
	}
	if got[findings.DimensionSecurity] != 80 {
		t.Errorf("ResolveWeights(...)[security] = %d, want 80", got[findings.DimensionSecurity])
	}
	if got[findings.DimensionDB] != 20 {
		t.Errorf("ResolveWeights(...)[db] = %d, want 20", got[findings.DimensionDB])
	}
	if _, ok := got[findings.DimensionReview]; ok {
		t.Errorf("ResolveWeights must not invent a review entry the user never named, got %v", got)
	}
}
