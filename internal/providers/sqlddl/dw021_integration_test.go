package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DW-021 measured against the REAL sqlddl parser and the REAL S1 paradigm
// classifier — not a hand-built db.Schema/db.Index literal (the recurring
// defect this task calls out). Both the positive fire path and the
// negative/trap fire path are proven here on genuine PostgreSQL DDL text,
// parsed by sqlddl.New(); a T-SQL trap is proven too, since T-SQL contributes
// a SEPARATE vocabulary word (columnstore) that only the real parser can
// actually populate. The star shape (two dimension tables, two foreign keys)
// is the minimum S1 needs to corroborate fact_sales as fact-role — see
// paradigm.Detect's factFanOutMin.

// starSchemaDDL is the shared PostgreSQL DDL for the positive/negative pair
// below: a two-dimension star with fact_sales carrying ONE extra statement
// (the index under test), injected via indexStmt.
func starSchemaDDL(indexStmt string) string {
	return `
CREATE TABLE dim_customer (
    customer_id integer PRIMARY KEY
);

CREATE TABLE dim_product (
    product_id integer PRIMARY KEY
);

CREATE TABLE fact_sales (
    sale_id integer PRIMARY KEY,
    customer_id integer NOT NULL,
    product_id integer NOT NULL,
    amount numeric(12,2)
);

ALTER TABLE fact_sales ADD CONSTRAINT fact_sales_customer_fk FOREIGN KEY (customer_id) REFERENCES dim_customer(customer_id);
ALTER TABLE fact_sales ADD CONSTRAINT fact_sales_product_fk FOREIGN KEY (product_id) REFERENCES dim_product(product_id);

` + indexStmt + `
`
}

// dwSurfaceForPG parses src under the default (PostgreSQL) dialect, confirms
// fact_sales corroborated to fact-role through the REAL S1 classifier, and
// returns DW-021's real surface output.
func dwSurfaceForPG(t *testing.T, src string) []findings.SurfaceItem {
	t.Helper()
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "schema.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	c := paradigm.Detect(s)
	if got := c.Roles["fact_sales"]; got != paradigm.RoleFact {
		t.Fatalf("fact_sales role = %q, want %q — the star shape must corroborate before DW-021 can be tested "+
			"through the real classifier", got, paradigm.RoleFact)
	}
	fs, surf := dwrules.Run(s, &c)
	if len(fs) != 0 {
		t.Fatalf("DW-021 must be SURFACE-only (ADR 0017), got findings: %v", fs)
	}
	return surf
}

// countDW021 counts DW-021 items in surf.
func countDW021(surf []findings.SurfaceItem) int {
	n := 0
	for _, it := range surf {
		if it.Category == string(surface.CategoryDWNoColumnarIndex) {
			n++
		}
	}
	return n
}

// TestDW021_RealPostgreSQLParser_OnlyPlainIndex_Fires is the REAL-PARSER
// positive fire path: fact_sales carries only an ordinary (no access method)
// index — genuinely no columnar/analytic index exists.
func TestDW021_RealPostgreSQLParser_OnlyPlainIndex_Fires(t *testing.T) {
	surf := dwSurfaceForPG(t, starSchemaDDL("CREATE INDEX idx_fact_sales_amount ON fact_sales (amount);"))
	if n := countDW021(surf); n != 1 {
		t.Errorf("DW-021 items for fact_sales = %d, want 1 (real-parsed DDL, no columnar index)", n)
	}
}

// TestDW021_RealPostgreSQLParser_BRINIndex_DoesNotFire is the REAL-PARSER
// negative/trap fire path: fact_sales carries a REAL `USING brin` index,
// captured by the real parser's index-method-capture, not fabricated by a
// test fixture.
func TestDW021_RealPostgreSQLParser_BRINIndex_DoesNotFire(t *testing.T) {
	surf := dwSurfaceForPG(t, starSchemaDDL("CREATE INDEX idx_fact_sales_amount ON fact_sales USING brin (amount);"))
	if n := countDW021(surf); n != 0 {
		t.Errorf("DW-021 items for fact_sales = %d, want 0 (real USING brin index)", n)
	}
}

