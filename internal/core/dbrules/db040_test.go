package dbrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// --- DB-040: a TRIGGER whose body performs DML (INSERT/UPDATE/DELETE) against a
// table OTHER than the trigger's OWN table — a cross-table cascade. It is
// SURFACE, never an affirmation: it states the structural fact "this trigger
// writes to other table(s): X, Y; documented_by_comment: <bool>", and the AGENT
// judges whether the cascade is intentional/correct. TRIGGERS only
// (procedures/functions are DB-031/DB-030's domain).
//
// Body source is per-dialect. MySQL/T-SQL triggers carry an INLINE body
// (Table is the trigger's own table, Body.Text scanned directly). A PostgreSQL
// trigger has NO inline body (ADR 0026) — its logic lives in the executed
// function, resolved via Schema.ExecutedProcedure(t), whose own Body is scanned.

// inlineTriggerSchema builds a schema with ONE MySQL/T-SQL-style trigger that
// carries an inline body (the dialect embeds the logic directly in Body.Text).
func inlineTriggerSchema(name, table, bodyText string, complete bool) *db.Schema {
	return &db.Schema{Triggers: []db.Trigger{
		{
			Name:  name,
			Pos:   db.Pos{File: "t.sql", Line: 7},
			Table: table,
			Body:  db.Body{Text: bodyText, Complete: complete},
		},
	}}
}

// pgTriggerSchema builds a schema mirroring the PostgreSQL shape (ADR 0026):
// the trigger has NO inline body and names an executed function; the function
// is a separate Procedure whose Body carries the logic.
func pgTriggerSchema(trigName, trigTable, fnName, fnBody string, fnComplete bool) *db.Schema {
	return &db.Schema{
		Triggers: []db.Trigger{{
			Name:             trigName,
			Pos:              db.Pos{File: "t.sql", Line: 3},
			Table:            trigTable,
			Body:             db.Body{Text: "CREATE TRIGGER " + trigName + " ... EXECUTE FUNCTION " + fnName + "()", Complete: true},
			ExecutesFunction: fnName,
		}},
		Procedures: []db.Procedure{{
			Name: fnName,
			Pos:  db.Pos{File: "t.sql", Line: 1},
			Body: db.Body{Text: fnBody, Complete: fnComplete},
		}},
	}
}

// hasSignal reports whether sig is present verbatim in the signal list.
func hasSignal(signals []string, sig string) bool {
	for _, s := range signals {
		if s == sig {
			return true
		}
	}
	return false
}

// --- Positives: a cross-table write → the rule FIRES (surface) ---

func TestDB040_FiresOnInlineTriggerInsertingOtherTable(t *testing.T) {
	// A trigger on `film` whose body INSERTs into `film_text` (a DIFFERENT table).
	body := "CREATE TRIGGER ins_film AFTER INSERT ON film FOR EACH ROW BEGIN\n" +
		"    INSERT INTO film_text (film_id, title) VALUES (new.film_id, new.title);\nEND"
	s := inlineTriggerSchema("ins_film", "film", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade)
	if len(got) != 1 {
		t.Fatalf("DB-040 = %d, want 1 (trigger on film INSERTs into film_text)", len(got))
	}
	if !hasSignal(got[0].StructuralSignals, "writes_other_table: film_text") {
		t.Errorf("expected a signal naming the other table, got %v", got[0].StructuralSignals)
	}
}

