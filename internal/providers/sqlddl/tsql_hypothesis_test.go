package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Unit B (db-debt-views-and-nplus1, Phase 2.2): T-SQL fixture proof.
//
// architecture/tsql-body-truncation-limit (obs #1050) STATES AS PROBABLE, NOT
// CONFIRMED, that T-SQL views are unaffected by the body-truncation limit
// (a CREATE VIEW ... AS SELECT ... is a single statement with no internal
// ';'), while multi-statement T-SQL PROCEDURES and TRIGGERS truncate at the
// first internal ';'. This file is the confirmation (or disproof) the
// architect required before COVERAGE.md may say so.
//
// Fixture provenance: the existing golden fixture
// (testdata/tsql/adventureworks_excerpt.sql, used by golden_test.go's
// byte-for-byte snapshot) contains ONLY CREATE TABLE/INDEX statements — no
// view, procedure, or trigger — confirmed by inspection before writing this
// file. Rather than mutate that golden (which would force regenerating its
// JSON snapshot and rewriting Unit A's TestSQLDDL_ExistingGoldens_
// UnaffectedByBodyField 0/0/0 assertion for reasons unrelated to Unit B), the
// fixtures below are new, self-contained, multi-line T-SQL DDL "in the style
// of" AdventureWorks — the SAME provenance discipline the existing
// adventureworks_excerpt.sql fixture itself already declares in its own
// trailer comment ("Focused excerpt in the style of the SQL Server
// AdventureWorks sample database ... assembled for this test only"), not a
// new practice invented here. They reuse the exact table/column vocabulary
// already established in that golden fixture (Customer, SalesOrderHeader)
// for continuity, and echo real, well-known AdventureWorks stored-procedure
// conventions (bracket-qualified [dbo].[X] names, a leading "SET NOCOUNT
// ON;" — the literal first line of nearly every real AdventureWorks
// uspGet*/uspUpdate* procedure). This is deliberately NOT a synthetic
// one-liner (contrast with the placeholder p()/t()/@a fixtures Unit A used
// for the equivalent generic-case tests).
//
// Reference: https://github.com/microsoft/sql-server-samples (AdventureWorks)

// TestTSQL_CreateView_SingleStatement_AlwaysComplete is the confirming test
// for the "T-SQL views are unaffected" half of the hypothesis: a CREATE VIEW
// body is one SELECT with no internal ';' in any dialect (reduce.go's
// viewBody is unconditional — it does not even branch on dialect), so it
// must come back Complete=true on T-SQL exactly as it does on every other
// dialect.
func TestTSQL_CreateView_SingleStatement_AlwaysComplete(t *testing.T) {
	src := `CREATE VIEW [dbo].[vSalesOrderCustomerSummary]
AS
SELECT
    soh.[SalesOrderId],
    soh.[OrderDate],
    soh.[TotalDue],
    c.[AccountNumber]
FROM [dbo].[SalesOrderHeader] AS soh
INNER JOIN [dbo].[Customer] AS c ON c.[CustomerId] = soh.[CustomerId]
WHERE c.[IsActive] = 1;
GO`
	s := parseDialect(t, sqlddl.SQLServer(), src)
	if len(s.Views) != 1 {
		t.Fatalf("views = %d, want 1", len(s.Views))
	}
	body := s.Views[0].Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — HYPOTHESIS DISPROVED: a T-SQL CREATE VIEW came back incomplete; Note=%q Text=%q", body.Note, body.Text)
	}
	if !strings.Contains(body.Text, "SELECT") || !strings.Contains(body.Text, "INNER JOIN") {
		t.Errorf("Body.Text = %q, want the full SELECT verbatim", body.Text)
	}
}

// TestTSQL_MultiStatementProcedure_PartialCapture is the confirming test for
// the "T-SQL multi-statement procedures truncate at the first internal ';'"
// half of the hypothesis, on realistic (not placeholder) fixture text.
func TestTSQL_MultiStatementProcedure_PartialCapture(t *testing.T) {
	src := `CREATE PROCEDURE [dbo].[uspGetCustomerOrderHistory]
    @CustomerId INT
AS
BEGIN
    SET NOCOUNT ON;
    SELECT [SalesOrderId], [OrderDate], [TotalDue]
    FROM [dbo].[SalesOrderHeader]
    WHERE [CustomerId] = @CustomerId;
    UPDATE [dbo].[Customer]
    SET [ModifiedDate] = GETDATE()
    WHERE [CustomerId] = @CustomerId;
END
GO`
	s := parseDialect(t, sqlddl.SQLServer(), src)
	if len(s.Procedures) != 1 {
		t.Fatalf("procedures = %d, want 1", len(s.Procedures))
	}
	body := s.Procedures[0].Body
	if body.Complete {
		t.Errorf("Complete = true, want false — HYPOTHESIS DISPROVED: a multi-statement T-SQL procedure came back complete; Text=%q", body.Text)
	}
	if body.Note == "" {
		t.Error("Note = \"\", want a non-empty reason explaining the truncation")
	}
	if !strings.Contains(body.Text, "SET NOCOUNT ON") {
		t.Errorf("Body.Text = %q, want it to contain the FIRST statement (SET NOCOUNT ON)", body.Text)
	}
	if strings.Contains(body.Text, "SELECT [SalesOrderId]") || strings.Contains(body.Text, "UPDATE [dbo].[Customer]") {
		t.Errorf("Body.Text = %q, must NOT contain statements 2/3 — only text up to the first internal ';' may be captured", body.Text)
	}
}

// TestTSQL_MultiStatementTrigger_PartialCapture is the confirming test for
// the trigger half of the same hypothesis. Unlike PostgreSQL (Unit A2:
// TriggerHasInlineBody=false, triggers are wires with no body of their own),
// T-SQL triggers DO carry an inline body (TriggerHasInlineBody=true) and so
// go through the SAME routineBody derivation as procedures — they CAN
// truncate exactly like a multi-statement procedure.
func TestTSQL_MultiStatementTrigger_PartialCapture(t *testing.T) {
	src := `CREATE TRIGGER [dbo].[trgSalesOrderHeader_UpdateAudit]
ON [dbo].[SalesOrderHeader]
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE [dbo].[Customer]
    SET [ModifiedDate] = GETDATE()
    WHERE [CustomerId] IN (SELECT [CustomerId] FROM inserted);
    SELECT @@ROWCOUNT AS UpdatedRows;
END
GO`
	s := parseDialect(t, sqlddl.SQLServer(), src)
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	body := s.Triggers[0].Body
	if body.Complete {
		t.Errorf("Complete = true, want false — HYPOTHESIS DISPROVED: a multi-statement T-SQL trigger came back complete; Text=%q", body.Text)
	}
	if body.Note == "" {
		t.Error("Note = \"\", want a non-empty reason explaining the truncation")
	}
	if !strings.Contains(body.Text, "SET NOCOUNT ON") {
		t.Errorf("Body.Text = %q, want it to contain the FIRST statement (SET NOCOUNT ON)", body.Text)
	}
	if strings.Contains(body.Text, "UPDATE [dbo].[Customer]") || strings.Contains(body.Text, "SELECT @@ROWCOUNT") {
		t.Errorf("Body.Text = %q, must NOT contain statements 2/3 — only text up to the first internal ';' may be captured", body.Text)
	}
	if s.Triggers[0].ExecutesFunction != "" {
		t.Errorf("ExecutesFunction = %q, want empty (T-SQL triggers embed logic inline, unlike PostgreSQL)", s.Triggers[0].ExecutesFunction)
	}
}
