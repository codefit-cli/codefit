package dbcoverage_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/dbcoverage"
)

// Control E (spec domain dbcoverage-manifest-accuracy, scenario "Manifest no
// longer over-declares") — Controls A/B/D above all check CORRESPONDENCE
// (does an entry exist for every rule, does every mentioned id resolve).
// None of them checks the ACCURACY of a single entry's own prose. This
// control closes exactly that gap for the ONE class of accuracy failure this
// project has now hit twice in the worst possible direction (see the
// dbcoverage.go row of CLAUDE.md's doc map): the source citing a CLOSED
// fabrication as though it were still live, while its own COVERAGE.md
// mirror had already been corrected — the source less truthful than its
// mirror is exactly what this project's own doc-map table exists to catch.
//
// WHAT IT CHECKS, precisely: it locates the db-model-completeness-contract's
// BOUNDARY sentence inside dbcoverage.Reasoning() by its own stable anchor
// phrase ("Complete covers DROPS, not FABRICATIONS"), extracts the balanced
// parenthetical example list that follows the anchor, splits that list on
// top-level commas, and asserts that every comma-delimited clause NAMING a
// fabrication ("phantom" or "fabricat" as a case-insensitive substring —
// matching "fabrication"/"fabricated"/"fabricates" too) also carries a
// closure qualifier ("closed" or "narrowed") SOMEWHERE in that same clause.
// A clause naming neither trigger word is not a fabrication example and is
// skipped entirely — this is deliberate, not a gap: it is what lets a
// STILL-genuinely-open fabrication (e.g. the M5 residual paren-type
// fabrication, SQL-DDL known limit (5)) sit in the very same list without
// being flagged. It is only checked for a qualifier when the manifest ALSO
// calls it a fabrication, and a genuinely open one correctly never earns
// "closed"/"narrowed" — demanding one there would be asking the manifest to
// lie in the opposite direction.
//
// WHY THIS IS A PROPERTY CHECK, NOT A STRING MATCH ON ONE SENTENCE: nothing
// here names "film.fulltext" or any other specific example literally. The
// check fires on ANY future clause added to this same parenthetical that
// names a fabrication without a qualifier, whatever words it uses — which is
// the exact shape of defect that shipped here (a since-closed example left
// unqualified) and the shape a bare substring grep for one token would not
// generalize to.
//
// WHAT IT DOES NOT GUARANTEE, stated the way Control C states its own limit
// rather than left implicit: this is CLAUSE granularity, not WORD
// granularity. Two fabrication mentions joined by an em dash inside ONE
// comma clause — which is exactly the shape the fix below uses, mirroring
// COVERAGE.md's already-corrected wording ("the residual ... fabrication —
// e.g. ... — the closed \"ADD CONSTRAINT\" ... fabrication") — are checked
// TOGETHER: the clause passes because a qualifier appears SOMEWHERE in it,
// not because EVERY fabrication mention inside that one clause individually
// earns its own qualifier. A future edit that adds a THIRD, unqualified
// fabrication mention into that SAME clause (rather than as its own
// comma-separated item) would not be caught here. A word-distance check
// would close that gap, but was rejected: natural-language prose does not
// place a qualifier at a fixed token distance from the word it qualifies
// (the post-fix wording itself puts "closed" measurably closer to the
// SECOND fabrication mention in its clause than the first), so a
// distance-based rule would either fail the legitimate corrected wording or
// accept a qualifier that belongs to an unrelated mention. Comma-clause
// granularity is the coarsest grain that still catches this project's actual
// defect without also flagging correct prose as broken.

// completenessBoundaryAnchor is the stable phrase that identifies the
// db-model-completeness-contract's BOUNDARY sentence among every other entry
// dbcoverage.Reasoning() returns. It is prose already locked by this
// project's own doc comment (dbcoverage.go, item 40) — not a new invention
// for this test.
const completenessBoundaryAnchor = "Complete covers DROPS, not FABRICATIONS"

// fabricationTriggers mark a clause as NAMING a fabrication example.
var fabricationTriggers = []string{"phantom", "fabricat"}

// qualifierWords mark a clause as stating that the fabrication it names is
// no longer live.
var qualifierWords = []string{"closed", "narrowed"}

