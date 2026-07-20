package dbrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// DB-041: a TRIGGER whose body invokes an EXTERNAL-EFFECTING call — one that
// reaches OUTSIDE the database. STRICT vocabulary: a plain EXECUTE/CALL of an
// internal stored procedure is NOT external (that is the trap, locked below).
// Reuses inlineTriggerSchema / pgTriggerSchema / hasSignal from db040_test.go.

// --- Positives (external-effecting call → fires) ---

func TestDB041_FiresOnTSQLXpCmdshell(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n    EXEC xp_cmdshell 'del *.*';\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)
	if len(got) != 1 {
		t.Fatalf("DB-041 = %d, want 1 (xp_cmdshell is a shell-exec external call)", len(got))
	}
	if !hasSignal(got[0].StructuralSignals, "external_call: xp_cmdshell") {
		t.Errorf("want an xp_cmdshell external_call signal, got %v", got[0].StructuralSignals)
	}
}

func TestDB041_FiresOnTSQLSpSendDbmail(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n    EXEC msdb.dbo.sp_send_dbmail @recipients='x@y.z';\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall); len(got) != 1 {
		t.Fatalf("DB-041 = %d, want 1 (sp_send_dbmail sends email out of the DB)", len(got))
	}
}

func TestDB041_FiresOnTSQLSpOACreate(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n    DECLARE @o int; EXEC sp_OACreate 'MSXML2.XMLHTTP', @o OUT;\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 1 {
		t.Fatalf("want 1 (sp_OACreate is OLE automation — external)")
	}
}

func TestDB041_FiresOnTSQLOpenrowset(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n    SELECT * FROM OPENROWSET('SQLNCLI','...','SELECT 1');\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 1 {
		t.Fatalf("want 1 (OPENROWSET reaches a remote/external data source)")
	}
}

// --- THE TRAP: an EXECUTE/CALL of an INTERNAL stored proc is NOT external ---

func TestDB041_InternalProcExecuteDoesNotFire(t *testing.T) {
	// uPurchaseOrderDetail-shaped: EXECUTE of an internal logging proc. Under the
	// STRICT vocabulary this is a normal internal routine call, NOT an external
	// call — the token-that-looks-like-but-is-not (cf. RAISE EXCEPTION in DB-031,
	// UPDATE(col) in DB-040).
	body := "CREATE TRIGGER t ON PurchaseOrderDetail AFTER UPDATE AS\nBEGIN\n" +
		"    EXECUTE [dbo].[uspPrintError];\n    EXECUTE [dbo].[uspLogError];\nEND;"
	s := inlineTriggerSchema("t", "PurchaseOrderDetail", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall); len(got) != 0 {
		t.Errorf("DB-041 = %d, want 0 (EXECUTE of an INTERNAL stored proc is not an external call)", len(got))
	}
}

