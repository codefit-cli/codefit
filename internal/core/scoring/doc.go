// Package scoring computes the per-dimension scores (0-100) and the weighted
// global score from a set of findings, using the configurable weights from
// .codefit.yaml (PRD RF-07).
//
// [Compute] returns a [ScoreSummary] (per-dimension and weighted global score),
// and [IsBlocked] reports whether a critical, unconsented security finding must
// block the deploy.
package scoring
