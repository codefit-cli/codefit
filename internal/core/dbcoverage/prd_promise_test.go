package dbcoverage_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/dbcoverage"
)

// Control D — the manifest answers for every capability the PRD promises
// (spec docs/specs/coverage-manifest-completeness.md).
//
// Controls A and B both look OUTWARD FROM THE CODE: A asks "is every registered
// rule declared?", B asks "does every declared rule exist?". Neither looks
// INWARD FROM THE PRD, which CLAUDE.md names as the project's source of scope.
// So a capability the PRD promises that was never built and never declared
// absent passes both — invisible from either end. That asymmetry is how seven
// promised IDs reached main undeclared.
//
// This file lives in package dbcoverage_test (EXTERNAL), like the rest of the
// controls, so dbcoverage stays a pure leaf: the PRD is a FILE this test opens,
// never an import, and nothing here is in dbcoverage's non-test dependency set.

// prdPath is the PRD, relative to THIS PACKAGE'S DIRECTORY — `go test` runs a
// package's tests with that directory as the working directory.
const prdPath = "../../../docs/PRD-codefit-v1.4.md"

// promisedIDs derives, MECHANICALLY, every DB/DW rule ID the PRD names, using
// the SAME ruleIDToken regexp Control B uses, so "rule-ID-shaped token" has one
// definition in this package and cannot drift between the two controls.
//
// There is deliberately NO hand-maintained list of "IDs the PRD promises" (spec
// R2). A second hand-maintained list is the same failure mode one level down: it
// would drift from the PRD exactly the way the manifest drifted, and it would
// pass its own test while doing so. Control A holds to this standard already
// (registeredIDs() derives from All(), never from a literal).
//
// CONSEQUENCE, chosen rather than discovered: EDITING THE PRD CAN FAIL THIS
// TEST. That is intended. Adding a rule ID to the PRD is a promise, and a
// promise with no manifest answer is the defect this control exists to catch.
func promisedIDs(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(prdPath))
	if err != nil {
		// A PRD that cannot be read yields an EMPTY promised set, which would
		// pass this control vacuously. Fail loudly instead.
		t.Fatalf("reading the PRD at %s: %v — the promised set is derived from it, "+
			"so an unreadable PRD is a broken control, never a pass", prdPath, err)
	}
	seen := map[string]bool{}
	var ids []string
	for _, id := range ruleIDToken.FindAllString(string(raw), -1) {
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// answerText is every prose surface the manifest exposes — the three answers R1
// allows, joined:
//
//	(1) covered — Deterministic() union Reasoning(). A REGISTERED rule is
//	    answered here, because Control A already forces every registered rule to
//	    have an entry in one of these two.
//	(2) declared absent — NotCovered().
//	(3) delivered under another identifier — DeliveredElsewhere().
//
// Silence is not a fourth answer.
func answerText() string {
	var all []string
	all = append(all, proseOf(dbcoverage.Deterministic())...)
	all = append(all, proseOf(dbcoverage.Reasoning())...)
	all = append(all, proseOf(dbcoverage.NotCovered())...)
	all = append(all, proseOf(dbcoverage.DeliveredElsewhere())...)
	return strings.Join(all, "\n")
}

// unansweredIDs runs Control D's REAL check loop over ids and returns the ones
// the manifest answers in none of the three ways. Both the control and its
// negative scenario call THIS function, so the negative exercises the real
// check rather than a description of it.
//
// MATCHING IS PLAIN SUBSTRING CONTAINMENT, not the whole-token match Controls A
// and B use, and the difference is deliberate. A PRD PARENT id must be answered
// by the CHILDREN it shipped as: the PRD promises DB-011 (duplicate/redundant
// indexes) and codefit shipped it split into DB-011a and DB-011b, so the prose
// spells only the children. Whole-token matching would report DB-011 as
// unanswered, which is false — the capability is declared, under two narrower
// ids.
//
// THE LIMIT THIS ACCEPTS, stated rather than hidden: a promised id that is a
// strict PREFIX of a longer one would be answered by the longer one's entry
// (a hypothetical DB-05 would be "answered" by DB-050's prose). No id in the
// PRD or the rule set has that shape, and the alternative — whole-token
// matching plus a hand-maintained parent→children alias table — reintroduces
// exactly the hand-maintained list R2 forbids.
func unansweredIDs(ids []string) []string {
	text := answerText()
	var missing []string
	for _, id := range ids {
		if !strings.Contains(text, id) {
			missing = append(missing, id)
		}
	}
	return missing
}

// TestManifest_EveryPRDPromisedRule_IsAnswered is Control D itself.
//
// WHAT IT GUARANTEES, and nothing more: CORRESPONDENCE — that an answer EXISTS
// for every id the PRD promises. It does NOT guarantee ACCURACY. A NotCovered()
// entry that misdescribes why a rule is absent, or a DeliveredElsewhere() entry
// that names the wrong deliverer, passes this control. That is the same limit
// Control A states about itself, and claiming more would be the over-promising
// manifest this whole effort exists to kill (spec R4).
func TestManifest_EveryPRDPromisedRule_IsAnswered(t *testing.T) {
	ids := promisedIDs(t)
	missing := unansweredIDs(ids)
	if len(missing) > 0 {
		t.Errorf("the PRD promises %d DB/DW rule ids; %d are answered by NOTHING in the manifest: %v\n"+
			"Every promised id needs one of the three answers: registered (so Control A gives it a "+
			"Deterministic()/Reasoning() entry), named in NotCovered() with its reason, or named in "+
			"DeliveredElsewhere() with what actually delivers it. Silence is a hole the agent cannot see.",
			len(ids), len(missing), missing)
	}
}

// TestPRDPromisedSet_IsNotVacuous is the positive control: if PRD extraction
// yields nothing, Control D above passes over an empty loop and guarantees
// nothing. This is the same guard TestManifest_CurrentRuleSet_Passes provides
// for Control A's mechanical derivation.
func TestPRDPromisedSet_IsNotVacuous(t *testing.T) {
	ids := promisedIDs(t)
	if len(ids) == 0 {
		t.Fatal("promisedIDs() returned nothing — the mechanical derivation from the PRD is broken, " +
			"and Control D would pass vacuously over an empty set")
	}
}

// TestManifest_UnansweredPRDPromise_Fails is Control D's negative scenario. It
// feeds the REAL check loop (unansweredIDs, the same function the control runs)
// the derived promised set plus a FABRICATED id that no manifest entry can
// possibly mention, and asserts that id is reported as the ONLY unanswered one.
//
// The "only" is what makes it a proof rather than a coincidence: it fires on the
// fabricated id specifically, not as a side effect of some other, real gap. It
// is the same shape as TestManifest_UndeclaredRule_Fails does for Control A.
func TestManifest_UnansweredPRDPromise_Fails(t *testing.T) {
	ids := append(append([]string(nil), promisedIDs(t)...), "DB-998")
	missing := unansweredIDs(ids)
	if len(missing) != 1 || missing[0] != "DB-998" {
		t.Fatalf("missing = %v, want exactly [DB-998] — the negative scenario must fire on the "+
			"fabricated promised id and nothing else", missing)
	}
}

// TestManifest_EachOfTheThreeAnswers_SatisfiesTheControl proves R1's three
// answers ONE AT A TIME: each is independently enough to answer a promised id,
// and each is carried by the bucket it belongs to.
//
// The ids are real, not fabricated, so this also locks which bucket answers each
// of them today.
func TestManifest_EachOfTheThreeAnswers_SatisfiesTheControl(t *testing.T) {
	registered := map[string]bool{}
	for _, id := range registeredIDs() {
		registered[id] = true
	}
	notCovered := strings.Join(proseOf(dbcoverage.NotCovered()), "\n")
	delivered := strings.Join(proseOf(dbcoverage.DeliveredElsewhere()), "\n")

	t.Run("registered", func(t *testing.T) {
		// DB-002 (multivalued columns) is a registered rule, so Control A
		// already forces its Deterministic()/Reasoning() entry.
		const id = "DB-002"
		if !registered[id] {
			t.Fatalf("%s must be a registered rule for this to be the REGISTERED answer", id)
		}
		if got := unansweredIDs([]string{id}); len(got) != 0 {
			t.Errorf("%s is registered and must be answered; unanswered = %v", id, got)
		}
	})

	t.Run("declared absent in NotCovered", func(t *testing.T) {
		// DB-012 (never-used index) is permanently not covered: it needs
		// runtime telemetry codefit never has. It is NOT registered, so
		// NotCovered() is the only thing that can answer it.
		const id = "DB-012"
		if registered[id] {
			t.Fatalf("%s must NOT be registered for this to be the NOT-COVERED answer", id)
		}
		if !containsWholeToken(notCovered, id) {
			t.Fatalf("%s must be named in NotCovered() for this to be the NOT-COVERED answer", id)
		}
		if got := unansweredIDs([]string{id}); len(got) != 0 {
			t.Errorf("%s is declared absent and must be answered; unanswered = %v", id, got)
		}
	})

	t.Run("delivered under another identifier", func(t *testing.T) {
		// DB-201 (N+1) ships, as the provider's nplus1 surface category — not
		// as a DB/DW rule with that id. It is neither registered nor absent, so
		// only the third bucket can answer it honestly. (It is ALSO
		// cross-referenced from Reasoning(), which is what makes Control B's
		// amendment load-bearing; this subtest asserts the third bucket carries
		// it, not that the third bucket is its only mention.)
		const id = "DB-201"
		if registered[id] {
			t.Fatalf("%s must NOT be registered — N+1 is code-derived and belongs to the provider", id)
		}
		if containsWholeToken(notCovered, id) {
			t.Fatalf("%s must NOT be in NotCovered() — the capability ships, calling it absent is a lie", id)
		}
		if !containsWholeToken(delivered, id) {
			t.Fatalf("%s must be named in DeliveredElsewhere() for this to be the DELIVERED-ELSEWHERE answer", id)
		}
		if got := unansweredIDs([]string{id}); len(got) != 0 {
			t.Errorf("%s is delivered under another identifier and must be answered; unanswered = %v", id, got)
		}
	})
}
