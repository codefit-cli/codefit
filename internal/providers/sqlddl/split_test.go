package sqlddl

import "testing"

func texts(sts []stmt) []string {
	out := make([]string, len(sts))
	for i, s := range sts {
		out[i] = s.text
	}
	return out
}

func TestSplit_BasicSemicolons(t *testing.T) {
	got := texts(split([]byte("CREATE TABLE a (id int); CREATE TABLE b (id int);")))
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
	got := texts(split([]byte(src)))
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
	got := texts(split([]byte(src)))
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
	got := texts(split([]byte(src)))
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (semicolons in comments/strings ignored): %q", len(got), got)
	}
}

func TestSplit_TracksStartLine(t *testing.T) {
	src := "SELECT 1;\n\nCREATE TABLE t (id int);"
	sts := split([]byte(src))
	if len(sts) != 2 {
		t.Fatalf("want 2, got %d", len(sts))
	}
	if sts[1].line != 3 {
		t.Errorf("second statement start line = %d, want 3", sts[1].line)
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
