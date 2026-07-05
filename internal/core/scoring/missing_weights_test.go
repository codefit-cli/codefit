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
	// practices is NOT in DefaultWeights.
	got := scoring.MissingWeights([]findings.Dimension{findings.DimensionPractices}, scoring.DefaultWeights())
	if len(got) != 1 || got[0] != findings.DimensionPractices {
		t.Errorf("MissingWeights = %v, want [practices]", got)
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
