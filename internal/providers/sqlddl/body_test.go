package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Unit A (db-debt-views-and-nplus1, Phase 2.2): db.Body + per-dialect
// completeness signal. These tests lock the per-dialect body-capture contract
// from the spec (RF-03.6/§4) against the REAL tokenizer, not an assumption —
// the architect's binding condition (architecture/tsql-body-truncation-limit):
// a truncated body must NEVER be able to produce an affirmation, and that must
// be TESTED.

// TestBody_PostgresDollarQuoted_Complete — a PL/pgSQL function body wrapped in
// $$...$$ is captured WHOLE, including every internal ';' (split.go's
// DollarQuoting handling keeps it as one stmt) — Complete=true.
func TestBody_PostgresDollarQuoted_Complete(t *testing.T) {
	src := `CREATE FUNCTION f() RETURNS int AS $$
BEGIN
  UPDATE t SET x = 1;
  RETURN 1;
END;
$$ LANGUAGE plpgsql;`
	s := parse(t, src)
	if len(s.Procedures) != 1 {
		t.Fatalf("procedures = %d, want 1", len(s.Procedures))
	}
	body := s.Procedures[0].Body
	if !body.Complete {
		t.Errorf("Complete = false, want true (dollar-quoted body captures every internal ';'); Note=%q", body.Note)
	}
	if !strings.Contains(body.Text, "UPDATE t SET x = 1") || !strings.Contains(body.Text, "RETURN 1") {
		t.Errorf("Body.Text = %q, want it to contain BOTH internal statements verbatim", body.Text)
	}
}

// TestBody_PostgresSingleStatementView_Complete — a CREATE VIEW body is a
// single SELECT statement that cannot legally contain a top-level ';' in any
// dialect — always Complete, per the design's explicit "views are always
// Complete" rule.
func TestBody_PostgresSingleStatementView_Complete(t *testing.T) {
	src := `CREATE VIEW active_users AS SELECT id, email FROM users WHERE active = true;`
	s := parse(t, src)
	if len(s.Views) != 1 {
		t.Fatalf("views = %d, want 1", len(s.Views))
	}
	body := s.Views[0].Body
	if !body.Complete {
		t.Errorf("Complete = false, want true (a view body is never split); Note=%q", body.Note)
	}
	if !strings.Contains(body.Text, "SELECT id, email FROM users") {
		t.Errorf("Body.Text = %q, want the full SELECT verbatim", body.Text)
	}
}

// TestBody_MySQLDelimiterWrapped_Complete — a MySQL routine wrapped in
// DELIMITER //...// is captured WHOLE: the active-terminator override keeps
// every internal ';' from flushing the statement early — Complete=true.
func TestBody_MySQLDelimiterWrapped_Complete(t *testing.T) {
	src := `DELIMITER //
CREATE PROCEDURE p()
BEGIN
  UPDATE t SET x = 1;
  SELECT 1;
END //
DELIMITER ;`
	s := parseDialect(t, sqlddl.MySQL(), src)
	if len(s.Procedures) != 1 {
		t.Fatalf("procedures = %d, want 1", len(s.Procedures))
	}
	body := s.Procedures[0].Body
	if !body.Complete {
		t.Errorf("Complete = false, want true (DELIMITER-wrapped body captures every internal ';'); Note=%q", body.Note)
	}
	if !strings.Contains(body.Text, "UPDATE t SET x = 1") || !strings.Contains(body.Text, "SELECT 1") {
		t.Errorf("Body.Text = %q, want it to contain BOTH internal statements verbatim", body.Text)
	}
}

