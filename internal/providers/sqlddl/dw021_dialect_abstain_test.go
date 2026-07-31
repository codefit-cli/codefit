package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DW-021's declared MySQL dialect-wide abstain (S3, db-olap-rules), proven
// against the REAL sqlddl parser/reducer rather than a hand-built db.Schema —
// this is what actually sets Schema.Dialect and (fails to) capture an
// index's Method, the two mechanisms the abstain depends on.
//
// The table's ROLE is hand-assigned RoleFact rather than derived through
// paradigm.Detect (the same choice dw001_test.go's family documents): this
// test is about the DIALECT gate, not about the detection heuristic's FK
// fan-out corroboration, which has its own dedicated tests.

const fact021DDL = "CREATE TABLE fact_sales (id int, amount int);\n" +
	"CREATE INDEX idx_fact_sales_id ON fact_sales (id);\n"

func TestDW021_RealMySQLParse_AbstainsOnDialectBlindness(t *testing.T) {
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(fact021DDL)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if s.Dialect != "mysql" {
		t.Fatalf("Schema.Dialect = %q, want %q (precondition for this test)", s.Dialect, "mysql")
	}

	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"fact_sales": paradigm.RoleFact}}
	_, surf := dwrules.Run(s, cls)

	for _, it := range surf {
		if it.Category == string(surface.CategoryDWNoColumnarIndex) {
			t.Fatalf("DW-021 fired over a MySQL-parsed schema: %+v — must abstain, the parser cannot reliably "+
				"read an index's method on this dialect", it)
		}
	}
}

// Positive control, the identical shape under PostgreSQL: proves the abstain
// above is DIALECT-specific, not a general "the rule stopped firing" break.
func TestDW021_RealPostgreSQLParse_StillFires(t *testing.T) {
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(fact021DDL)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if s.Dialect != "postgresql" {
		t.Fatalf("Schema.Dialect = %q, want %q (precondition for this test)", s.Dialect, "postgresql")
	}

	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"fact_sales": paradigm.RoleFact}}
	_, surf := dwrules.Run(s, cls)

	var found bool
	for _, it := range surf {
		if it.Category == string(surface.CategoryDWNoColumnarIndex) {
			found = true
		}
	}
	if !found {
		t.Error("DW-021 must still fire on the identical shape parsed under PostgreSQL (idx_fact_sales_id has no columnar method)")
	}
}
