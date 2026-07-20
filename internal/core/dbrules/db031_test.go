package dbrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// --- DB-031: stored procedure/function whose body has NO exception-handling
// construct (surface, body-derived, never affirms — an ABSENCE stated as a
// structural fact; adequacy of a present handler is the agent's judgment).
// Functions and procedures both land in the neutral model as db.Procedure
// (reduce.go: CREATE FUNCTION and CREATE PROCEDURE build the same struct), so
// one iteration over s.Procedures covers both. ---

func db031Schema(name, bodyText string, complete bool) *db.Schema {
	return &db.Schema{Procedures: []db.Procedure{
		{
			Name: name,
			Pos:  db.Pos{File: "s.sql", Line: 42},
			Body: db.Body{Text: bodyText, Complete: complete},
		},
	}}
}

// --- Positives: no handler present → the rule FIRES (surface) ---

func TestDB031_FiresOnTSQLProcWithoutTryCatch(t *testing.T) {
	// A T-SQL body whose only block is BEGIN ... END with no BEGIN TRY.
	body := "CREATE PROCEDURE p AS\nBEGIN\n    SET NOCOUNT ON;\n    SELECT 1;\nEND;"
	s := db031Schema("p", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling); len(got) != 1 {
		t.Fatalf("DB-031 = %d, want 1 (T-SQL proc with no TRY/CATCH)", len(got))
	}
}

func TestDB031_FiresOnMySQLProcWithoutHandler(t *testing.T) {
	body := "CREATE PROCEDURE p()\nBEGIN\n    DECLARE x INT;\n    SET x = 1;\nEND"
	s := db031Schema("p", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling); len(got) != 1 {
		t.Fatalf("DB-031 = %d, want 1 (MySQL proc with no DECLARE ... HANDLER)", len(got))
	}
}

// TestDB031_FiresOnPGFunctionWithRaiseButNoHandler is THE trap: a PL/pgSQL
// body that RAISEs (RAISE EXCEPTION) but has NO handler clause (EXCEPTION
// WHEN). RAISE EXCEPTION is a THROW, not a handler — a scanner that matched
// bare "EXCEPTION" would false-negative here. The rule MUST still fire.
func TestDB031_FiresOnPGFunctionWithRaiseButNoHandler(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n    IF x = 0 THEN\n        RAISE EXCEPTION 'bad';\n    END IF;\n    RETURN;\nEND\n$$ LANGUAGE plpgsql;"
	s := db031Schema("f", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling)
	if len(got) != 1 {
		t.Fatalf("DB-031 = %d, want 1 — RAISE EXCEPTION is a THROW, not a handler; the rule MUST fire DESPITE it (bare-EXCEPTION false-negative trap)", len(got))
	}
	for _, sig := range got[0].StructuralSignals {
		if sig == "routine: f" {
			return
		}
	}
	t.Errorf("expected a structural signal naming the routine, got %v", got[0].StructuralSignals)
}

// --- Negatives: a handler IS present → the rule does NOT fire ---

func TestDB031_NegativeTSQLTryCatch(t *testing.T) {
	body := "CREATE PROCEDURE p AS\nBEGIN\n    BEGIN TRY\n        SELECT 1;\n    END TRY\n    BEGIN CATCH\n        EXECUTE dbo.uspLogError;\n    END CATCH;\nEND;"
	s := db031Schema("p", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling); len(got) != 0 {
		t.Errorf("DB-031 = %d, want 0 (T-SQL BEGIN TRY handler present)", len(got))
	}
}

func TestDB031_NegativeMySQLDeclareHandler(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS INT\nBEGIN\n    DECLARE EXIT HANDLER FOR NOT FOUND RETURN NULL;\n    RETURN 1;\nEND"
	s := db031Schema("f", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling); len(got) != 0 {
		t.Errorf("DB-031 = %d, want 0 (MySQL DECLARE ... HANDLER present)", len(got))
	}
}

func TestDB031_NegativePGExceptionWhen(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n    SELECT 1;\nEXCEPTION\n    WHEN others THEN\n        RAISE NOTICE 'caught';\nEND\n$$ LANGUAGE plpgsql;"
	s := db031Schema("f", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling); len(got) != 0 {
		t.Errorf("DB-031 = %d, want 0 (PL/pgSQL EXCEPTION WHEN handler present)", len(got))
	}
}

// --- Complete-gate (ADR 0004/0025): an unproven-whole body is never
// evaluated, so an absence over unread text is never falsely affirmed. ---

func TestDB031_AbstainOnIncompleteBody(t *testing.T) {
	// No handler is textually present, but Complete=false means the rule must
	// NOT affirm the absence: the missing handler might be past the cut.
	body := "CREATE PROCEDURE p AS\nBEGIN\n    SELECT 1;"
	s := db031Schema("p", body, false)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling); len(got) != 0 {
		t.Errorf("Complete=false body must NEVER be evaluated, got %d surface items", len(got))
	}
}

// --- String / comment awareness: a handler-shaped token inside a comment or
// a string literal is NOT a real handler → the rule still fires. ---

func TestDB031_HandlerTokenInCommentDoesNotSuppress(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n    -- EXCEPTION WHEN others: intentionally not handled here\n    RETURN;\nEND\n$$ LANGUAGE plpgsql;"
	s := db031Schema("f", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling); len(got) != 1 {
		t.Fatalf("DB-031 = %d, want 1 (EXCEPTION WHEN inside a comment is not a real handler)", len(got))
	}
}

func TestDB031_HandlerTokenInStringDoesNotSuppress(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n    RAISE NOTICE 'use EXCEPTION WHEN to handle';\n    RETURN;\nEND\n$$ LANGUAGE plpgsql;"
	s := db031Schema("f", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling); len(got) != 1 {
		t.Fatalf("DB-031 = %d, want 1 (EXCEPTION WHEN inside a string literal is not a real handler)", len(got))
	}
}

// --- Epistemology + registration ---

func TestDB031_NeverAffirms(t *testing.T) {
	// Pure surface (ADR 0017): even the clearest no-handler case must never be
	// a deterministic Finding.
	body := "CREATE PROCEDURE p AS BEGIN SELECT 1; END;"
	s := db031Schema("p", body, true)
	findingsOut, items := dbrules.Run(s)
	if len(findingsOut) != 0 {
		t.Fatalf("DB-031 must return 0 deterministic findings, got %d: %+v", len(findingsOut), findingsOut)
	}
	if len(surfaceWithCategory(items, surface.CategoryDBRoutineNoExceptionHandling)) == 0 {
		t.Fatal("sanity check failed: expected the fixture to fire as SURFACE at all")
	}
}

func TestAll_IncludesDB031(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range dbrules.All() {
		ids[r.ID()] = true
	}
	if !ids["DB-031"] {
		t.Errorf("dbrules.All() missing DB-031 (have %v)", ids)
	}
}
