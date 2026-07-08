package sqlddl

import "testing"

// pg is the shared Postgres dialect used by the internal tokenizer/reducer
// tests, which exercise split()/newBuilder() directly rather than through the
// public Parser.
var pg = Postgres()

func texts(sts []stmt) []string {
	out := make([]string, len(sts))
	for i, s := range sts {
		out[i] = s.text
	}
	return out
}

func TestSplit_BasicSemicolons(t *testing.T) {
	got := texts(split([]byte("CREATE TABLE a (id int); CREATE TABLE b (id int);"), &pg))
	if len(got) != 2 {
		t.Fatalf("got %d statements, want 2: %q", len(got), got)
	}
}

func TestSplit_DollarQuotedBlockNotCut(t *testing.T) {
	// The semicolons INSIDE the $$ block must not split it.
	src := `DO $$
DECLARE x int;
BEGIN
  SELECT 1; SELECT 2;
END $$;
CREATE TABLE after_do (id int);`
	got := texts(split([]byte(src), &pg))
	if len(got) != 2 {
		t.Fatalf("got %d statements, want 2 (DO block is one): %q", len(got), got)
	}
	if got[1] != "CREATE TABLE after_do (id int)" {
		t.Errorf("second statement = %q, want the CREATE TABLE", got[1])
	}
}

func TestSplit_TaggedDollarQuote(t *testing.T) {
	src := `CREATE FUNCTION f() RETURNS int AS $func$ BEGIN RETURN 1; END; $func$ LANGUAGE plpgsql;
SELECT 1;`
	got := texts(split([]byte(src), &pg))
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (function body is one stmt): %q", len(got), got)
	}
}

func TestSplit_CommentsAndStrings(t *testing.T) {
	src := `-- a leading comment with ; semicolon
CREATE TABLE t (
  name varchar(20) DEFAULT 'a;b', -- inline comment ;
  /* block ; comment */ code int
); SELECT 'O''Brien';`
	got := texts(split([]byte(src), &pg))
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (semicolons in comments/strings ignored): %q", len(got), got)
	}
}

func TestSplit_TracksStartLine(t *testing.T) {
	src := "SELECT 1;\n\nCREATE TABLE t (id int);"
	sts := split([]byte(src), &pg)
	if len(sts) != 2 {
		t.Fatalf("want 2, got %d", len(sts))
	}
	if sts[1].line != 3 {
		t.Errorf("second statement start line = %d, want 3", sts[1].line)
	}
}

func TestSplit_QuotedIdentifierCanonicalizedIdentity(t *testing.T) {
	// PostgreSQL's own quote pair ('"','"') round-trips byte-identical —
	// the identity transform the design promises for the PG no-regression gate.
	src := `CREATE TABLE "Users" ("Id" int, "a""b" int);`
	got := texts(split([]byte(src), &pg))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := `CREATE TABLE "Users" ("Id" int, "a""b" int)`
	if got[0] != want {
		t.Errorf("got %q, want %q (PG canonicalization must be identity)", got[0], want)
	}
}

func TestSplit_UnterminatedQuotedIdentifierNoSyntheticClose(t *testing.T) {
	// EOF hits before the closing '"' is found. The pre-refactor tokenizer
	// only wrote a closing '"' when it actually matched one in the source —
	// it never invents one. Regression: the refactored scanIdentQuoted wrote
	// a trailing '"' unconditionally, breaking the PG byte-identical contract
	// for malformed/truncated input.
	src := `CREATE TABLE "unterminated (id int);`
	got := texts(split([]byte(src), &pg))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := `CREATE TABLE "unterminated (id int);`
	if got[0] != want {
		t.Errorf("got %q, want %q (no synthetic closing quote on EOF)", got[0], want)
	}
}