// TestBody_MySQLNoDelimiterSingleStatement_IncompleteWithNote — a MySQL
// trigger with no DELIMITER wrapper ends at the first bare ';', which is ALSO
// its own (only) internal statement's terminator. The tokenizer cannot tell
// "this ';' is the last one" from "this ';' is the first of several" from
// local state alone, so the conservative, honest call is Complete=false even
// though this particular body happens to be whole (design §4: "we accept the
// under-claim and declare it" — a false 'partial' only downgrades a rule to
// surface, never fabricates a wrong affirmation).
func TestBody_MySQLNoDelimiterSingleStatement_IncompleteWithNote(t *testing.T) {
	src := `CREATE TRIGGER trg BEFORE INSERT ON t FOR EACH ROW SET x = 1;`
	s := parseDialect(t, sqlddl.MySQL(), src)
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	body := s.Triggers[0].Body
	if body.Complete {
		t.Errorf("Complete = true, want false (no DELIMITER wrapper: cannot prove the body is whole); Text=%q", body.Text)
	}
	if body.Note == "" {
		t.Error("Note = \"\", want a non-empty reason when Complete is false")
	}
}

// TestBody_TSQLSingleStatement_Complete — a T-SQL trigger body with NO
// internal ';' at all (T-SQL does not require statement-terminating
// semicolons) never triggers the semicolon-cut path: it flushes whole at the
// GO batch separator (or EOF) — Complete=true, nothing lost.
func TestBody_TSQLSingleStatement_Complete(t *testing.T) {
	src := `CREATE TRIGGER trg ON t AFTER INSERT AS BEGIN SELECT 1 END
GO`
	s := parseDialect(t, sqlddl.SQLServer(), src)
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	body := s.Triggers[0].Body
	if !body.Complete {
		t.Errorf("Complete = false, want true (no internal ';': the whole body flushed at GO); Note=%q", body.Note)
	}
	if !strings.Contains(body.Text, "SELECT 1") {
		t.Errorf("Body.Text = %q, want it to contain the body's only statement", body.Text)
	}
}

// TestBody_TSQLMultiStatement_PartialCaptureFirstStatementOnly — the load-
// bearing case (architecture/tsql-body-truncation-limit): T-SQL has NEITHER
// dollar-quoting NOR a DELIMITER-style active-terminator override, so
// split() treats every top-level ';' inside a multi-statement BEGIN...END
// body as an ordinary statement terminator. Statements 2..N become separate,
// unassociated top-level stmt entries that apply()'s default branch silently
// skips (they match no CREATE * head regex) — captured Body is ONLY the text
// up to and including the FIRST internal ';'. This MUST be Complete=false
// with an explanatory Note, never a silent truncation and never an
// affirmation over the missing tail.
func TestBody_TSQLMultiStatement_PartialCaptureFirstStatementOnly(t *testing.T) {
	src := `CREATE PROCEDURE p AS
BEGIN
  SET @a = 1;
  SELECT * FROM t;
  UPDATE t SET x = 2;
END
GO`
	s := parseDialect(t, sqlddl.SQLServer(), src)
	if len(s.Procedures) != 1 {
		t.Fatalf("procedures = %d, want 1", len(s.Procedures))
	}
	body := s.Procedures[0].Body
	if body.Complete {
		t.Errorf("Complete = true, want false (multi-statement T-SQL body is cut at the first internal ';'); Text=%q", body.Text)
	}
	if body.Note == "" {
		t.Error("Note = \"\", want a non-empty reason explaining the truncation")
	}
	if !strings.Contains(body.Text, "SET @a = 1") {
		t.Errorf("Body.Text = %q, want it to contain the FIRST statement", body.Text)
	}
	if strings.Contains(body.Text, "SELECT * FROM t") || strings.Contains(body.Text, "UPDATE t SET x = 2") {
		t.Errorf("Body.Text = %q, must NOT contain statements 2/3 — this is the partial-capture case, not a full body", body.Text)
	}
}

// --- helpers ---

// parseDialect is parse() (reduce_test.go) with an explicit dialect, needed
// here because the shared parse() helper always binds Postgres.
func parseDialect(t *testing.T, d sqlddl.Dialect, src string) *db.Schema {
	t.Helper()
	p := sqlddl.New(sqlddl.WithDialect(d))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "V0__m.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}
