package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// REL-003 (4R reliability lens): CLUSTERED/NONCLUSTERED is the ORDINARY
// standalone index form in T-SQL, not an exotic one — reCreateIndex only
// recognizes an UNQUALIFIED "CREATE [UNIQUE] INDEX ... ON ...", so on an
// ordinary SQL Server project every table carrying a plain CLUSTERED or
// NONCLUSTERED index now becomes Complete=false via reIndexShapedHead's
// default: branch. This is the CORRECT, honest outcome per ADR 0034 SS2.4 —
// the parser genuinely cannot read those statements — but it was untested
// under the real T-SQL dialect: TestSQLDDL_UnrecognizedIndexForms_Fixture_...
// parses everything under the default PostgreSQL dialect and only covers
// CLUSTERED COLUMNSTORE, an anonymous index, and ON ONLY.
//
// This test drives the REAL parser via WithDialect(SQLServer()) and locks
// two things: (1) the demoted table itself, and (2) the schema-wide
// amplification this feeds through paradigm.unprovableDemotions — one
// incomplete table (orders, no recognized fact_/dim_ prefix, so never
// Unprovable itself) still sets anyIncomplete and marks EVERY
// recognized-prefix demotion in the WHOLE schema unprovable, including a
// dimension (dim_customer) that is itself proven complete and has nothing
// to do with the dropped index. Per the architect's instruction: this
// demotion must NOT be softened — if the honest cost is real, it must be
// measurable, not hidden.
func TestSQLDDL_TSQL_OrdinaryClusteredIndex_MarksTableUnproven_AndAmplifiesSchemaWideUnprovable(t *testing.T) {
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
	if orders.Complete {
		t.Error("orders.Complete = true, want false — a plain T-SQL NONCLUSTERED INDEX is the ORDINARY standalone form, and reCreateIndex does not recognize it")
	}
	if !strings.Contains(orders.Note, string(db.ReasonUnreducedTableStatement)) {
		t.Errorf("orders.Note = %q, want it to contain ReasonUnreducedTableStatement", orders.Note)
	}

	// Schema-wide amplification: dim_customer has zero fan-in (nothing
	// references it), so it stays RoleUnclassified regardless of this
	// change — confirm the baseline before asserting the amplification.
	cls := paradigm.Detect(s)
	if cls.Roles["dim_customer"] != paradigm.RoleUnclassified {
		t.Fatalf("dim_customer role = %q, want unclassified (fan-in is zero)", cls.Roles["dim_customer"])
	}
	if !cls.Unprovable["dim_customer"] {
		t.Error("Unprovable[dim_customer] = false, want true — orders' dropped NONCLUSTERED INDEX statement sets anyIncomplete, which marks EVERY recognized-prefix demotion schema-wide unprovable, even for a table (dim_customer) that is itself proven complete")
	}
	// orders itself carries no recognized fact_/dim_ prefix, so it is never
	// marked Unprovable regardless of its own Complete=false — the
	// demotion cause for an unprefixed table is never "structure", proving
	// this amplification is not a blanket "mark everything" bug.
	if cls.Unprovable["orders"] {
		t.Error("Unprovable[orders] = true, want false — orders has no recognized fact_/dim_ prefix, so it is never a candidate for Unprovable regardless of its own completeness")
	}
}
