package dbcoverage_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/core/dbcoverage"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
)

// db-model-completeness-contract — the coverage manifest correspondence test
// (proposal SS3/SS5, spec "Domain: dbcoverage Enforcement Test"). Before this
// file, internal/core/dbcoverage/ had ZERO test files — the mechanical cause
// of two BLOCKING defects that shipped on main (the manifest denied the DW
// family and the DB-010/DB-013 cross AFTER both were built). This is package
// dbcoverage_test (EXTERNAL), so dbcoverage stays a pure leaf: it is not in
// dbcoverage's own non-test dependency set, and dbrules/dwrules/crossrules do
// not import dbcoverage either (verified — no cycle either way).

// registeredIDs mechanically derives every rule ID across the three roots
// that expose All()+ID() — dbrules, dwrules, crossrules. No hand-maintained
// second list: adding a rule to any All() extends this set automatically.
func registeredIDs() []string {
	var ids []string
	for _, r := range dbrules.All() {
		ids = append(ids, r.ID())
	}
	for _, r := range dwrules.All() {
		ids = append(ids, r.ID())
	}
	for _, r := range crossrules.All() {
		ids = append(ids, r.ID())
	}
	return ids
}

// manifestText is every prose entry the manifest exposes, joined — Control A
// checks CORRESPONDENCE (an entry exists), never accuracy of that entry.
func manifestText() string {
	var all []string
	all = append(all, proseOf(dbcoverage.Deterministic())...)
	all = append(all, proseOf(dbcoverage.Reasoning())...)
	return strings.Join(all, "\n")
}

// containsWholeToken reports whether id appears in text as a whole token —
// not as a strict prefix of a longer ID (e.g. "DB-011" must NOT be satisfied
// by a manifest that only mentions "DB-011a"). The byte immediately
// following a match must not be alphanumeric.
func containsWholeToken(text, id string) bool {
	start := 0
	for {
		i := strings.Index(text[start:], id)
		if i < 0 {
			return false
		}
		idx := start + i
		end := idx + len(id)
		if end >= len(text) || !isAlnum(text[end]) {
			return true
		}
		start = idx + 1
	}
}

func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// TestManifest_EveryRegisteredRule_HasACoveredEntry is Control A (the
// PRIMARY, mechanical control): every rule ID registered in dbrules.All() /
// dwrules.All() / crossrules.All() must have a matching manifest entry in
// Deterministic() union Reasoning(). This is the control that would have
// caught BOTH blocking v0.2.5-alpha.1 defects: the DW family registered
// while the manifest said "not yet built", and DB-010/DB-013 shipped while
// the manifest denied the cross.
func TestManifest_EveryRegisteredRule_HasACoveredEntry(t *testing.T) {
	text := manifestText()
	for _, id := range registeredIDs() {
		if !containsWholeToken(text, id) {
			t.Errorf("rule %q is registered (All()) but has no matching entry in "+
				"dbcoverage.Deterministic()/Reasoning() — an undeclared capability", id)
		}
	}
}

// TestManifest_CurrentRuleSet_Passes is the explicit "current rule set
// passes" scenario from the spec — a positive control proving the test
// itself is not vacuously green (e.g. from an empty registeredIDs()).
func TestManifest_CurrentRuleSet_Passes(t *testing.T) {
	ids := registeredIDs()
	if len(ids) == 0 {
		t.Fatal("registeredIDs() returned nothing — the mechanical derivation is broken, this test would pass vacuously")
	}
	text := manifestText()
	for _, id := range ids {
		if !containsWholeToken(text, id) {
			t.Fatalf("today's registered rule set must pass: %q has no manifest entry", id)
		}
	}
}

// TestManifest_UndeclaredRule_Fails is the spec's negative scenario ("Given
// a rule ID registered in one of the four roots with no manifest entry, the
// enforcement test fails, naming the missing ID").
//
// sdd-verify W5 (obs #1279): the original version only asserted "DB-999 is
// absent from the manifest today" — a fixture sanity check, not a proof that
// Control A's own CHECK LOGIC fires on an undeclared ID. This version feeds
// the exact loop Control A runs (registeredIDs() + containsWholeToken against
// manifestText()) a FABRICATED extra ID appended to the real registered set —
// exercising the real check, not a description of it — and asserts it is the
// ONLY one reported missing (proving the negative fires on the fabricated ID
// specifically, not as a side effect of some other, real gap).
func TestManifest_UndeclaredRule_Fails(t *testing.T) {
	ids := append(append([]string(nil), registeredIDs()...), "DB-999")
	text := manifestText()
	var missing []string
	for _, id := range ids {
		if !containsWholeToken(text, id) {
			missing = append(missing, id)
		}
	}
	if len(missing) != 1 || missing[0] != "DB-999" {
		t.Fatalf("missing = %v, want exactly [DB-999] — the negative scenario must fire on the fabricated "+
			"undeclared ID and nothing else", missing)
	}
}

// ruleIDToken matches an "XX-###" or "XX-###a" rule-ID-shaped token anywhere
// in prose — used by Control B to find every ID the manifest MENTIONS,
// independent of whether it is registered.
var ruleIDToken = regexp.MustCompile(`\bD[BW]-[0-9]+[a-z]?\b`)

