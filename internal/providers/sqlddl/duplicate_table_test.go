package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// parsePGTableNamed parses sql under the default (PostgreSQL) dialect and
// returns the table matching name, failing the test if absent.
func parsePGTableNamed(t *testing.T, sql, name string) db.Table {
	t.Helper()
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found in parsed schema (tables: %v)", name, s.Tables)
	return db.Table{}
}

// F3 (4R ledger, obs #1282, WARNING — the false affirmation survives): a
// second CREATE TABLE for an already-known name is skipped WHOLE
// (reduce.go, "AJUSTE 1") — that is correct, declared behavior ONLY for the
// explicit "IF NOT EXISTS" case (the first CREATE legitimately wins by SQL
// semantics). WITHOUT "IF NOT EXISTS", a second CREATE TABLE for the same
// name is a genuine anomaly — most commonly `normalizeName` (reduce.go)
// stripping the schema qualifier so two DIFFERENT tables (public.users,
// audit.users) collapse into one name — and the second declaration's
// columns/constraints are silently discarded with no MarkUnproven, so
// Complete stayed true over a table missing real, declared structure. This
// drop site was not among the five design originally enumerated.
func TestSQLDDL_DuplicateCreateTable_WithoutIfNotExists_MarksUnproven(t *testing.T) {
	sql := "CREATE TABLE public.users (id int, email text);\n" +
		"CREATE TABLE audit.users (id int, event text);\n"
	tb := parsePGTableNamed(t, sql, "users")
	if tb.Complete {
		t.Error("Complete = true, want false — a second, non-IF-NOT-EXISTS CREATE TABLE for the same " +
			"normalized name silently discarded real declared structure")
	}
	if !strings.Contains(tb.Note, db.ReasonUnreducedTableStatement) {
		t.Errorf("Note = %q, want it to contain ReasonUnreducedTableStatement", tb.Note)
	}
	if len(tb.Unreduced) != 1 || !strings.Contains(tb.Unreduced[0].Text, "audit.users") {
		t.Errorf("Unreduced = %v, want one entry carrying the verbatim second CREATE TABLE statement", tb.Unreduced)
	}
	// The first declaration's own columns still win (AJUSTE 1's "no
	// Frankenstein merge" — unchanged), it is the COMPLETENESS signal that
	// changes, not which columns survive.
	if len(tb.Columns) != 2 || tb.Columns[0].Name != "id" || tb.Columns[1].Name != "email" {
		t.Errorf("Columns = %v, want the FIRST declaration's columns (id, email) unchanged", tb.Columns)
	}
}

// Positive control: explicit "IF NOT EXISTS" is the DECLARED, RECOGNIZED
// skip (ADR 0018) — the SQL itself says "only create if absent", so the
// no-op is genuinely safe and must NOT demote completeness.
func TestSQLDDL_DuplicateCreateTable_WithIfNotExists_StaysComplete(t *testing.T) {
	sql := "CREATE TABLE t (id int);\n" +
		"CREATE TABLE IF NOT EXISTS t (id int, extra text);\n"
	tb := parsePGTable(t, sql)
	if !tb.Complete {
		t.Error("Complete = false, want true — CREATE TABLE IF NOT EXISTS is a declared, recognized skip (ADR 0018)")
	}
	if tb.Note != "" {
		t.Errorf("Note = %q, want empty", tb.Note)
	}
}
