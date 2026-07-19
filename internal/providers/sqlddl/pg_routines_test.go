package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Real PostgreSQL Pagila routine coverage (feat/tsql-routine-fixtures): the
// Pagila PL/pgSQL rewards_report is the DB-030 dynamic-SQL POSITIVE that
// neither the MySQL Sakila nor the T-SQL AdventureWorks corpus contains. The
// body is byte-for-byte upstream (see testdata/pagila_real_objects.sql's
// header for source + commit + license and the byte-diff provenance).
// findProc lives in tsql_hypothesis_test.go.

// TestPostgres_PagilaRewardsReport_DynamicSQL_FullCapture — the Pagila PL/pgSQL
// rewards_report BUILDS a query string in tmpSQL and runs it with
// "EXECUTE tmpSQL" (plus "FOR rr IN EXECUTE '...'"): a real DB-030 dynamic-SQL
// POSITIVE, and (no EXCEPTION block) a DB-031 POSITIVE. The dollar-quoted
// ($_$...$_$) body carries many internal ';' — captured whole (Complete=true),
// with the dynamic EXECUTE present verbatim.
func TestPostgres_PagilaRewardsReport_DynamicSQL_FullCapture(t *testing.T) {
	s := goldenSchema(t, "pagila_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	body := findProc(t, s, "rewards_report").Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — the dollar-quoted Pagila rewards_report body must be captured whole; Note=%q Text=%q", body.Note, body.Text)
	}
	if !strings.Contains(body.Text, "EXECUTE tmpSQL") {
		t.Errorf("Body.Text = %q, want the dynamic-SQL EXECUTE (DB-030 positive) present verbatim", body.Text)
	}
	if !strings.Contains(body.Text, "FOR rr IN EXECUTE") {
		t.Errorf("Body.Text = %q, want the second dynamic EXECUTE (whole body captured)", body.Text)
	}
	if !strings.Contains(body.Text, "DROP TABLE tmpCustomer") {
		t.Errorf("Body.Text = %q, want the LAST internal statement (whole body captured, not cut at first ';')", body.Text)
	}
}
