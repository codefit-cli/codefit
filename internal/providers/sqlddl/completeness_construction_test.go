package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// N1 (design §1-D1b, spec DELTA 1) — the zero-value trap. db.Table.Complete's
// zero value is false (fail-closed). getTable (reduce.go) is the SQL-DDL
// provider's sole construction site: it MUST set Complete=true explicitly, or
// every table of every SQL-DDL-parsed project mutes DB-050/DB-001/DB-052 and
// the whole DW family. This test goes RED against the zero-value the moment
// db.Table gains the field (WU-1) and stays red until getTable sets it
// (this commit's GREEN).
func TestSQLDDL_WellFormedTable_IsComplete(t *testing.T) {
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(
		"CREATE TABLE t (id int PRIMARY KEY, name varchar(50));\n",
	)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 1 {
		t.Fatalf("parsed %d tables, want 1", len(s.Tables))
	}
	tb := s.Tables[0]
	if !tb.Complete {
		t.Error("Complete = false, want true — a well-formed CREATE TABLE with no dropped statements must be proven complete (N1)")
	}
	if tb.Note != "" {
		t.Errorf("Note = %q, want empty on a complete table", tb.Note)
	}
	if tb.Unreduced != nil {
		t.Errorf("Unreduced = %v, want nil on a complete table", tb.Unreduced)
	}
}
