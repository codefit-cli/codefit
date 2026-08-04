package scoring_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

// preRebalanceWeights is the weight map codefit shipped through v0.2.7, frozen
// here as a literal on purpose: it is the BASELINE the "the re-balance moves no
// score" claim (ADR 0055) is measured against. Deriving it from DefaultWeights
// would make every comparison below a tautology.
func preRebalanceWeights() map[findings.Dimension]int {
	return map[findings.Dimension]int{
		findings.DimensionSecurity:   35,
		findings.DimensionReview:     20,
		findings.DimensionDB:         20,
		findings.DimensionComplexity: 15,
		findings.DimensionTests:      10,
	}
}

// measuredToday is what scan-all can put in `measured`: security always, db when
// a schema is configured and the parse succeeded (internal/mcp/scanall.go —
// "Per-dimension scoring (ADR 0021)"). No other dimension has a sensor, which is
// the entire reason the re-balance is a no-op.
func measuredToday() []findings.Dimension {
	return []findings.Dimension{findings.DimensionSecurity, findings.DimensionDB}
}

// allDeclaredDimensions mirrors the const block in core/findings. Go cannot
// enumerate a typed string const, so this list is hand-maintained; it exists so
// that a dimension declared without a weight fails here instead of being
// silently dropped by Compute (ADR 0021's contract, stated positively).
func allDeclaredDimensions() []findings.Dimension {
	return []findings.Dimension{
		findings.DimensionSecurity,
		findings.DimensionReview,
		findings.DimensionDB,
		findings.DimensionComplexity,
		findings.DimensionPractices,
		findings.DimensionTests,
	}
}

// practices is a dimension of its own, carrying the smallest weight, and its 5
// points come from complexity (ADR 0055).
func TestDefaultWeights_PracticesIsItsOwnWeightedDimension(t *testing.T) {
	want := map[findings.Dimension]int{
		findings.DimensionSecurity:   35,
		findings.DimensionReview:     20,
		findings.DimensionDB:         20,
		findings.DimensionComplexity: 10,
		findings.DimensionTests:      10,
		findings.DimensionPractices:  5,
	}
	got := scoring.DefaultWeights()
	if len(got) != len(want) {
		t.Fatalf("DefaultWeights has %d dimensions, want %d: got %v", len(got), len(want), got)
	}
	for d, w := range want {
		if got[d] != w {
			t.Errorf("weight[%s] = %d, want %d", d, got[d], w)
		}
	}
	// The two dimensions the re-balance actually moved, pinned against the frozen
	// pre-change map so a silent revert cannot pass.
	if got[findings.DimensionComplexity] >= preRebalanceWeights()[findings.DimensionComplexity] {
		t.Errorf("complexity = %d, want less than the pre-re-balance %d (its 5 points fund practices)",
			got[findings.DimensionComplexity], preRebalanceWeights()[findings.DimensionComplexity])
	}
	if got[findings.DimensionPractices] >= got[findings.DimensionComplexity] {
		t.Errorf("practices (%d) must stay below complexity (%d): codefit audits what the developer never "+
			"sees, and a linter already shows practices defects in the editor",
			got[findings.DimensionPractices], got[findings.DimensionComplexity])
	}
}

// The doc comment on DefaultWeights claims the weights sum to 100. Nothing
// checked that claim before this test.
func TestDefaultWeights_SumIsExactly100(t *testing.T) {
	sum := 0
	for _, w := range scoring.DefaultWeights() {
		sum += w
	}
	if sum != 100 {
		t.Errorf("DefaultWeights sums to %d, want 100 (the doc comment claims it, and "+
			"config validation rejects a user map that does not)", sum)
	}
}

// Every dimension core/findings declares must carry a weight: Compute iterates
// the weights map, so an unweighted dimension is invisible the moment a sensor
// starts measuring it.
func TestDefaultWeights_CoversEveryDeclaredDimension(t *testing.T) {
	w := scoring.DefaultWeights()
	for _, d := range allDeclaredDimensions() {
		if _, ok := w[d]; !ok {
			t.Errorf("dimension %q is declared in core/findings but has no weight", d)
		}
	}
}

// Test contract item 1: the re-balance moves NO score codefit produces today.
// Same findings, same measured set, identical ScoreSummary under the frozen
// pre-re-balance weights and under DefaultWeights.
//
// It holds only because complexity and practices are unmeasured — Compute
// accumulates totalWeight over the measured dimensions alone. Mutation: add
// complexity to measuredToday and both sub-checks fail.
func TestRebalance_MovesNoScoreCodefitProducesToday(t *testing.T) {
	before, after := preRebalanceWeights(), scoring.DefaultWeights()

	// 1. No measured dimension's weight moved at all.
	for _, d := range measuredToday() {
		if before[d] != after[d] {
			t.Errorf("the re-balance moved the weight of %s (%d → %d), a dimension codefit MEASURES today: "+
				"scores produced today would shift", d, before[d], after[d])
		}
	}

	// 2. Over every measured set scan-all can produce, the whole ScoreSummary is
	//    identical. A findings fixture with two DIFFERENT per-dimension scores, so
	//    a weight change would show up in the weighted average.
	fs := []findings.Finding{
		{Dimension: findings.DimensionSecurity, Severity: findings.SeverityCritical}, // security 80
		{Dimension: findings.DimensionDB, Severity: findings.SeverityMedium},         // db 95
	}
	for _, measured := range measuredSets(measuredToday()) {
		b := scoring.Compute(measured, fs, before)
		a := scoring.Compute(measured, fs, after)
		if b.Global != a.Global {
			t.Errorf("measured=%v: global moved %d → %d; the re-balance is NOT a no-op",
				measured, b.Global, a.Global)
		}
		// Every dimension the pre-change map knew about must report the same value.
		// (The post-change map adds `practices`, which is the declared shape change.)
		for d := range before {
			bp, ap := b.ByDimension[d], a.ByDimension[d]
			switch {
			case (bp == nil) != (ap == nil):
				t.Errorf("measured=%v: by_dimension.%s changed measured-ness (%v → %v)", measured, d, bp, ap)
			case bp != nil && *bp != *ap:
				t.Errorf("measured=%v: by_dimension.%s moved %d → %d", measured, d, *bp, *ap)
			}
		}
	}
}

// measuredSets returns every non-empty subset of dims — all the measured sets
// scan-all can hand Compute today.
func measuredSets(dims []findings.Dimension) [][]findings.Dimension {
	var out [][]findings.Dimension
	for mask := 1; mask < 1<<len(dims); mask++ {
		var set []findings.Dimension
		for i, d := range dims {
			if mask&(1<<i) != 0 {
				set = append(set, d)
			}
		}
		out = append(out, set)
	}
	return out
}
