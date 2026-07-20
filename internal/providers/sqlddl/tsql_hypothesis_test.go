package sqlddl_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Unit B (db-debt-views-and-nplus1, Phase 2.2): T-SQL fixture proof — RE-POINTED
// at REAL upstream AdventureWorks DDL (Unit B2, architecture/tsql-golden-real-ddl,
// obs #1054).
//
// architecture/tsql-body-truncation-limit (obs #1050) is CLOSED by ADR 0027:
// T-SQL views were already unaffected by the body-truncation limit (a CREATE
// VIEW ... AS SELECT ... is a single statement with no internal ';'), and
// multi-statement T-SQL PROCEDURES and TRIGGERS no longer truncate at the
// first internal ';' either — split() now suspends ';'-flushing once it
// recognizes a routine head, capturing the whole body to the GO batch
// separator (or EOF). This file is the confirmation the architect required
// before COVERAGE.md may say so — the first pass (Unit B) confirmed the
// (now-closed) truncation limit against hand-written "in the style of
// AdventureWorks" DDL; THIS pass (Unit B2) re-confirms full capture to GO
// against a REAL, VERBATIM excerpt of upstream AdventureWorks DDL, per the
// architect's binding Condition 1 (obs #1054): "the DDL must come from
// UPSTREAM AdventureWorks, COPIED, not rewritten in the style of."
//
// Fixture provenance: testdata/tsql/adventureworks_real_objects.sql — three
// real objects copied VERBATIM (byte-for-byte, aside from CRLF->LF
// normalization) from
// https://github.com/microsoft/sql-server-samples/blob/master/samples/databases/adventure-works/oltp-install-script/instawdb.sql
// (MIT license, full notice retained in the fixture file's header):
//   - CREATE VIEW      [HumanResources].[vEmployee]
//   - CREATE PROCEDURE [dbo].[uspGetBillOfMaterials]        (recursive CTE)
//   - CREATE TRIGGER   [Purchasing].[uPurchaseOrderDetail]  (TRY/CATCH)
//
// The prior hand-written fixture (adventureworks_excerpt.sql, table/index
// only) is untouched — see Unit A's TestSQLDDL_ExistingGoldens_
// UnaffectedByBodyField, which still locks it at 0 views/procs/triggers.

// TestTSQL_CreateView_SingleStatement_AlwaysComplete is the confirming test
// for the "T-SQL views are unaffected" half of the hypothesis: a CREATE VIEW
// body is one SELECT with no internal ';' in any dialect (reduce.go's
// viewBody is unconditional — it does not even branch on dialect), so it
// must come back Complete=true on T-SQL exactly as it does on every other
// dialect. Fixture: the REAL HumanResources.vEmployee view (six INNER/LEFT
// OUTER JOINs, still a single statement).
func TestTSQL_CreateView_SingleStatement_AlwaysComplete(t *testing.T) {
	s := realDDLSchema(t)
	if len(s.Views) != 1 {
		t.Fatalf("views = %d, want 1", len(s.Views))
	}
	body := s.Views[0].Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — HYPOTHESIS DISPROVED: the real T-SQL vEmployee view came back incomplete; Note=%q Text=%q", body.Note, body.Text)
	}
	if !strings.Contains(body.Text, "SELECT") || !strings.Contains(body.Text, "INNER JOIN") || !strings.Contains(body.Text, "LEFT OUTER JOIN") {
		t.Errorf("Body.Text = %q, want the full SELECT verbatim (all JOINs present)", body.Text)
	}
}

// TestTSQL_MultiStatementProcedure_PartialCapture is the confirming test for
// the "T-SQL multi-statement procedures capture the WHOLE body to the GO
// batch separator" half of ADR 0027 (formerly: truncate at the first internal
// ';', before de-truncation landed). Fixture: the REAL uspGetBillOfMaterials
// procedure — a recursive CTE (WITH ... UNION ALL ...) followed by an outer
// SELECT, genuinely complex, the architect's recommended candidate (obs
// #1054).
func TestTSQL_MultiStatementProcedure_PartialCapture(t *testing.T) {
	s := realDDLSchema(t)
	body := findProc(t, s, "uspGetBillOfMaterials").Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — the real uspGetBillOfMaterials procedure must be captured whole to GO; Text=%q", body.Text)
	}
	if body.Note != "" {
		t.Errorf("Note = %q, want empty (no truncation occurred)", body.Note)
	}
	if !strings.Contains(body.Text, "SET NOCOUNT ON") {
		t.Errorf("Body.Text = %q, want it to contain the FIRST statement (SET NOCOUNT ON)", body.Text)
	}
	if !strings.Contains(body.Text, "BOM_cte") || !strings.Contains(body.Text, "UNION ALL") || !strings.Contains(body.Text, "MAXRECURSION") {
		t.Errorf("Body.Text = %q, MUST contain the recursive CTE and the outer SELECT — the whole body is captured to GO now", body.Text)
	}
}

