package scoring_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

// The guard for the measured ⊆ weights contract: a measured dimension without a
// weight would be silently dropped by Compute; MissingWeights surfaces it so the
// caller can fail loudly instead.
func TestMissingWeights_DetectsUnweightedMeasured(t *testing.T) {
	// This test used to name `practices`, which ADR 0055 gave a weight — the guard
	// would have gone on "passing" over a dimension that is no longer unweighted.
	// Every dimension core/findings declares is weighted now
	// (TestDefaultWeights_CoversEveryDeclaredDimension holds that line), so the
	// genuinely unweighted case is the one this guard exists for: a dimension a
	// sensor starts measuring BEFORE anyone adds its weight.
	const unweighted = findings.Dimension("regression-risk")
	if _, ok := scoring.DefaultWeights()[unweighted]; ok {
		t.Fatalf("%q now has a weight — this fixture no longer exercises the unweighted case", unweighted)
	}

	// Measured alongside a weighted dimension: the guard must report the unweighted
	// one and ONLY it, so an implementation that returns everything (or nothing)
	// fails here.
	got := scoring.MissingWeights(
		[]findings.Dimension{findings.DimensionSecurity, unweighted, findings.DimensionDB},
		scoring.DefaultWeights())
	if len(got) != 1 || got[0] != unweighted {
		t.Errorf("MissingWeights = %v, want [%s]", got, unweighted)
	}
}

func TestMissingWeights_AllWeighted(t *testing.T) {
	// security and db both have weights → nothing missing.
	got := scoring.MissingWeights(
		[]findings.Dimension{findings.DimensionSecurity, findings.DimensionDB},
		scoring.DefaultWeights())
	if len(got) != 0 {
		t.Errorf("MissingWeights = %v, want empty (both weighted)", got)
	}
}
