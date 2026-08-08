package baseline_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/scope"
)

// R2's symmetry (baseline-write-gate spec, test contract item 4): the `gone`
// direction is guarded by a two-dimensional scope (category AND file, ADR
// 0048/filescope_test.go) so a pass cannot prune what it never opened. The
// `known` direction had no equivalent guard: ANY observed item matching a
// previous fingerprint was promoted to known/refreshed, even when that
// item's own category did not run this pass or its file was never opened.
//
// This is not hypothetical: the code x schema cross rules (DB-010/DB-013)
// anchor their fingerprint to the SCHEMA file, which the DB dimension always
// reads in full regardless of scope narrowing — so a narrowed pass, whose
// `scanned` set deliberately excludes cross categories (scanall.go), can
// still re-observe the same fingerprint from a shrunken set of query
// filters. Without this guard that re-observation silently re-confirms
// "known" under a category the pass itself declared out of scope.

// TestDiff_ObservedButCategoryDidNotRun_NotPromotedToKnown is the category
// dimension of the symmetry: an observation matching a previous item whose
// CATEGORY is not in `scanned` must not be promoted to known, and must not be
// duplicated in Next.Items (the out-of-scope carry-forward loop already
// carried it forward once).
func TestDiff_ObservedButCategoryDidNotRun_NotPromotedToKnown(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "crossfp", Category: "db-010", File: "schema.prisma", Snippet: "old snippet"},
	}}
	// The rule still ran and re-observed the SAME fingerprint (schema-anchored,
	// so it survives a narrowed pass), but "db-010" is not in scanned — a
	// narrowed pass whose scanned set excludes cross categories.
	res := baseline.Diff(prev,
		[]baseline.Observed{{FP: "crossfp", Category: "db-010", File: "schema.prisma", Snippet: "old snippet"}},
		secScope(), // does not include "db-010"
		scope.Full())

	if _, ok := res.State["crossfp"]; ok {
		t.Errorf("an observed item whose category did not run must be ABSENT from the delta (State), got %q", res.State["crossfp"])
	}
	if res.Shown["crossfp"] {
		t.Error("an observed item whose category did not run must not be shown — the pass has no basis to reconfirm it")
	}
	n := 0
	for _, it := range res.Next.Items {
		if it.FP == "crossfp" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("crossfp must be carried forward EXACTLY ONCE, got %d — a second copy means the known-direction "+
			"guard is missing and the observed-loop re-added an item the out-of-scope carry-forward already kept", n)
	}
}

// TestDiff_ObservedButFileNotOpened_NotPromotedToKnown is the file dimension
// of the same symmetry: the category ran, but the item's own FILE was never
// opened this pass (e.g. a schema-anchored fingerprint re-observed while the
// file scope narrowed to unrelated code files). Mirrors
// TestDiff_OutOfFileScope_Unobserved_NotGone in filescope_test.go, on the
// known side instead of the gone side.
func TestDiff_ObservedButFileNotOpened_NotPromotedToKnown(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "crossfp", Category: "overfetch", File: "schema.prisma", Snippet: "old snippet"},
	}}
	res := baseline.Diff(prev,
		[]baseline.Observed{{FP: "crossfp", Category: "overfetch", File: "schema.prisma", Snippet: "old snippet"}},
		secScope(),                           // "overfetch" DID run
		scope.Of([]string{"src/touched.ts"}), // schema.prisma was never opened this pass
	)

	if _, ok := res.State["crossfp"]; ok {
		t.Errorf("an observed item whose file was not opened must be ABSENT from the delta (State), got %q", res.State["crossfp"])
	}
	if res.Shown["crossfp"] {
		t.Error("an observed item whose file was not opened must not be shown")
	}
	n := 0
	for _, it := range res.Next.Items {
		if it.FP == "crossfp" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("crossfp must be carried forward EXACTLY ONCE, got %d", n)
	}
}

// TestDiff_ObservedAndInScope_StillPromotedToKnown is the control: when BOTH
// dimensions admit the item (the same guard that lets an in-scope item become
// gone also lets an in-scope item become known), the existing known behaviour
// is unchanged.
func TestDiff_ObservedAndInScope_StillPromotedToKnown(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "fp", Category: "overfetch", File: "src/touched.ts", Snippet: "old"},
	}}
	res := baseline.Diff(prev,
		[]baseline.Observed{{FP: "fp", Category: "overfetch", File: "src/touched.ts", Snippet: "old"}},
		secScope(),
		scope.Of([]string{"src/touched.ts"}))

	if res.State["fp"] != baseline.StateKnown {
		t.Errorf("an in-scope re-observation must still be known, got %q", res.State["fp"])
	}
	n := 0
	for _, it := range res.Next.Items {
		if it.FP == "fp" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("fp must appear exactly once in Next.Items, got %d", n)
	}
}