// boundaryExampleParenthetical locates the BOUNDARY anchor inside
// dbcoverage.Reasoning() and returns the balanced parenthetical example list
// that immediately follows it. It fails the test loudly — never silently
// returns an empty string — if the anchor or a balanced parenthetical cannot
// be found, so a future rewording that drops the anchor phrase breaks this
// control visibly instead of passing over nothing.
func boundaryExampleParenthetical(t *testing.T) string {
	t.Helper()

	var entry string
	for _, e := range proseOf(dbcoverage.Reasoning()) {
		if strings.Contains(e, completenessBoundaryAnchor) {
			entry = e
			break
		}
	}
	if entry == "" {
		t.Fatalf("no entry in dbcoverage.Reasoning() contains the anchor %q — "+
			"the db-model-completeness-contract BOUNDARY sentence moved or was reworded; "+
			"this control cannot locate its target and must not silently pass over nothing",
			completenessBoundaryAnchor)
	}

	afterAnchor := entry[strings.Index(entry, completenessBoundaryAnchor):]
	open := strings.Index(afterAnchor, "(")
	if open < 0 {
		t.Fatalf("found the BOUNDARY anchor but no parenthetical example list follows it in: %.200s", afterAnchor)
	}

	depth := 0
	closeIdx := -1
	for i := open; i < len(afterAnchor); i++ {
		switch afterAnchor[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		t.Fatalf("found the BOUNDARY anchor's opening paren but no matching close — unbalanced parentheses")
	}
	return afterAnchor[open+1 : closeIdx]
}

// unqualifiedFabricationClauses runs the REAL check loop: split the
// parenthetical on top-level commas, keep only clauses naming a fabrication,
// and return those with no closure qualifier anywhere in the same clause.
// Both the main control and its synthetic proof below call this exact
// function, so the proof exercises the real logic rather than a description
// of it.
func unqualifiedFabricationClauses(clauses []string) []string {
	var unqualified []string
	for _, clause := range clauses {
		lower := strings.ToLower(clause)

		names := false
		for _, trigger := range fabricationTriggers {
			if strings.Contains(lower, trigger) {
				names = true
				break
			}
		}
		if !names {
			continue
		}

		qualified := false
		for _, q := range qualifierWords {
			if strings.Contains(lower, q) {
				qualified = true
				break
			}
		}
		if !qualified {
			unqualified = append(unqualified, strings.TrimSpace(clause))
		}
	}
	return unqualified
}

// TestManifest_BoundaryFabricationExamples_AreQualifiedOrOpen is Control E
// itself — the spec's "Manifest no longer over-declares" scenario.
func TestManifest_BoundaryFabricationExamples_AreQualifiedOrOpen(t *testing.T) {
	list := boundaryExampleParenthetical(t)
	clauses := strings.Split(list, ",")
	if len(clauses) < 2 {
		t.Fatalf("boundary example list split into %d clause(s) (%q) — expected multiple comma-delimited "+
			"examples; a change to the sentence's punctuation broke this control's extraction", len(clauses), list)
	}

	if unqualified := unqualifiedFabricationClauses(clauses); len(unqualified) > 0 {
		t.Errorf("the BOUNDARY parenthetical names a fabrication example with no \"closed\"/\"narrowed\" "+
			"qualifier in the same clause, so it reads as a currently-live example when it may not be: %v",
			unqualified)
	}
}

// TestUnqualifiedFabricationClauses_RealCheckLogic is the synthetic proof
// that unqualifiedFabricationClauses (the exact function the control above
// runs) actually discriminates. It feeds the real check loop a clause shape
// mirroring the ACTUAL post-fix wording (below), where the still-open M5
// residual fabrication and the closed "ADD CONSTRAINT" fabrication share ONE
// comma clause, exactly as COVERAGE.md's already-corrected wording joins
// them with an em dash. This is the same "exercise the real check loop with
// fabricated input" shape TestManifest_UndeclaredRule_Fails and
// TestManifest_UnansweredPRDPromise_Fails use for Controls A and D.
func TestUnqualifiedFabricationClauses_RealCheckLogic(t *testing.T) {
	clauses := []string{
		" Pagila's film.fulltext phantom index",
		" the residual paren-type fabrication (still open, SQL-DDL known limit 5) — the closed \"ADD  CONSTRAINT\" double-space fabrication",
		" and the missing-comma-before-PRIMARY-KEY wrong composite key",
		" all documented in the SQL-DDL known limits below",
	}

	got := unqualifiedFabricationClauses(clauses)
	want := []string{"Pagila's film.fulltext phantom index"}

	if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
		t.Fatalf("unqualifiedFabricationClauses(%v) = %v, want %v — the check must flag the unqualified "+
			"\"phantom\" clause and ONLY that clause: not the merged residual/closed clause (a \"closed\" "+
			"qualifier appears somewhere in it), and not the composite-key clause (names no fabrication at "+
			"all)", clauses, got, want)
	}
}
