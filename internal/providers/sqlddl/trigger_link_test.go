package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Unit A2 (db-debt-views-and-nplus1, Phase 2.2): the PG-trigger false-
// incomplete fix + the trigger→function LINK in the neutral model
// (architecture/pg-trigger-body-link, obs #1053 — the architect's two binding
// conditions).
//
// Condition 1 (non-negotiable, first): the Complete flag must TELL THE
// TRUTH. A PostgreSQL trigger has NO inline body — it is a wire
// ("... EXECUTE FUNCTION fn();") — so it is COMPLETE, not partial, even
// though its own statement ends on an ordinary ';' with no dollar-quoted
// block (the exact shape routineBody's formula would otherwise flag as
// possibly-truncated). This must be a per-dialect DATA descriptor field
// (Dialect.TriggerHasInlineBody), never a dialect.Name branch — the same
// architecture as v0.2.1 (ADR 0022).

// TestTrigger_PostgresWiresOnly_CompleteTrue is the RED test for Condition 1:
// before the fix, a PostgreSQL trigger with no body of its own — only an
// EXECUTE FUNCTION clause — is reported Complete=false by routineBody's
// generic formula (term==termSemicolon, no dollar-quoted block was seen in
// THIS statement). That is a FALSE incomplete: the statement was never cut,
// it simply had nothing to truncate.
func TestTrigger_PostgresWiresOnly_CompleteTrue(t *testing.T) {
	src := `CREATE TABLE t (id int);
CREATE TRIGGER trg AFTER INSERT ON t FOR EACH ROW EXECUTE FUNCTION fn();`
	s := parse(t, src)
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	trig := s.Triggers[0]
	if !trig.Body.Complete {
		t.Errorf("Complete = false, want true (a PG trigger has no inline body to truncate); Note=%q Text=%q", trig.Body.Note, trig.Body.Text)
	}
	if trig.Body.Note == "" {
		t.Error(`Note = "", want a non-empty explanation that PG triggers carry no inline body`)
	}
	if trig.ExecutesFunction != "fn" {
		t.Errorf("ExecutesFunction = %q, want %q", trig.ExecutesFunction, "fn")
	}
}

// TestTrigger_MySQLNoDelimiter_StaysIncomplete regression-locks that the
// PRE-EXISTING MySQL no-DELIMITER trigger case (TestBody_MySQLNoDelimiterSingleStatement_IncompleteWithNote
// in body_test.go) is NOT disturbed by the PG-only descriptor fix: MySQL's
// TriggerHasInlineBody is true, so MySQL triggers keep going through
// routineBody's conservative formula unchanged.
func TestTrigger_MySQLNoDelimiter_StaysIncomplete(t *testing.T) {
	src := `CREATE TRIGGER trg BEFORE INSERT ON t FOR EACH ROW SET x = 1;`
	s := parseDialect(t, sqlddl.MySQL(), src)
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	if s.Triggers[0].Body.Complete {
		t.Errorf("Complete = true, want false (MySQL triggers carry an inline body; no DELIMITER wrapper here means it cannot be proven whole)")
	}
	if s.Triggers[0].ExecutesFunction != "" {
		t.Errorf("ExecutesFunction = %q, want empty (MySQL triggers embed logic inline, no EXECUTE FUNCTION clause)", s.Triggers[0].ExecutesFunction)
	}
}

// TestTrigger_TSQL_StaysUnaffected regression-locks that T-SQL triggers
// (TriggerHasInlineBody=true) keep going through routineBody unchanged: the
// existing single-statement Complete=true case from body_test.go
// (TestBody_TSQLSingleStatement_Complete) is re-asserted here directly on
// Trigger, and ExecutesFunction stays empty (T-SQL has no EXECUTE FUNCTION
// clause).
func TestTrigger_TSQL_StaysUnaffected(t *testing.T) {
	src := `CREATE TRIGGER trg ON t AFTER INSERT AS BEGIN SELECT 1 END
GO`
	s := parseDialect(t, sqlddl.SQLServer(), src)
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	if !s.Triggers[0].Body.Complete {
		t.Errorf("Complete = false, want true (unchanged T-SQL single-statement case)")
	}
	if s.Triggers[0].ExecutesFunction != "" {
		t.Errorf("ExecutesFunction = %q, want empty (T-SQL triggers embed logic inline)", s.Triggers[0].ExecutesFunction)
	}
}

