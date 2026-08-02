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

// typeLookupKey's two steps, unit-level, because ONE of its guards has no
// observable effect through the TypeMap and would otherwise be untestable —
// see the WHOLE-IDENTIFIER cases below.
//
// The parser-level behavior is locked over real DDL in
// delimited_type_names_test.go; this file locks the string transform itself, so
// the guards can each be broken on purpose and seen to fail.
func TestTypeLookupKey(t *testing.T) {
	cases := []struct{ raw, want, why string }{
		// typeBase's existing work, unchanged.
		{"VARCHAR(30)", "VARCHAR", "length/precision stripped"},
		{"tag[]", "tag", "PostgreSQL array marker stripped as a SUFFIX"},
		{"integer", "integer", "a bare name passes through"},

		// The unwrap: exactly ONE canonical quoted identifier.
		{`"int"`, "int", "canonicalized [int] / `int` / \"int\" all arrive as this"},
		{`"nvarchar"(50)`, "nvarchar", "parens stripped first, then unwrapped"},
		{`"text"[]`, "text", "array marker stripped first, then unwrapped — the two compose"},

		// WHOLE-IDENTIFIER GUARD. Each of these is NOT one quoted identifier, so
		// it is returned untouched rather than half-stripped into a fragment.
		// None of them maps to a db.Type either way, which is precisely why the
		// guard cannot be falsified through mapType and is asserted here.
		{`"dbo"."MyType"`, `"dbo"."MyType"`, "a schema-qualified user type is two identifiers, not one"},
		{`"a""b"`, `"a""b"`, "a doubled-quote escape means the interior is not readable whole"},
		{`"unterminated`, `"unterminated`, "no closing delimiter"},
		{`trailing"`, `trailing"`, "no opening delimiter"},
		{`"`, `"`, "a lone delimiter is not a wrapped name"},
		{`""`, "", "an empty quoted identifier unwraps to empty, which maps to TypeUnknown"},
	}
	for _, tc := range cases {
		if got := typeLookupKey(tc.raw); got != tc.want {
			t.Errorf("typeLookupKey(%q) = %q, want %q — %s", tc.raw, got, tc.want, tc.why)
		}
	}
}
