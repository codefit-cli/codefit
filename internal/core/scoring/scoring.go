package scoring

import "github.com/codefit-cli/codefit/internal/core/findings"

// ScoreSummary is the computed result of an audit: the global score and the
// per-dimension breakdown, each on a 0-100 scale. A dimension whose sensor did
// not run is "not measured" and reported as nil (JSON null) — distinct from a
// dimension that was audited and scored 100. The global is re-normalized over
// the measured dimensions only, so unmeasured dimensions never inflate it
// (PRD section 21).
type ScoreSummary struct {
	Global      int                         `json:"global"`
	ByDimension map[findings.Dimension]*int `json:"by_dimension"`
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

// DefaultWeights are the per-dimension score weights (PRD RF-07, re-balanced by
// ADR 0055); they sum to 100, and every dimension core/findings declares has an
// entry — Compute iterates this map, so an unweighted dimension would be
// silently dropped the moment a sensor measured it.
//
// practices is a dimension of its own and carries the smallest weight: codefit
// audits what the developer never sees, and `any` / `console.log` / a missing
// catch are the most visible defects there are — a linter flags them in the
// editor. Its 5 points come from complexity, which is never measured (post-v1.0),
// so the re-balance moves no score codefit produces today.
//
// The PRD's defaults line still reads complexity 15; it is exempt from
// reflect-today and is not corrected.
//
// These are the DEFAULTS, not the effective weights: since roadmap P1-2 a project
// may override them with `report.score_weights` in .codefit.yaml, resolved by
// [ResolveWeights] and consumed at both call sites. Until then the key was
// validated and never read — a config knob that did nothing — and this comment
// said so; it is corrected here rather than left to contradict the code three
// lines below it.
func DefaultWeights() map[findings.Dimension]int {
	return map[findings.Dimension]int{
		findings.DimensionSecurity:   35,
		findings.DimensionReview:     20,
		findings.DimensionDB:         20,
		findings.DimensionComplexity: 10,
		findings.DimensionTests:      10,
		findings.DimensionPractices:  5,
	}
}

// ResolveWeights decides WHICH weight map a scan-all run uses: userWeights
// (typically cfg.Report.ScoreWeights, string-keyed as .codefit.yaml spells
// dimension names) converted to findings.Dimension keys, when the caller
// named at least one entry; DefaultWeights() otherwise (roadmap P1-2 —
// report.score_weights was validated and then never read).
//
// This does NOT re-validate the sum-to-100 contract — config.Validate already
// rejected a map that does not sum to 100 before it ever reaches here — and it
// does NOT pad a partial map with defaults for the dimensions it omits: a map
// naming only {"security": 100} resolves to exactly {security: 100}, nothing
// else. Whether that partial map is USABLE for a given scan (every dimension
// the scan actually measured has a weight) is Compute's/MissingWeights'
// concern, not this function's — ResolveWeights only picks the map.
func ResolveWeights(userWeights map[string]int) map[findings.Dimension]int {
	if len(userWeights) == 0 {
		return DefaultWeights()
	}
	resolved := make(map[findings.Dimension]int, len(userWeights))
	for k, v := range userWeights {
		resolved[findings.Dimension(k)] = v
	}
	return resolved
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

// Compute calculates the per-dimension scores and the weighted global score.
// Only the dimensions in measured are scored (others are reported as nil, "not
// measured"); a measured dimension with no findings scores 100. The global is
// the weighted average over the measured dimensions' weights only.
func Compute(measured []findings.Dimension, fs []findings.Finding, weights map[findings.Dimension]int) ScoreSummary {
	measuredSet := make(map[findings.Dimension]bool, len(measured))
	for _, d := range measured {
		measuredSet[d] = true
	}

	byDim := make(map[findings.Dimension]*int, len(weights))
	totalWeight, weighted := 0, 0
	for dim, w := range weights {
		if !measuredSet[dim] {
			byDim[dim] = nil // not measured
			continue
		}
		score := DimensionScore(findingsFor(fs, dim))
		byDim[dim] = &score
		weighted += score * w
		totalWeight += w
	}

	global := 0
	if totalWeight > 0 {
		global = weighted / totalWeight
	}
	return ScoreSummary{Global: global, ByDimension: byDim}
}

// MissingWeights returns the measured dimensions that have NO weight. Compute
// iterates the weights map, so such a dimension would be silently dropped (no
// by_dimension entry, no global contribution). A caller runs this guard before
// Compute and fails loudly on a non-empty result: a measured dimension without a
// weight is a codefit wiring bug, never a silently incomplete score (ADR 0021).
func MissingWeights(measured []findings.Dimension, weights map[findings.Dimension]int) []findings.Dimension {
	var missing []findings.Dimension
	for _, d := range measured {
		if _, ok := weights[d]; !ok {
			missing = append(missing, d)
		}
	}
	return missing
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
