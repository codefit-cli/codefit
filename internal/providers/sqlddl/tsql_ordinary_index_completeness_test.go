package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// index-method-capture: this test used to lock the OPPOSITE outcome — that a
// plain T-SQL CLUSTERED/NONCLUSTERED index (the ORDINARY standalone index
// form in T-SQL, not an exotic one) demoted its table to Complete=false and
// amplified schema-wide via paradigm.Classification.Unprovable, because
// reCreateIndex only recognized an unqualified "CREATE [UNIQUE] INDEX ... ON
// ...". That was the CORRECT, honest outcome at the time per ADR 0034 SS2.4 —
// the parser genuinely could not read the statement — but it was a declared,
// deferred parser-shape gap (dbcoverage.go's known-limits item (7)), not a
// permanent boundary.
//
// index-method-capture teaches reduce.go's reCreateIndex to actually READ
// CLUSTERED/NONCLUSTERED (reduce.go:47) — the SAME statement shape as an
// ordinary index, just an extra keyword. This test now locks the OPPOSITE,
// CORRECT-going-forward outcome: the table parses cleanly, Method carries the
// declared kind, and NOTHING amplifies schema-wide, because there is no
// longer anything to demote.
func TestSQLDDL_TSQL_OrdinaryClusteredIndex_ParsesWithMethod_NoAmplification(t *testing.T) {
	sql := `
CREATE TABLE orders (
    order_id int NOT NULL,
    customer_id int NOT NULL
);

CREATE NONCLUSTERED INDEX idx_orders_customer ON orders (customer_id);

CREATE TABLE dim_customer (
    customer_id int NOT NULL
);
`
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 2 {
		t.Fatalf("parsed %d tables, want 2 (orders, dim_customer)", len(s.Tables))
	}

	byName := map[string]db.Table{}
	for _, tb := range s.Tables {
		byName[tb.Name] = tb
	}

	orders, ok := byName["orders"]
	if !ok {
		t.Fatalf("table orders not found (tables: %v)", s.Tables)
	}
	if !orders.Complete {
		t.Errorf("orders.Complete = false, want true — reCreateIndex now recognizes T-SQL's ordinary CLUSTERED/NONCLUSTERED index form. Note=%q Unreduced=%v", orders.Note, orders.Unreduced)
	}
	if len(orders.Indexes) != 1 {
		t.Fatalf("orders.Indexes = %v, want exactly 1", orders.Indexes)
	}
	if got := orders.Indexes[0].Method; got != "nonclustered" {
		t.Errorf("orders.Indexes[0].Method = %q, want %q", got, "nonclustered")
	}

	// Schema-wide amplification must NOT fire anymore: dim_customer has zero
	// fan-in (RoleUnclassified, unaffected by this change) and, critically,
	// is no longer marked Unprovable — there is no unproven table left in
	// this schema to amplify from.
	cls := paradigm.Detect(s)
	if cls.Roles["dim_customer"] != paradigm.RoleUnclassified {
		t.Fatalf("dim_customer role = %q, want unclassified (fan-in is zero)", cls.Roles["dim_customer"])
	}
	if cls.Unprovable["dim_customer"] {
		t.Error("Unprovable[dim_customer] = true, want false — orders' NONCLUSTERED INDEX statement is now RECOGNIZED, so nothing sets anyIncomplete and nothing amplifies schema-wide")
	}
	if cls.Unprovable["orders"] {
		t.Error("Unprovable[orders] = true, want false — \"orders\" carries no recognized warehouse name token (no delimited fact/dim segment, no PascalCase Fact/Dim boundary), so it was never a candidate for Unprovable regardless of completeness")
	}
}