func TestDB041_MySQLInternalCallDoesNotFire(t *testing.T) {
	body := "CREATE TRIGGER t AFTER INSERT ON orders FOR EACH ROW\nBEGIN\n    CALL recompute_totals(NEW.id);\nEND"
	s := inlineTriggerSchema("t", "orders", body, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 0 {
		t.Errorf("want 0 (CALL of an internal proc is not external)")
	}
}

// --- PostgreSQL: resolve the trigger to its function (ADR 0026) and scan THAT ---

func TestDB041_PGResolvesToFunctionWithNotify_Fires(t *testing.T) {
	fn := "CREATE FUNCTION notify_fn() RETURNS trigger AS $$\nBEGIN\n    NOTIFY order_events;\n    RETURN NEW;\nEND\n$$ LANGUAGE plpgsql;"
	s := pgTriggerSchema("order_notify", "orders", "notify_fn", fn, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)
	if len(got) != 1 {
		t.Fatalf("DB-041 = %d, want 1 (the resolved function issues NOTIFY — an async signal out of the transaction)", len(got))
	}
	if !hasSignal(got[0].StructuralSignals, "external_call: notify") {
		t.Errorf("want a notify external_call signal from the resolved function, got %v", got[0].StructuralSignals)
	}
}

func TestDB041_PGResolvesToFunctionWithDblink_Fires(t *testing.T) {
	fn := "CREATE FUNCTION remote_fn() RETURNS trigger AS $$\nBEGIN\n    PERFORM dblink_exec('dbname=other', 'INSERT INTO t VALUES (1)');\n    RETURN NEW;\nEND $$;"
	s := pgTriggerSchema("remote_trg", "orders", "remote_fn", fn, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 1 {
		t.Fatalf("want 1 (dblink_exec runs a query on a REMOTE database)")
	}
}

func TestDB041_PGCopyProgram_Fires(t *testing.T) {
	fn := "CREATE FUNCTION copy_fn() RETURNS trigger AS $$\nBEGIN\n    COPY orders TO PROGRAM 'cat > /tmp/x';\n    RETURN NEW;\nEND $$;"
	s := pgTriggerSchema("copy_trg", "orders", "copy_fn", fn, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 1 {
		t.Fatalf("want 1 (COPY ... TO PROGRAM pipes to a shell command)")
	}
}

func TestDB041_PGResolvedFunctionNoExternal_DoesNotFire(t *testing.T) {
	fn := "CREATE FUNCTION last_updated() RETURNS trigger AS $$\nBEGIN\n    NEW.last_update = CURRENT_TIMESTAMP;\n    RETURN NEW;\nEND $$;"
	s := pgTriggerSchema("last_updated", "actor", "last_updated", fn, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 0 {
		t.Errorf("want 0 (only sets NEW — no external call)")
	}
}

func TestDB041_PGUnresolvableFunctionAbstains(t *testing.T) {
	s := &db.Schema{Triggers: []db.Trigger{{
		Name:             "film_fulltext_trigger",
		Pos:              db.Pos{File: "t.sql", Line: 5},
		Table:            "film",
		Body:             db.Body{Text: "CREATE TRIGGER ... EXECUTE FUNCTION tsvector_update_trigger(...)", Complete: true},
		ExecutesFunction: "tsvector_update_trigger",
	}}}
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 0 {
		t.Errorf("want 0 (built-in function is unresolvable — abstain)")
	}
}

// --- String / comment awareness + Complete-gate ---

func TestDB041_ExternalTokenInStringDoesNotFire(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n    RAISERROR('do not run xp_cmdshell here', 16, 1);\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 0 {
		t.Errorf("want 0 (xp_cmdshell inside a string literal is not a real call)")
	}
}

func TestDB041_ExternalTokenInCommentDoesNotFire(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n    -- never call xp_cmdshell from a trigger\n    SET NOCOUNT ON;\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 0 {
		t.Errorf("want 0 (xp_cmdshell inside a comment is not a real call)")
	}
}

func TestDB041_AbstainOnIncompleteBody(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n    EXEC xp_cmdshell 'x'"
	s := inlineTriggerSchema("t", "orders", body, false)
	if _, items := dbrules.Run(s); len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) != 0 {
		t.Errorf("Complete=false body must NEVER be evaluated")
	}
}

// --- Epistemology + registration ---

func TestDB041_NeverAffirms(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n    EXEC xp_cmdshell 'x';\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	findingsOut, items := dbrules.Run(s)
	if len(findingsOut) != 0 {
		t.Fatalf("DB-041 must return 0 deterministic findings, got %d", len(findingsOut))
	}
	if len(surfaceWithCategory(items, surface.CategoryDBTriggerExternalCall)) == 0 {
		t.Fatal("sanity: expected the fixture to fire as SURFACE")
	}
}

func TestAll_IncludesDB041(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range dbrules.All() {
		ids[r.ID()] = true
	}
	if !ids["DB-041"] {
		t.Errorf("dbrules.All() missing DB-041 (have %v)", ids)
	}
}
