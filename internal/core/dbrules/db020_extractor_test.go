package dbrules

import "testing"

// White-box tests of DB-020's own bounded SELECT-column-list primitives
// (db020.go) — kept separate from the black-box behavior tests in
// db020_test.go, which exercise the rule end-to-end via dbrules.Run.

func TestSplitProjectionItems_QuotedIdentifierWithCommaIsNotMisSplit(t *testing.T) {
	// architecture/unit-c-tsql-risk-reeval (obs #1056), finding R3: a
	// canonical ANSI double-quoted identifier may legally contain a ','.
	// reduce.go's splitTopLevelParts (single-quote-only) WOULD mis-split
	// here; this function must not.
	got := splitProjectionItems(`a."last, first" AS full_name, b.password`)
	if len(got) != 2 {
		t.Fatalf("splitProjectionItems = %d items, want 2 (comma INSIDE the quoted identifier must not split), got %+v", len(got), got)
	}
	if got[0].text != `a."last, first" AS full_name` {
		t.Errorf("item[0] = %q, want the quoted comma preserved intact", got[0].text)
	}
	if got[1].text != ` b.password` {
		t.Errorf("item[1] = %q, want ' b.password'", got[1].text)
	}
}

func TestSplitProjectionItems_ParenDepthNotBrokenByQuotedComma(t *testing.T) {
	got := splitProjectionItems(`CONCAT(a."x, y", b.z) AS combined, password`)
	if len(got) != 2 {
		t.Fatalf("splitProjectionItems = %d items, want 2, got %+v", len(got), got)
	}
}

func TestFindTopLevelKeyword_SkipsParenthesizedOccurrence(t *testing.T) {
	// CAST(x AS int)'s own AS is at depth 1 and must be skipped; the
	// top-level "AS name" must be the one found.
	idx, ok := findTopLevelKeyword(`CAST(x AS int) AS name`, "AS", 0)
	if !ok {
		t.Fatal("findTopLevelKeyword: want ok=true")
	}
	if got := `CAST(x AS int) AS name`[idx:]; got != "AS name" {
		t.Errorf("findTopLevelKeyword found %q, want the outer 'AS name'", got)
	}
}

func TestFindTopLevelKeyword_DoesNotMatchInsideWord(t *testing.T) {
	if _, ok := findTopLevelKeyword("FROMAGE", "FROM", 0); ok {
		t.Error(`findTopLevelKeyword("FROMAGE", "FROM") should not match — not a word boundary`)
	}
	if _, ok := findTopLevelKeyword("SELECTOR", "SELECT", 0); ok {
		t.Error(`findTopLevelKeyword("SELECTOR", "SELECT") should not match — not a word boundary`)
	}
}

func TestFindTopLevelKeyword_IgnoresOccurrenceInsideStringLiteral(t *testing.T) {
	if _, ok := findTopLevelKeyword(`x, 'a FROM b' , y FROM t`, "FROM", 0); !ok {
		t.Fatal("want the REAL top-level FROM to be found")
	}
	idx, _ := findTopLevelKeyword(`x, 'a FROM b' , y FROM t`, "FROM", 0)
	if got := `x, 'a FROM b' , y FROM t`[idx:]; got != "FROM t" {
		t.Errorf("found %q, want the FROM outside the string literal ('FROM t')", got)
	}
}

func TestExtractSelectColumns_TableStarIsNotWholeStar(t *testing.T) {
	// "SELECT a.*, b.password FROM ..." — a qualified star is not the bare
	// "SELECT *" whole-view-miss case, but a.* itself has no extractable
	// name (declared per-item miss), while b.password is still recognized.
	cols, ok := extractSelectColumns(`SELECT a.*, b.password FROM t a, t2 b`)
	if !ok {
		t.Fatal("extractSelectColumns: want ok=true (not a bare SELECT *)")
	}
	if len(cols) != 1 || cols[0].name != "password" {
		t.Errorf("cols = %+v, want exactly [password]", cols)
	}
}

func TestExtractSelectColumns_NoTopLevelFromIsDeclaredMiss(t *testing.T) {
	if _, ok := extractSelectColumns(`SELECT 1`); ok {
		t.Error("no top-level FROM at all: want ok=false, not a crash or a guess")
	}
}