func TestSplit_MySQLBacktickIdentifierCanonicalized(t *testing.T) {
	// MySQL backtick-quoted identifiers are re-emitted as canonical ANSI
	// "..." — the seam that keeps reduce.go dialect-free (design §2). Unlike
	// the PG '"' pair, this is a genuine delimiter SWAP (unescaping), not a
	// byte-length-preserving identity transform: assert the canonical value,
	// not byte-length equality.
	mysql := MySQL()
	src := "CREATE TABLE `users` (`order` int);"
	got := texts(split([]byte(src), &mysql))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := `CREATE TABLE "users" ("order" int)`
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestSplit_MySQLHashCommentSkippedLikeDashDash(t *testing.T) {
	mysql := MySQL()
	src := "# a leading hash comment with ; semicolon\nCREATE TABLE t (id int);"
	got := texts(split([]byte(src), &mysql))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := "CREATE TABLE t (id int)"
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestSplit_MySQLDoubleQuoteIsStringNotIdentifier(t *testing.T) {
	// MySQL default (ANSI_QUOTES off): " opens a STRING literal, not a
	// quoted identifier — must not be mistaken for identifier quoting.
	mysql := MySQL()
	src := `SELECT * FROM t WHERE name = "O'Brien";`
	got := texts(split([]byte(src), &mysql))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := `SELECT * FROM t WHERE name = "O'Brien"`
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestSplit_MySQLDashDashRequiresBoundary(t *testing.T) {
	// MySQL's "--" opens a line comment ONLY when followed by whitespace,
	// a control char, or end-of-line/EOF (unlike PostgreSQL, where "--" is
	// unconditional). "--1" with no boundary after the second '-' is NOT a
	// comment: it must survive verbatim, including the terminating ';'.
	mysql := MySQL()
	src := "SELECT 1--1 FROM t;"
	got := texts(split([]byte(src), &mysql))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := "SELECT 1--1 FROM t"
	if got[0] != want {
		t.Errorf("got %q, want %q (MySQL '--' without a trailing boundary must NOT be treated as a comment)", got[0], want)
	}
}

func TestSplit_MySQLDashDashWithBoundaryIsComment(t *testing.T) {
	// Confirms the normal MySQL "-- " comment case still works: "--"
	// followed by whitespace does open a line comment.
	mysql := MySQL()
	src := "CREATE TABLE t (id INT) -- note\n;"
	got := texts(split([]byte(src), &mysql))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := "CREATE TABLE t (id INT)"
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestSplit_MySQLBacktickDoublingEscape(t *testing.T) {
	// Backtick doubling is the MySQL escape for a literal backtick inside a
	// quoted identifier: two backticks mean one literal backtick. This is a
	// genuine unescaping transform, not byte-length-preserving — assert the
	// canonical value.
	mysql := MySQL()
	src := "SELECT * FROM `a``b`;"
	got := texts(split([]byte(src), &mysql))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := `SELECT * FROM "a` + "`" + `b"`
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestSplit_SQLServerBracketIdentifierCanonicalized(t *testing.T) {
	// T-SQL bracket-quoted identifiers, INCLUDING schema-qualified names, are
	// re-emitted as canonical ANSI "..." per identifier part — the seam that
	// keeps reduce.go's regexes and normalizeName dialect-free (design §2).
	// The schema qualifier '.' between the two bracket pairs is a plain
	// literal byte, not part of either quoted identifier, so it survives
	// untouched between the two canonicalized parts.
	sqlserver := SQLServer()
	src := "CREATE TABLE [dbo].[Users] ([Id] int);"
	got := texts(split([]byte(src), &sqlserver))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := `CREATE TABLE "dbo"."Users" ("Id" int)`
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestSplit_SQLServerBracketDoublingEscape(t *testing.T) {
	// ']]' inside a bracket-quoted identifier is T-SQL's escape for one
	// literal ']' — the asymmetric-quote analogue of MySQL's backtick
	// doubling. Assert the canonical VALUE (not byte-length identity): this
	// is a genuine unescaping transform.
	sqlserver := SQLServer()
	src := "SELECT * FROM [Weird]]Name];"
	got := texts(split([]byte(src), &sqlserver))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := `SELECT * FROM "Weird]Name"`
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestSplit_SQLServerDoubleQuoteIdentifierAlsoSupported(t *testing.T) {
	// T-SQL simultaneously supports BOTH bracket quoting and ANSI
	// double-quote identifier quoting (SET QUOTED_IDENTIFIER ON, the
	// default) — two IdentQuotes pairs active at once, the first dialect to
	// exercise that. '"' must still be scanned as an identifier here (not a
	// string), matching the second QuotePair, independent of the bracket pair.
	sqlserver := SQLServer()
	src := `CREATE TABLE "Users" ("Id" int);`
	got := texts(split([]byte(src), &sqlserver))
	if len(got) != 1 {
		t.Fatalf("got %d statements, want 1: %q", len(got), got)
	}
	want := `CREATE TABLE "Users" ("Id" int)`
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

func TestDollarTag(t *testing.T) {
	cases := map[string]string{
		"$$rest":     "$$",
		"$func$rest": "$func$",
		"$_x1$rest":  "$_x1$",
		"$notclosed": "", // no closing $ → not a dollar tag
		"plain":      "",
	}
	for in, want := range cases {
		got, ok := dollarTag(in, 0)
		if want == "" && ok {
			t.Errorf("dollarTag(%q) = %q, want no match", in, got)
		}
		if want != "" && got != want {
			t.Errorf("dollarTag(%q) = %q, want %q", in, got, want)
		}
	}
}
