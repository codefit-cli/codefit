package dbcoverage_test

import (
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
// enforcement test fails, naming the missing ID") — proven directly against
// containsWholeToken/manifestText rather than by mutating production All().
func TestManifest_UndeclaredRule_Fails(t *testing.T) {
	if containsWholeToken(manifestText(), "DB-999") {
		t.Fatal("DB-999 is not a real rule ID and must not appear in the manifest — fixture sanity check failed")
	}
}