// TestTSQL_MultiStatementTrigger_PartialCapture is the confirming test for
// the trigger half of ADR 0027 (full capture to GO). Unlike PostgreSQL (Unit
// A2: TriggerHasInlineBody=false, triggers are wires with no body of their
// own), T-SQL triggers DO carry an inline body (TriggerHasInlineBody=true)
// and so go through the SAME routineBody derivation as procedures — they now
// capture whole to GO exactly like a multi-statement procedure. Fixture: the
// REAL Purchasing.uPurchaseOrderDetail AFTER UPDATE trigger (DECLARE,
// TRY/CATCH, multiple INSERT/UPDATE statements, EXECUTE calls to
// uspPrintError/uspLogError).
func TestTSQL_MultiStatementTrigger_PartialCapture(t *testing.T) {
	s := realDDLSchema(t)
	trig := findTrigger(t, s, "uPurchaseOrderDetail")
	if trig.Table != "PurchaseOrderDetail" {
		t.Errorf("Table = %q, want %q", trig.Table, "PurchaseOrderDetail")
	}
	body := trig.Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — the real uPurchaseOrderDetail trigger must be captured whole to GO; Text=%q", body.Text)
	}
	if body.Note != "" {
		t.Errorf("Note = %q, want empty (no truncation occurred)", body.Note)
	}
	if !strings.Contains(body.Text, "DECLARE @Count") {
		t.Errorf("Body.Text = %q, want it to contain the FIRST statement (DECLARE @Count int)", body.Text)
	}
	if !strings.Contains(body.Text, "BEGIN TRY") || !strings.Contains(body.Text, "INSERT INTO") || !strings.Contains(body.Text, "uspPrintError") {
		t.Errorf("Body.Text = %q, MUST contain the TRY/CATCH block and the INSERT/UPDATE statements — the whole body is captured to GO now", body.Text)
	}
	if trig.ExecutesFunction != "" {
		t.Errorf("ExecutesFunction = %q, want empty (T-SQL triggers embed logic inline, unlike PostgreSQL)", trig.ExecutesFunction)
	}
}

// TestTSQL_TriggerName_SchemaQualified_IsTriggerNameNotSchema locks the fix
// for the reTrigger regex bug discovered while writing the tests above
// (discovery/sqlddl-trigger-name-regex-bug): reTrigger's trigger-NAME capture
// group was missing '.' from its character class, unlike reView/reRoutine's
// name group and reTrigger's own TABLE-name group — so a schema-qualified
// T-SQL trigger name like [Purchasing].[uPurchaseOrderDetail] came back as
// just "Purchasing" (the schema), not "uPurchaseOrderDetail" (the trigger's
// own name). Fixture: the REAL Purchasing.uPurchaseOrderDetail trigger.
func TestTSQL_TriggerName_SchemaQualified_IsTriggerNameNotSchema(t *testing.T) {
	s := realDDLSchema(t)
	// The schema-qualified [Purchasing].[uPurchaseOrderDetail] must resolve to
	// the trigger's OWN name — findTrigger locating it by "uPurchaseOrderDetail"
	// (not "Purchasing") is itself the regression assertion.
	trig := findTrigger(t, s, "uPurchaseOrderDetail")
	if trig.Name != "uPurchaseOrderDetail" {
		t.Errorf("Name = %q, want %q (the trigger's own name, not the schema)", trig.Name, "uPurchaseOrderDetail")
	}
}

