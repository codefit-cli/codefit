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
	all = append(all, dbcoverage.Deterministic()...)
	all = append(all, dbcoverage.Reasoning()...)
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

// TestManifest_NoPhantomCapability is Control B (secondary, design SS9):
// every rule-ID-shaped token the manifest MENTIONS in Deterministic()/
// Reasoning() must be either a REGISTERED rule, or ALSO named in
// NotCovered() — the legitimate way to describe a not-yet-built rule (e.g.
// DW-020/DW-021, which Reasoning names as future work and NotCovered
// explicitly declares not covered). A token that is neither is a phantom
// capability: prose describing a rule that does not exist and is not
// declared absent either.
func TestManifest_NoPhantomCapability(t *testing.T) {
	registered := map[string]bool{}
	for _, id := range registeredIDs() {
		registered[id] = true
	}
	notCovered := strings.Join(dbcoverage.NotCovered(), "\n")

	seen := map[string]bool{}
	for _, id := range ruleIDToken.FindAllString(manifestText(), -1) {
		if seen[id] || registered[id] {
			continue
		}
		seen[id] = true
		if !containsWholeToken(notCovered, id) {
			t.Errorf("manifest mentions %q in Deterministic()/Reasoning() — it is neither a registered rule "+
				"nor named in NotCovered() — a phantom capability", id)
		}
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
