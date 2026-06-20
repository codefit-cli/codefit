package scoring

import "github.com/codefit-cli/codefit/internal/core/findings"

// ScoreSummary is the computed result of an audit: the global score and the
// per-dimension breakdown, each on a 0-100 scale.
type ScoreSummary struct {
	Global      int                        `json:"global"`
	ByDimension map[findings.Dimension]int `json:"by_dimension"`
}

// severityPenalty is the point cost a finding of each severity subtracts from a
// dimension's base score of 100.
var severityPenalty = map[findings.Severity]int{
	findings.SeverityCritical: 20,
	findings.SeverityHigh:     10,
	findings.SeverityMedium:   5,
	findings.SeverityLow:      2,
	findings.SeverityInfo:     0,
}

// DefaultWeights are the per-dimension score weights from the PRD (RF-07); they
// sum to 100.
func DefaultWeights() map[findings.Dimension]int {
	return map[findings.Dimension]int{
		findings.DimensionSecurity:   35,
		findings.DimensionReview:     20,
		findings.DimensionDB:         20,
		findings.DimensionComplexity: 15,
		findings.DimensionTests:      10,
	}
}

// counts reports whether a finding contributes to scoring. Suppressed
// (consent-accepted) and baselined (historical debt) findings do not penalize.
func counts(f findings.Finding) bool {
	return f.Suppressed == nil && !f.Baselined
}

// DimensionScore returns the 0-100 score for a single dimension's findings: a
// base of 100 minus the severity penalties of the counting findings, clamped
// to a floor of 0.
func DimensionScore(fs []findings.Finding) int {
	score := 100
	for _, f := range fs {
		if !counts(f) {
			continue
		}
		score -= severityPenalty[f.Severity]
	}
	if score < 0 {
		return 0
	}
	return score
}

// Compute calculates the per-dimension scores for every weighted dimension and
// the weighted global score. Dimensions with no findings score 100.
func Compute(fs []findings.Finding, weights map[findings.Dimension]int) ScoreSummary {
	byDim := make(map[findings.Dimension]int, len(weights))
	for dim := range weights {
		byDim[dim] = DimensionScore(findingsFor(fs, dim))
	}

	totalWeight, weighted := 0, 0
	for dim, w := range weights {
		weighted += byDim[dim] * w
		totalWeight += w
	}
	global := 0
	if totalWeight > 0 {
		global = weighted / totalWeight
	}
	return ScoreSummary{Global: global, ByDimension: byDim}
}

// findingsFor returns the findings belonging to a dimension.
func findingsFor(fs []findings.Finding, dim findings.Dimension) []findings.Finding {
	var out []findings.Finding
	for _, f := range fs {
		if f.Dimension == dim {
			out = append(out, f)
		}
	}
	return out
}

// IsBlocked reports whether the report must block the deploy: a critical
// security finding that is neither suppressed (consent) nor baselined. This is
// non-configurable — critical security always blocks (PRD §18).
func IsBlocked(fs []findings.Finding) bool {
	for _, f := range fs {
		if f.Dimension == findings.DimensionSecurity &&
			f.Severity == findings.SeverityCritical &&
			counts(f) {
			return true
		}
	}
	return false
}