// TestTSQL_CleanNegativeProcedure_TryCatch_FullCapture locks the vendored
// CLEAN-NEGATIVE procedure uspUpdateEmployeePersonalInfo (feat/tsql-routine-
// fixtures): a single UPDATE wrapped in BEGIN TRY ... BEGIN CATCH, no dynamic
// SQL. Its multi-statement body (SET NOCOUNT ON; TRY; UPDATE; CATCH) must be
// captured WHOLE to the GO batch separator — Complete=true, no truncation Note
// — so a future DB-031 (missing-exception-handling) rule can read the real
// TRY/CATCH and correctly NOT fire. This is the live proof of ADR 0027
// de-truncation over a second, independent real T-SQL routine.
func TestTSQL_CleanNegativeProcedure_TryCatch_FullCapture(t *testing.T) {
	body := findProc(t, realDDLSchema(t), "uspUpdateEmployeePersonalInfo").Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — the real uspUpdateEmployeePersonalInfo body must be captured whole to GO; Note=%q Text=%q", body.Note, body.Text)
	}
	if body.Note != "" {
		t.Errorf("Note = %q, want empty (no truncation occurred)", body.Note)
	}
	if !strings.Contains(body.Text, "SET NOCOUNT ON") {
		t.Errorf("Body.Text = %q, want the FIRST statement (SET NOCOUNT ON)", body.Text)
	}
	// NOTE: the tokenizer canonicalizes T-SQL [bracket] identifiers to "..".
	// quoting in Body.Text (the on-disk fixture stays verbatim; the parsed body
	// is normalized), so assert on the unquoted keywords/tokens.
	if !strings.Contains(body.Text, "BEGIN TRY") || !strings.Contains(body.Text, "UPDATE") || !strings.Contains(body.Text, "Employee") || !strings.Contains(body.Text, "BEGIN CATCH") {
		t.Errorf("Body.Text = %q, MUST contain the TRY/CATCH block and the UPDATE — the whole body is captured to GO", body.Text)
	}
}

// TestTSQL_NonCascadingTrigger_FullCapture locks the vendored CLEAN-NEGATIVE
// trigger dEmployee (feat/tsql-routine-fixtures): an INSTEAD OF DELETE trigger
// whose whole body is DECLARE/SET/RAISERROR/ROLLBACK — it writes NO other
// table (DB-040 cascade NEGATIVE) and makes NO external call (DB-041
// NEGATIVE). The body still carries internal ';' and must be captured whole to
// GO — Complete=true — so those future trigger rules read the real body and
// correctly do NOT fire.
func TestTSQL_NonCascadingTrigger_FullCapture(t *testing.T) {
	trig := findTrigger(t, realDDLSchema(t), "dEmployee")
	if trig.Table != "Employee" {
		t.Errorf("Table = %q, want %q", trig.Table, "Employee")
	}
	body := trig.Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — the real dEmployee body must be captured whole to GO; Note=%q Text=%q", body.Note, body.Text)
	}
	if body.Note != "" {
		t.Errorf("Note = %q, want empty (no truncation occurred)", body.Note)
	}
	if !strings.Contains(body.Text, "INSTEAD OF DELETE") || !strings.Contains(body.Text, "RAISERROR") {
		t.Errorf("Body.Text = %q, want the RAISERROR-only body verbatim", body.Text)
	}
	// Honest negative: no INSERT/UPDATE/DELETE into any OTHER table, no EXECUTE.
	if strings.Contains(body.Text, "INSERT INTO") || strings.Contains(body.Text, "EXECUTE") {
		t.Errorf("Body.Text = %q, expected NO cascade write and NO external EXECUTE (DB-040/DB-041 negative)", body.Text)
	}
}

// findProc returns the procedure with the given name, failing the test if
// absent — used since the real-object fixture now holds MORE than one
// procedure (positional [0] indexing would be fragile).
func findProc(t *testing.T, s *db.Schema, name string) db.Procedure {
	t.Helper()
	for _, p := range s.Procedures {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("procedure %q not found; have %+v", name, s.Procedures)
	return db.Procedure{}
}

// findTrigger returns the trigger with the given name, failing the test if
// absent.
func findTrigger(t *testing.T, s *db.Schema, name string) db.Trigger {
	t.Helper()
	for _, tr := range s.Triggers {
		if tr.Name == name {
			return tr
		}
	}
	t.Fatalf("trigger %q not found; have %+v", name, s.Triggers)
	return db.Trigger{}
}

// realDDLSchema parses the real, verbatim AdventureWorks excerpt
// (testdata/tsql/adventureworks_real_objects.sql) under the SQLServer
// dialect. Shared by all three tests above — the fixture holds exactly one
// view, one procedure, and one trigger.
func realDDLSchema(t *testing.T) *db.Schema {
	t.Helper()
	return goldenSchema(t, filepath.Join("tsql", "adventureworks_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
}
