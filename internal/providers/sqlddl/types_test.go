package sqlddl

import "testing"

// C4: splitTypeAndMods must strip a trailing "(...)" from the candidate word
// BEFORE the Modifiers lookup, so a paren-bearing modifier keyword (e.g. a
// future T-SQL "IDENTITY(1,1)") still ends the type expression instead of
// being swallowed into it. This is shared, dialect-free code — exercised here
// via a synthetic Modifiers set (no dialect needs a paren-bearing modifier
// yet; this locks the mechanism design §3 calls for ahead of Unit F).
func TestSplitTypeAndMods_StripsParensBeforeModifierLookup(t *testing.T) {
	mods := map[string]bool{"FOO": true, "NOT": true}
	typeExpr, rest := splitTypeAndMods("INT FOO(1,2) NOT NULL", mods)
	if typeExpr != "INT" {
		t.Errorf("typeExpr = %q, want %q (FOO(1,2) must stop the type expression like a bare FOO would)", typeExpr, "INT")
	}
	if rest != "FOO(1,2) NOT NULL" {
		t.Errorf("mods = %q, want %q", rest, "FOO(1,2) NOT NULL")
	}
}

// Sanity: a bare (no-parens) modifier keyword still works as before.
func TestSplitTypeAndMods_BareModifierStillWorks(t *testing.T) {
	mods := map[string]bool{"UNSIGNED": true}
	typeExpr, rest := splitTypeAndMods("INT UNSIGNED", mods)
	if typeExpr != "INT" {
		t.Errorf("typeExpr = %q, want %q", typeExpr, "INT")
	}
	if rest != "UNSIGNED" {
		t.Errorf("mods = %q, want %q", rest, "UNSIGNED")
	}
}

// Sanity: length/precision parens on the TYPE ITSELF (not a modifier) are
// untouched — NUMERIC(10,2) must not be mistaken for a modifier just because
// it has trailing parens.
func TestSplitTypeAndMods_TypeParensNotMistakenForModifier(t *testing.T) {
	mods := map[string]bool{"NOT": true}
	typeExpr, rest := splitTypeAndMods("NUMERIC(10,2) NOT NULL", mods)
	if typeExpr != "NUMERIC(10,2)" {
		t.Errorf("typeExpr = %q, want %q", typeExpr, "NUMERIC(10,2)")
	}
	if rest != "NOT NULL" {
		t.Errorf("mods = %q, want %q", rest, "NOT NULL")
	}
}