// TestDW021_RealSQLServerParser_ColumnstoreIndex_DoesNotFire proves the
// SEPARATE T-SQL vocabulary word end to end: a REAL CREATE CLUSTERED
// COLUMNSTORE INDEX statement, parsed by the T-SQL dialect, must populate
// Method="columnstore" and suppress DW-021 — no dialect branch in dw021.go
// makes this work, only the shared vocabulary map reading what the real
// parser captured.
func TestDW021_RealSQLServerParser_ColumnstoreIndex_DoesNotFire(t *testing.T) {
	const src = `
CREATE TABLE [dbo].[dim_customer]([customer_id] [int] NOT NULL);
GO
CREATE TABLE [dbo].[dim_product]([product_id] [int] NOT NULL);
GO
CREATE TABLE [dbo].[fact_sales]([sale_id] [int] NOT NULL, [customer_id] [int] NOT NULL, [product_id] [int] NOT NULL, [amount] [numeric](12,2) NULL);
GO
ALTER TABLE [dbo].[fact_sales] ADD CONSTRAINT [fk_customer] FOREIGN KEY ([customer_id]) REFERENCES [dbo].[dim_customer] ([customer_id]);
GO
ALTER TABLE [dbo].[fact_sales] ADD CONSTRAINT [fk_product] FOREIGN KEY ([product_id]) REFERENCES [dbo].[dim_product] ([product_id]);
GO
CREATE CLUSTERED COLUMNSTORE INDEX [cci_fact_sales] ON [dbo].[fact_sales];
GO
`
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "schema.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	c := paradigm.Detect(s)
	if got := c.Roles["fact_sales"]; got != paradigm.RoleFact {
		t.Fatalf("fact_sales role = %q, want %q", got, paradigm.RoleFact)
	}
	fact := tableNamed(t, s, "fact_sales")
	if len(fact.Indexes) != 1 || fact.Indexes[0].Method != "columnstore" {
		t.Fatalf("fact_sales.Indexes = %+v, want one index with Method=columnstore — the real T-SQL parser did "+
			"not capture what this test claims to exercise", fact.Indexes)
	}
	_, surf := dwrules.Run(s, &c)
	if n := countDW021(surf); n != 0 {
		t.Errorf("DW-021 items for fact_sales = %d, want 0 (real T-SQL CLUSTERED COLUMNSTORE INDEX)", n)
	}
}

// TestDW021_RealBlindShape_ONONLY_Abstains is the end-to-end abstention proof
// the task requires: a REAL genuinely-unrecognized index shape (PostgreSQL's
// `ON ONLY`, per internal/providers/sqlddl/testdata/
// pg_constructed_unrecognized_index_forms.sql) marks fact_sales unproven, and
// DW-021 emits NOTHING for it — even though fact_sales otherwise has zero
// columnar indexes, which would deterministically fire per
// TestDW021_RealPostgreSQLParser_OnlyPlainIndex_Fires above. This is proven
// through the real StructureProven() gate, with NO dialect branch in
// dw021.go — the whole thesis of index-method-capture (PR #79) and this
// slice, exercised together.
func TestDW021_RealBlindShape_ONONLY_Abstains(t *testing.T) {
	src := starSchemaDDL("CREATE INDEX idx_fact_sales_amount ON ONLY fact_sales (amount);")
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "schema.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	fact := tableNamed(t, s, "fact_sales")
	if fact.StructureProven() {
		t.Fatalf("fact_sales.StructureProven() = true, want false — ON ONLY is a genuinely unrecognized " +
			"CREATE INDEX shape (see the SQL-DDL known limits in dbcoverage.go) and must mark the table unproven")
	}

	c := paradigm.Detect(s)
	if got := c.Roles["fact_sales"]; got != paradigm.RoleFact {
		t.Fatalf("fact_sales role = %q, want %q", got, paradigm.RoleFact)
	}

	_, surf := dwrules.Run(s, &c)
	if n := countDW021(surf); n != 0 {
		t.Errorf("DW-021 items for fact_sales = %d, want 0 — must ABSTAIN on an unproven table, not fire on "+
			"absence over a dropped statement", n)
	}
}