// TestTrigger_PagilaLink_ResolvesToCompleteFunctionBody is Condition 2's
// dogfood proof, LOCKED AGAINST THE REAL PAGILA FIXTURE
// (testdata/pagila_excerpt.sql), per the architect's standing instruction:
// "if the link cannot be proven with dogfood, DB-040/DB-041 go back to
// NotCovered and do not ship. That outcome is acceptable; a fabricated pass
// is not."
//
// Pagila's excerpt has a genuine trigger+function pair: the "last_updated"
// trigger on "actor" wires to "EXECUTE FUNCTION public.last_updated()", and
// "CREATE FUNCTION public.last_updated()" is itself present in the same
// excerpt (a dollar-quoted PL/pgSQL body, independently Complete). This
// proves the link end-to-end: trigger name → ExecutesFunction → resolved
// Procedure → that procedure's OWN (unrelated, independently-derived) Body is
// present and Complete.
func TestTrigger_PagilaLink_ResolvesToCompleteFunctionBody(t *testing.T) {
	s := goldenSchema(t, "pagila_excerpt.sql", sqlddl.New())

	var found bool
	for _, tr := range s.Triggers {
		if tr.Name != "last_updated" || tr.Table != "actor" {
			continue
		}
		found = true
		if tr.ExecutesFunction != "last_updated" {
			t.Fatalf("ExecutesFunction = %q, want %q", tr.ExecutesFunction, "last_updated")
		}
		if !tr.Body.Complete {
			t.Errorf("trigger Body.Complete = false, want true (PG trigger is a wire, not a truncated body)")
		}
		proc, ok := s.ExecutedProcedure(tr)
		if !ok {
			t.Fatalf("ExecutedProcedure(%q) resolved = false, want true — the function IS defined in this excerpt", tr.ExecutesFunction)
		}
		if !proc.Body.Complete {
			t.Errorf("resolved Procedure %q Body.Complete = false, want true (dollar-quoted PL/pgSQL body)", proc.Name)
		}
		if !strings.Contains(proc.Body.Text, "NEW.last_update = CURRENT_TIMESTAMP") {
			t.Errorf("resolved Procedure Body.Text = %q, want it to contain the real function logic", proc.Body.Text)
		}
	}
	if !found {
		t.Fatal(`trigger "last_updated" on "actor" not found in pagila_excerpt.sql — fixture drifted`)
	}
}

// TestTrigger_PagilaLink_UnresolvedBuiltinIsHonestlyAbsent locks the OTHER
// half of Condition 2's honesty requirement: the fixture's second trigger
// ("film_fulltext_trigger") wires to "tsvector_update_trigger", a PostgreSQL
// BUILT-IN function with no CREATE FUNCTION statement anywhere in the
// excerpt. ExecutesFunction still names it (the link is a structural fact
// about the trigger statement, independent of resolvability), but
// Schema.ExecutedProcedure MUST NOT fabricate a match — it returns
// (nil, false), same as if no trigger→function work existed at all. Fixture
// path: internal/providers/sqlddl/testdata/pagila_excerpt.sql.
func TestTrigger_PagilaLink_UnresolvedBuiltinIsHonestlyAbsent(t *testing.T) {
	s := goldenSchema(t, "pagila_excerpt.sql", sqlddl.New())

	var found bool
	for _, tr := range s.Triggers {
		if tr.Name != "film_fulltext_trigger" || tr.Table != "film" {
			continue
		}
		found = true
		if tr.ExecutesFunction != "tsvector_update_trigger" {
			t.Fatalf("ExecutesFunction = %q, want %q", tr.ExecutesFunction, "tsvector_update_trigger")
		}
		if !tr.Body.Complete {
			t.Errorf("trigger Body.Complete = false, want true (PG trigger is a wire, regardless of whether the target resolves)")
		}
		if _, ok := s.ExecutedProcedure(tr); ok {
			t.Error("ExecutedProcedure resolved = true, want false — tsvector_update_trigger is a PG built-in with no CREATE FUNCTION in this excerpt; a match here would be fabricated")
		}
	}
	if !found {
		t.Fatal(`trigger "film_fulltext_trigger" on "film" not found in pagila_excerpt.sql — fixture drifted`)
	}
}