func TestDB040_FiresOnInlineTriggerUpdatingOtherTable(t *testing.T) {
	body := "CREATE TRIGGER upd_film AFTER UPDATE ON film FOR EACH ROW BEGIN\n" +
		"    UPDATE film_text SET title = new.title WHERE film_id = old.film_id;\nEND"
	s := inlineTriggerSchema("upd_film", "film", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 1 {
		t.Fatalf("DB-040 = %d, want 1 (trigger on film UPDATEs film_text)", len(got))
	}
}

func TestDB040_FiresOnInlineTriggerDeletingOtherTable(t *testing.T) {
	body := "CREATE TRIGGER del_film AFTER DELETE ON film FOR EACH ROW BEGIN\n" +
		"    DELETE FROM film_text WHERE film_id = old.film_id;\nEND"
	s := inlineTriggerSchema("del_film", "film", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 1 {
		t.Fatalf("DB-040 = %d, want 1 (trigger on film DELETEs FROM film_text)", len(got))
	}
}

// --- Same-table write is NOT a cascade → the rule does NOT fire ---

func TestDB040_SameTableWriteDoesNotFire(t *testing.T) {
	// A trigger on `audit` whose only write is to `audit` itself: not a cascade.
	body := "CREATE TRIGGER t ON audit AFTER INSERT AS\nBEGIN\n" +
		"    UPDATE audit SET n = n + 1 WHERE id = 1;\nEND;"
	s := inlineTriggerSchema("t", "audit", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (a write to the trigger's OWN table is not a cross-table cascade)", len(got))
	}
}

func TestDB040_SameTableWriteSchemaQualifiedDoesNotFire(t *testing.T) {
	// The trigger's own table is "PurchaseOrderDetail"; the write is
	// schema-qualified ("Purchasing"."PurchaseOrderDetail") — still the same
	// table, so no cascade (schema-qualification-aware comparison).
	body := "CREATE TRIGGER t ON \"Purchasing\".\"PurchaseOrderDetail\" AFTER UPDATE AS\nBEGIN\n" +
		"    UPDATE \"Purchasing\".\"PurchaseOrderDetail\" SET \"ModifiedDate\" = GETDATE();\nEND;"
	s := inlineTriggerSchema("t", "PurchaseOrderDetail", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (schema-qualified write to the SAME table is not a cascade)", len(got))
	}
}

// --- No DML at all → does not fire ---

func TestDB040_NoDMLDoesNotFire(t *testing.T) {
	body := "CREATE TRIGGER t BEFORE INSERT ON orders FOR EACH ROW BEGIN\n" +
		"    SET NEW.created_at = NOW();\nEND"
	s := inlineTriggerSchema("t", "orders", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (no INSERT/UPDATE/DELETE in the body)", len(got))
	}
}

// TestDB040_TSQLUpdateFunctionIsNotDML locks the T-SQL "UPDATE(column)"
// function trap: "IF UPDATE([ProductID])" tests whether a column was updated —
// it is NOT a DML UPDATE and names no table. A scanner treating UPDATE( as DML
// would fabricate a phantom cross-table target.
func TestDB040_TSQLUpdateFunctionIsNotDML(t *testing.T) {
	body := "CREATE TRIGGER t ON PurchaseOrderDetail AFTER UPDATE AS\nBEGIN\n" +
		"    IF UPDATE(ProductID) OR UPDATE(OrderQty)\n        SET NOCOUNT ON;\nEND;"
	s := inlineTriggerSchema("t", "PurchaseOrderDetail", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (T-SQL UPDATE(col) is a column-changed test, not DML)", len(got))
	}
}

// --- Complete-gate (ADR 0004/0025): an unproven-whole body is never evaluated ---

func TestDB040_AbstainOnIncompleteInlineBody(t *testing.T) {
	// A cross-table write is textually present, but Complete=false means the
	// rule must NOT affirm anything over a body the parser could not prove whole.
	body := "CREATE TRIGGER ins_film AFTER INSERT ON film FOR EACH ROW BEGIN\n" +
		"    INSERT INTO film_text (film_id) VALUES (new.film_id);"
	s := inlineTriggerSchema("ins_film", "film", body, false)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("Complete=false inline body must NEVER be evaluated, got %d surface items", len(got))
	}
}

// --- PostgreSQL: the trigger has no inline body; the rule MUST follow
// ExecutesFunction to the executed function's body (ADR 0026). ---

func TestDB040_PGResolvesTriggerToFunctionBody_Fires(t *testing.T) {
	// The trigger statement itself has NO DML — the cascade lives in the
	// executed function. The rule must resolve the function and fire.
	fn := "CREATE FUNCTION audit_fn() RETURNS trigger AS $$\nBEGIN\n" +
		"    INSERT INTO order_audit (order_id) VALUES (NEW.order_id);\n    RETURN NEW;\nEND\n$$ LANGUAGE plpgsql;"
	s := pgTriggerSchema("order_audit_trigger", "orders", "audit_fn", fn, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade)
	if len(got) != 1 {
		t.Fatalf("DB-040 = %d, want 1 (PG trigger resolves to its function, which INSERTs into order_audit)", len(got))
	}
	if !hasSignal(got[0].StructuralSignals, "writes_other_table: order_audit") {
		t.Errorf("expected the resolved function's cross-table target, got %v", got[0].StructuralSignals)
	}
}

func TestDB040_PGResolvedFunctionNoCrossTable_DoesNotFire(t *testing.T) {
	// last_updated-shaped: the function only sets NEW, no cross-table write.
	fn := "CREATE FUNCTION last_updated() RETURNS trigger AS $$\nBEGIN\n" +
		"    NEW.last_update = CURRENT_TIMESTAMP;\n    RETURN NEW;\nEND $$;"
	s := pgTriggerSchema("last_updated", "actor", "last_updated", fn, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (resolved function only sets NEW, no cross-table write)", len(got))
	}
}

func TestDB040_PGUnresolvableFunctionAbstains(t *testing.T) {
	// A trigger naming a built-in (tsvector_update_trigger) has no CREATE
	// FUNCTION to resolve — ExecutedProcedure returns (nil,false) — so the rule
	// abstains (honest: it cannot see the logic).
	s := &db.Schema{Triggers: []db.Trigger{{
		Name:             "film_fulltext_trigger",
		Pos:              db.Pos{File: "t.sql", Line: 5},
		Table:            "film",
		Body:             db.Body{Text: "CREATE TRIGGER ... EXECUTE FUNCTION tsvector_update_trigger(...)", Complete: true},
		ExecutesFunction: "tsvector_update_trigger",
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (built-in function is unresolvable — abstain)", len(got))
	}
}

func TestDB040_PGResolvedFunctionIncompleteBodyAbstains(t *testing.T) {
	fn := "CREATE FUNCTION audit_fn() RETURNS trigger AS $$\nBEGIN\n    INSERT INTO order_audit"
	s := pgTriggerSchema("order_audit_trigger", "orders", "audit_fn", fn, false)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (resolved function body Complete=false — abstain)", len(got))
	}
}

// --- documented_by_comment FACT (not a judgment) ---

func TestDB040_DocumentedByCommentFactTrue(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER UPDATE AS\nBEGIN\n" +
		"    -- Insert an audit row into the history table\n" +
		"    INSERT INTO order_history (order_id) VALUES (1);\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade)
	if len(got) != 1 {
		t.Fatalf("DB-040 = %d, want 1", len(got))
	}
	if !got[0].StructuralFacts["documented_by_comment"] {
		t.Errorf("expected documented_by_comment=true (a comment precedes the cross-table write), facts=%v", got[0].StructuralFacts)
	}
}

func TestDB040_DocumentedByCommentFactFalse(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER UPDATE AS\nBEGIN\n" +
		"    INSERT INTO order_history (order_id) VALUES (1);\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade)
	if len(got) != 1 {
		t.Fatalf("DB-040 = %d, want 1", len(got))
	}
	if got[0].StructuralFacts["documented_by_comment"] {
		t.Errorf("expected documented_by_comment=false (no comment near the cross-table write), facts=%v", got[0].StructuralFacts)
	}
}

// --- String / comment awareness: a DML-shaped token inside a comment or a
// string literal is NOT a real write → does not fabricate a cascade. ---

func TestDB040_DMLTokenInStringDoesNotFire(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n" +
		"    RAISERROR('INSERT INTO other_table failed', 16, 1);\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (INSERT INTO inside a string literal is not a real write)", len(got))
	}
}

func TestDB040_DMLTokenInCommentDoesNotFire(t *testing.T) {
	body := "CREATE TRIGGER t ON orders AFTER INSERT AS\nBEGIN\n" +
		"    -- INSERT INTO other_table would cascade, but we do not\n    SET NOCOUNT ON;\nEND;"
	s := inlineTriggerSchema("t", "orders", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (INSERT INTO inside a comment is not a real write)", len(got))
	}
}

// --- Epistemology + registration ---

func TestDB040_NeverAffirms(t *testing.T) {
	body := "CREATE TRIGGER ins_film AFTER INSERT ON film FOR EACH ROW BEGIN\n" +
		"    INSERT INTO film_text (film_id) VALUES (new.film_id);\nEND"
	s := inlineTriggerSchema("ins_film", "film", body, true)
	findingsOut, items := dbrules.Run(s)
	if len(findingsOut) != 0 {
		t.Fatalf("DB-040 must return 0 deterministic findings, got %d: %+v", len(findingsOut), findingsOut)
	}
	if len(surfaceWithCategory(items, surface.CategoryDBTriggerCrossTableCascade)) == 0 {
		t.Fatal("sanity check failed: expected the fixture to fire as SURFACE at all")
	}
}

func TestAll_IncludesDB040(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range dbrules.All() {
		ids[r.ID()] = true
	}
	if !ids["DB-040"] {
		t.Errorf("dbrules.All() missing DB-040 (have %v)", ids)
	}
}