// mentionedTokenIsAnswered is Control B's per-token predicate, extracted so the
// control and its negative scenario run the SAME logic rather than two
// descriptions of it. A rule-ID-shaped token the manifest mentions is legitimate
// when it is answered in one of the THREE ways R1 allows: it is REGISTERED, it
// is named in NotCovered(), or it is named in DeliveredElsewhere().
//
// THE THIRD BRANCH IS THE AMENDMENT (spec R3), and it was forced by DB-201. N+1
// ships — as the provider's nplus1 surface, not as a DB/DW rule — so the DB
// manifest names DB-201 in Reasoning() to point at it. Before this branch
// existed, that mention read as a phantom: not registered, and rightly not in
// NotCovered(), because calling a shipped capability absent is a lie. The bucket
// and this branch are one change; either alone is wrong (the bucket alone leaves
// the tree red, the branch alone widens Control B's hole).
//
// What did NOT change: a token in NO bucket is still a phantom capability, and
// still fails. TestManifest_PhantomCapability_StillFailsAfterTheAmendment holds
// that line.
func mentionedTokenIsAnswered(id string) bool {
	for _, registered := range registeredIDs() {
		if registered == id {
			return true
		}
	}
	if containsWholeToken(strings.Join(proseOf(dbcoverage.NotCovered()), "\n"), id) {
		return true
	}
	return containsWholeToken(strings.Join(proseOf(dbcoverage.DeliveredElsewhere()), "\n"), id)
}

// TestManifest_NoPhantomCapability is Control B (secondary, design SS9):
// every rule-ID-shaped token the manifest MENTIONS in Deterministic()/
// Reasoning() must be either a REGISTERED rule, or ALSO named in
// NotCovered() — the legitimate way to describe a not-yet-built rule (DB-012
// and the permanently-dropped DW-022 candidate are the standing examples) —
// or named in DeliveredElsewhere(), for a promised id whose capability ships
// under another identifier (DB-201 is the standing example).
// DW-020 and DW-021 both used to sit on the NotCovered() branch and both took
// the REGISTERED one when they shipped, in S4 and S3: they are registered rules
// now, and the manifest describes them as capabilities rather than as gaps. A
// token in none of the three is a phantom capability: prose describing a rule
// that does not exist, is not declared absent, and is not delivered by anything
// else either.
func TestManifest_NoPhantomCapability(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range ruleIDToken.FindAllString(manifestText(), -1) {
		if seen[id] {
			continue
		}
		seen[id] = true
		if !mentionedTokenIsAnswered(id) {
			t.Errorf("manifest mentions %q in Deterministic()/Reasoning() — it is not a registered rule, "+
				"not named in NotCovered() and not named in DeliveredElsewhere() — a phantom capability", id)
		}
	}
}

// TestManifest_PhantomCapability_StillFailsAfterTheAmendment is the guard on the
// amendment itself. Widening Control B to accept a third bucket is one edit away
// from widening it to accept ANYTHING, and a control that accepts anything
// reports nothing while still looking green.
//
// Both directions are asserted against the REAL predicate the control runs:
// DB-201 (answered by DeliveredElsewhere() and by nothing else) must now be
// ACCEPTED, and a fabricated token that appears in no bucket at all must still
// be REJECTED. The mutation that proves it: make mentionedTokenIsAnswered
// return true unconditionally — the second assertion fails.
func TestManifest_PhantomCapability_StillFailsAfterTheAmendment(t *testing.T) {
	if !mentionedTokenIsAnswered("DB-201") {
		t.Errorf("Control B must ACCEPT DB-201: it is mentioned in Reasoning() and answered by " +
			"DeliveredElsewhere() — reporting a phantom for a capability that ships is the defect the " +
			"amendment exists to fix")
	}
	if mentionedTokenIsAnswered("DB-997") {
		t.Errorf("Control B must still REJECT a token in no bucket at all — DB-997 is registered nowhere, " +
			"named in NotCovered() nowhere and named in DeliveredElsewhere() nowhere, so it is a true phantom")
	}
}

// Control C (design SS9) — DELIBERATELY NOT IMPLEMENTED, stated rather than
// faked.
//
// internal/core/paradigm/ registers NO rules: it exposes no All() and no
// ID(), unlike dbrules/dwrules/crossrules. Controls A and B above both
// depend on that shape (a rule set to iterate, IDs to search for) and it does
// not exist here. A rule↔manifest correspondence test is IMPOSSIBLE for this
// root, not merely unwritten — claiming otherwise would be exactly the kind
// of over-promising manifest this whole enforcement effort exists to kill.
//
// The design's OPTIONAL fallback — adding paradigm.AllRoles()/AllParadigms()
// returning the closed Role/Paradigm sets, then asserting every value's
// string appears in Reasoning() — was considered and NOT implemented here.
// It would not eliminate the drift Control A/B close for the other three
// roots; it would only MOVE it three lines, from the const block that
// defines Role/Paradigm to a second hand-maintained AllRoles()/AllParadigms()
// function that could itself drift from the const block. That is a real,
// if smaller, improvement (a missing entry becomes visible in a 3-line
// review diff instead of 400 lines away) — but it is not the same guarantee
// Control A gives, and dressing it up as equivalent would be dishonest. Per
// architect direction (2026-07-30): "a test that pretends to a guarantee it
// does not provide is not acceptable" — so this control is left unwritten,
// documented here, rather than shipped as a false mechanical guarantee.
