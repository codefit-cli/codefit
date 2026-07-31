package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// F2 (coordinator review, index-method-capture — regression, confirmed by
// running it): making the index-name capture group optional (to admit
// PostgreSQL's anonymous form) also brought PostgreSQL's anonymous
// EXPRESSION index into reCreateIndex's reach — idiomatic PG
// (CREATE INDEX ON t (lower(email))). reCreateIndex's own column-list
// grammar, \(([^)]*)\), stops at the FIRST ')', so it FABRICATES a column
// literally named "lower(email" instead of failing to match, and the table
// stays Complete=true — a regression from HONEST ABSTENTION (before this
// slice, this statement fell to the floor and was recorded) to SILENT
// FABRICATION. Parsing SQL expressions is explicitly out of scope; the fix
// is to detect the unbalanced/nested-paren column list and fall to the SAME
// floor a genuinely unrecognized CREATE INDEX-shaped statement uses.
func TestSQLDDL_PG_ExpressionIndex_DoesNotFabricate_FallsToFloor(t *testing.T) {
	sql := "CREATE TABLE users (id int PRIMARY KEY, email text);\n" +
		"CREATE INDEX ON users (lower(email));\n"
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tb := table(t, s, "users")
	if tb.Complete {
		t.Fatalf("Complete = true, want false — an expression index must fall to the floor, not be silently proven complete")
	}
	for _, ix := range tb.Indexes {
		for _, c := range ix.Columns {
			if c == "lower(email" || c == "lower(email)" {
				t.Fatalf("fabricated column %q found in Indexes = %+v — the expression index must never invent a column name", c, tb.Indexes)
			}
		}
	}
	if len(tb.Unreduced) != 1 {
		t.Fatalf("Unreduced = %v, want exactly 1 entry recording the dropped statement", tb.Unreduced)
	}
}

// TestSQLDDL_PG_NamedExpressionIndex_DoesNotFabricate_FallsToFloor covers the
// SAME defect on the NAMED form (index name present, only the column list is
// an expression) — the paren-balance guard must not be accidentally
// conditioned on the name being absent.
func TestSQLDDL_PG_NamedExpressionIndex_DoesNotFabricate_FallsToFloor(t *testing.T) {
	sql := "CREATE TABLE users (id int PRIMARY KEY, email text);\n" +
		"CREATE INDEX idx_lower_email ON users (lower(email));\n"
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tb := table(t, s, "users")
	if tb.Complete {
		t.Fatalf("Complete = true, want false — a NAMED expression index must also fall to the floor")
	}
	if len(tb.Indexes) != 0 {
		t.Fatalf("Indexes = %+v, want empty — nothing fabricated", tb.Indexes)
	}
}

// TestSQLDDL_PG_OrdinaryIndex_StillWorks_AfterParenBalanceGuard is the
// positive control: a normal, non-expression column list is completely
// unaffected by the new guard.
func TestSQLDDL_PG_OrdinaryIndex_StillWorks_AfterParenBalanceGuard(t *testing.T) {
	sql := "CREATE TABLE users (id int PRIMARY KEY, email text);\n" +
		"CREATE INDEX idx_email ON users (email);\n"
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tb := table(t, s, "users")
	if !tb.Complete {
		t.Fatalf("Complete = false, want true — an ordinary index must be unaffected. Note=%q Unreduced=%v", tb.Note, tb.Unreduced)
	}
	if len(tb.Indexes) != 1 || len(tb.Indexes[0].Columns) != 1 || tb.Indexes[0].Columns[0] != "email" {
		t.Fatalf("Indexes = %+v, want exactly [email]", tb.Indexes)
	}
}
