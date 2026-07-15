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
// architecture/tsql-body-truncation-limit (obs #1050) STATES AS PROBABLE, NOT
// CONFIRMED, that T-SQL views are unaffected by the body-truncation limit
// (a CREATE VIEW ... AS SELECT ... is a single statement with no internal
// ';'), while multi-statement T-SQL PROCEDURES and TRIGGERS truncate at the
// first internal ';'. This file is the confirmation the architect required
// before COVERAGE.md may say so — the first pass (Unit B) confirmed it
// against hand-written "in the style of AdventureWorks" DDL; THIS pass
// (Unit B2) re-confirms the identical three behaviors against a REAL,
// VERBATIM excerpt of upstream AdventureWorks DDL, per the architect's
// binding Condition 1 (obs #1054): "the DDL must come from UPSTREAM
// AdventureWorks, COPIED, not rewritten in the style of."
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
// the "T-SQL multi-statement procedures truncate at the first internal ';'"
// half of the hypothesis. Fixture: the REAL uspGetBillOfMaterials procedure
// — a recursive CTE (WITH ... UNION ALL ...) followed by an outer SELECT,
// genuinely complex, the architect's recommended candidate (obs #1054).
func TestTSQL_MultiStatementProcedure_PartialCapture(t *testing.T) {
	s := realDDLSchema(t)
	if len(s.Procedures) != 1 {
		t.Fatalf("procedures = %d, want 1", len(s.Procedures))
	}
	body := s.Procedures[0].Body
	if body.Complete {
		t.Errorf("Complete = true, want false — HYPOTHESIS DISPROVED: the real uspGetBillOfMaterials procedure came back complete; Text=%q", body.Text)
	}
	if body.Note == "" {
		t.Error("Note = \"\", want a non-empty reason explaining the truncation")
	}
	if !strings.Contains(body.Text, "SET NOCOUNT ON") {
		t.Errorf("Body.Text = %q, want it to contain the FIRST statement (SET NOCOUNT ON)", body.Text)
	}
	if strings.Contains(body.Text, "BOM_cte") || strings.Contains(body.Text, "UNION ALL") || strings.Contains(body.Text, "MAXRECURSION") {
		t.Errorf("Body.Text = %q, must NOT contain the recursive CTE or the outer SELECT — only text up to the first internal ';' may be captured", body.Text)
	}
}

// TestTSQL_MultiStatementTrigger_PartialCapture is the confirming test for
// the trigger half of the same hypothesis. Unlike PostgreSQL (Unit A2:
// TriggerHasInlineBody=false, triggers are wires with no body of their own),
// T-SQL triggers DO carry an inline body (TriggerHasInlineBody=true) and so
// go through the SAME routineBody derivation as procedures — they CAN
// truncate exactly like a multi-statement procedure. Fixture: the REAL
// Purchasing.uPurchaseOrderDetail AFTER UPDATE trigger (DECLARE, TRY/CATCH,
// multiple INSERT/UPDATE statements, EXECUTE calls to uspPrintError/
// uspLogError).
func TestTSQL_MultiStatementTrigger_PartialCapture(t *testing.T) {
	s := realDDLSchema(t)
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	trig := s.Triggers[0]
	if trig.Table != "PurchaseOrderDetail" {
		t.Errorf("Table = %q, want %q", trig.Table, "PurchaseOrderDetail")
	}
	body := trig.Body
	if body.Complete {
		t.Errorf("Complete = true, want false — HYPOTHESIS DISPROVED: the real uPurchaseOrderDetail trigger came back complete; Text=%q", body.Text)
	}
	if body.Note == "" {
		t.Error("Note = \"\", want a non-empty reason explaining the truncation")
	}
	if !strings.Contains(body.Text, "DECLARE @Count") {
		t.Errorf("Body.Text = %q, want it to contain the FIRST statement (DECLARE @Count int)", body.Text)
	}
	if strings.Contains(body.Text, "BEGIN TRY") || strings.Contains(body.Text, "INSERT INTO") || strings.Contains(body.Text, "uspPrintError") {
		t.Errorf("Body.Text = %q, must NOT contain the TRY/CATCH block or the INSERT/UPDATE statements — only text up to the first internal ';' may be captured", body.Text)
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
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	trig := s.Triggers[0]
	if trig.Name != "uPurchaseOrderDetail" {
		t.Errorf("Name = %q, want %q (the trigger's own name, not the schema)", trig.Name, "uPurchaseOrderDetail")
	}
}

// realDDLSchema parses the real, verbatim AdventureWorks excerpt
// (testdata/tsql/adventureworks_real_objects.sql) under the SQLServer
// dialect. Shared by all three tests above — the fixture holds exactly one
// view, one procedure, and one trigger.
func realDDLSchema(t *testing.T) *db.Schema {
	t.Helper()
	return goldenSchema(t, filepath.Join("tsql", "adventureworks_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
}
