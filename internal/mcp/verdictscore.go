package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// verdictScore is the fold's result: the findings a persisted agent verdict
// contributes to scoring, plus what could NOT be scored and why.
//
// The second half is not bookkeeping. A verdict codefit declines to score and
// says nothing about is under-reporting — the direction
// docs/specs/audit-protocol.md's I3 calls unforgivable — so the count and its
// reason travel together and reach the response.
type verdictScore struct {
	Scored    []findings.Finding
	NotScored int
	Note      string
}

// verdictFindings turns persisted agent verdicts into scoring input (R6/R7, D4,
// ADR 0081 slice 3). It closes the dependency ADR 0021 stated and never
// mechanised: "surface scores only after confirm-surface".
//
// It reads prev — the baseline as loaded, i.e. what was actually recorded — and
// keeps only what survives every one of these, each load-bearing:
//
//  1. STILL OBSERVED. A verdict whose fp is not in this pass's observed set
//     scores nothing: the code moved, so the reasoning no longer applies
//     (ADR 0009's content-hash identity is what makes that check meaningful).
//  2. ONLY `vulnerable`. `not_vulnerable` and `uncertain` produce nothing. They
//     never remove and never zero — an agent recommending that something is fine
//     must not be able to lower the alarm on its own (D4).
//  3. NOT ACKED. A human already decided; the fold honours that here, and the
//     asymmetry with rule 2 IS the autonomy principle: a human's record is a
//     decision, an agent's is a recommendation.
//  4. ONE FINDING PER ITEM. Three agents agreeing is corroboration, not three
//     defects.
//  5. THE DIMENSION GATE. A category can belong to a dimension whose sensor did
//     not run. Such a verdict is DECLARED, never folded and never dropped — see
//     notScoredNote for why both alternatives are worse.
//
// `blocked` is deliberately untouched: scoring.IsBlocked runs over raw sensor
// findings in HandleScanSecurity and scan-all never computes it. Letting an
// agent's probabilistic judgment force a hard block would invert the autonomy
// principle in the one place codefit has no dial (PRD §18).
func verdictFindings(prev *baseline.Baseline, observedFP map[string]bool, measured []findings.Dimension) verdictScore {
	var out verdictScore
	if prev == nil {
		return out
	}
	measuredSet := make(map[findings.Dimension]bool, len(measured))
	for _, d := range measured {
		measuredSet[d] = true
	}

	unmeasured := map[findings.Dimension]int{}
	for _, it := range prev.Items {
		if it.Ack != nil || !observedFP[it.FP] {
			continue
		}
		v, ok := strongestVulnerable(it.AgentVerdicts)
		if !ok {
			continue
		}
		f := surface.FindingFrom(surface.Confirmation{
			Category:   it.Category,
			File:       it.File,
			Verdict:    v.Verdict,
			Reasoning:  v.Reasoning,
			Confidence: v.Confidence,
			Severity:   v.Severity,
		})
		if !measuredSet[f.Dimension] {
			unmeasured[f.Dimension]++
			out.NotScored++
			continue
		}
		out.Scored = append(out.Scored, f)
	}
	out.Note = notScoredNote(unmeasured)
	return out
}

// strongestVulnerable picks the ONE vulnerable verdict an item contributes.
//
// Highest confidence wins; ties go to the LAST recorded, because the list is
// append-ordered and the later pass saw the later state. Both keys are needed:
// without the second, two equally-confident verdicts would make the result
// depend on map/slice happenstance, and a score that reshuffles between
// identical scans is worse than one that ranks imperfectly — a reader cannot
// tell a real change from churn.
func strongestVulnerable(vs []baseline.AgentVerdict) (baseline.AgentVerdict, bool) {
	idx := make([]int, 0, len(vs))
	for i, v := range vs {
		if v.Verdict == surface.VerdictVulnerable {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return baseline.AgentVerdict{}, false
	}
	sort.SliceStable(idx, func(a, b int) bool {
		va, vb := vs[idx[a]], vs[idx[b]]
		if va.Confidence != vb.Confidence {
			return va.Confidence > vb.Confidence
		}
		return idx[a] > idx[b]
	})
	return vs[idx[0]], true
}

// notScoredNote names WHICH dimensions were skipped and WHY, in words, or
// returns empty when nothing was skipped.
//
// The wording carries the reason because the alternative reading is worse than
// the fact: a bare count invites "codefit lost some verdicts". It did not. The
// security sensor OWNS the nplus1 category, so a plain TypeScript project with no
// configured schema observes nplus1 surface while the db dimension never runs.
// Folding such a finding in would make scoring.Compute pass over it in silence,
// and appending its dimension to `measured` to compensate would claim a sensor
// ran that did not — which is exactly the mutation
// summary_measured_completeness_test.go exists to catch.
func notScoredNote(unmeasured map[findings.Dimension]int) string {
	if len(unmeasured) == 0 {
		return ""
	}
	dims := make([]string, 0, len(unmeasured))
	for d := range unmeasured {
		dims = append(dims, fmt.Sprintf("%s (%d)", d, unmeasured[d]))
	}
	sort.Strings(dims)
	return "These confirmed agent verdicts did NOT reach the score, and were not dropped either: " +
		strings.Join(dims, ", ") + ". Their dimension was NOT MEASURED on this run, so codefit has no " +
		"measured baseline to score them against — a category can belong to a dimension whose sensor did " +
		"not run (nplus1 is mapped to db but enumerated by the security sensor, so a project with no " +
		"configured database.schema_paths produces exactly this). Scoring them anyway would count them " +
		"against a dimension nobody measured; claiming the dimension WAS measured would be worse. " +
		"Configure the dimension's inputs and the next scan will score them."
}

// observedFPs is the fingerprint set of what this pass actually saw. The fold
// intersects against it so a verdict on code that has since changed or gone
// scores nothing — the staleness guarantee ADR 0009's content hash provides is
// only real if something checks it.
func observedFPs(observed []baseline.Observed) map[string]bool {
	set := make(map[string]bool, len(observed))
	for _, o := range observed {
		set[o.FP] = true
	}
	return set
}
