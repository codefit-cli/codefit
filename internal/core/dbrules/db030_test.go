package dbrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// DB-030: a stored procedure OR function whose body CONSTRUCTS and runs SQL at
// runtime from a string. Surface, never an affirmation — the injectability is
// the agent's judgment. THE TRAP: a static EXEC/CALL of a named internal
// procedure (EXEC dbo.uspFoo) is NOT dynamic SQL and must not fire.

func procSchema(name, body string, complete bool) *db.Schema {
	return &db.Schema{Procedures: []db.Procedure{{
		Name: name,
		Pos:  db.Pos{File: "p.sql", Line: 3},
		Body: db.Body{Text: body, Complete: complete},
	}}}
}

// --- Positives (dynamic SQL construction → fires) ---

func TestDB030_TSQL_SpExecutesql_Fires(t *testing.T) {
	body := "CREATE PROCEDURE p AS\nBEGIN\n    DECLARE @sql nvarchar(max) = N'SELECT * FROM t WHERE id = ' + @id;\n    EXEC sp_executesql @sql;\nEND;"
	s := procSchema("p", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine); len(got) != 1 {
		t.Fatalf("DB-030 = %d, want 1 (sp_executesql runs a built string)", len(got))
	}
}

func TestDB030_TSQL_ExecParen_Fires(t *testing.T) {
	body := "CREATE PROCEDURE p AS\nBEGIN\n    EXEC(@sql);\nEND;"
	s := procSchema("p", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine); len(got) != 1 {
		t.Fatalf("DB-030 = %d, want 1 (EXEC(@var) executes an expression)", len(got))
	}
}

func TestDB030_PG_ExecuteFormat_Fires(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n    EXECUTE format('SELECT * FROM %I', tbl);\nEND $$ LANGUAGE plpgsql;"
	s := procSchema("f", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine); len(got) != 1 {
		t.Fatalf("DB-030 = %d, want 1 (EXECUTE format(...) is dynamic SQL)", len(got))
	}
}

func TestDB030_PG_QuoteLiteral_Fires(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n    sql := 'SELECT * FROM t WHERE x = ' || quote_literal(v);\n    EXECUTE sql;\nEND $$;"
	s := procSchema("f", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine); len(got) != 1 {
		t.Fatalf("DB-030 = %d, want 1 (quote_literal builds a dynamic query string)", len(got))
	}
}

func TestDB030_PG_ExecuteStringLiteral_Fires(t *testing.T) {
	body := "CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n    FOR r IN EXECUTE 'SELECT * FROM t' LOOP\n    END LOOP;\nEND $$;"
	s := procSchema("f", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine); len(got) != 1 {
		t.Fatalf("DB-030 = %d, want 1 (EXECUTE '<string>' runs dynamic SQL)", len(got))
	}
}

func TestDB030_MySQL_PrepareFrom_Fires(t *testing.T) {
	body := "CREATE PROCEDURE p()\nBEGIN\n    SET @s = CONCAT('SELECT * FROM t WHERE id = ', id);\n    PREPARE stmt FROM @s;\n    EXECUTE stmt;\nEND"
	s := procSchema("p", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine); len(got) != 1 {
		t.Fatalf("DB-030 = %d, want 1 (PREPARE ... FROM a built string is dynamic SQL)", len(got))
	}
}

// --- THE TRAP: a static EXEC/CALL of a named internal proc is NOT dynamic ---

func TestDB030_TSQL_StaticExecProc_DoesNotFire(t *testing.T) {
	// EXEC of a literal proc NAME (no parens, no variable) is a static call —
	// the same EXEC family as DB-041's trap, but excluded here for a different
	// reason: it is not DYNAMIC (there, it was not EXTERNAL).
	body := "CREATE PROCEDURE p AS\nBEGIN\n    EXEC dbo.uspLogError;\n    EXECUTE dbo.uspDoWork;\nEND;"
	s := procSchema("p", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine); len(got) != 0 {
		t.Errorf("DB-030 = %d, want 0 (EXEC of a literal proc name is a static call, not dynamic SQL)", len(got))
	}
}

func TestDB030_MySQL_StaticCall_DoesNotFire(t *testing.T) {
	body := "CREATE PROCEDURE p()\nBEGIN\n    CALL recompute_totals(1);\nEND"
	s := procSchema("p", body, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine)) != 0 {
		t.Errorf("want 0 (CALL of a named proc is static)")
	}
}

// --- String / comment awareness + Complete-gate ---

func TestDB030_TokenInStringDoesNotFire(t *testing.T) {
	body := "CREATE PROCEDURE p AS\nBEGIN\n    RAISERROR('do not use sp_executesql or EXEC(@x) here', 16, 1);\nEND;"
	s := procSchema("p", body, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine)) != 0 {
		t.Errorf("want 0 (sp_executesql inside a string literal is not a real call)")
	}
}

func TestDB030_TokenInCommentDoesNotFire(t *testing.T) {
	body := "CREATE PROCEDURE p AS\nBEGIN\n    -- never call sp_executesql with untrusted input\n    SELECT 1;\nEND;"
	s := procSchema("p", body, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine)) != 0 {
		t.Errorf("want 0 (sp_executesql inside a comment is not a real call)")
	}
}

func TestDB030_AbstainOnIncompleteBody(t *testing.T) {
	body := "CREATE PROCEDURE p AS\nBEGIN\n    EXEC sp_executesql @sql"
	s := procSchema("p", body, false)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine)) != 0 {
		t.Errorf("Complete=false body must NEVER be evaluated")
	}
}

// --- Epistemology + registration ---

func TestDB030_NeverAffirms(t *testing.T) {
	body := "CREATE PROCEDURE p AS BEGIN EXEC(@sql); END;"
	s := procSchema("p", body, true)
	findingsOut, items := dbrules.Run(s)
	if len(findingsOut) != 0 {
		t.Fatalf("DB-030 must return 0 deterministic findings, got %d", len(findingsOut))
	}
	if len(surfaceWithCategory(items, surface.CategoryDBDynamicSQLInRoutine)) == 0 {
		t.Fatal("sanity: expected the fixture to fire as SURFACE")
	}
}

func TestAll_IncludesDB030(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range dbrules.All() {
		ids[r.ID()] = true
	}
	if !ids["DB-030"] {
		t.Errorf("dbrules.All() missing DB-030 (have %v)", ids)
	}
}
