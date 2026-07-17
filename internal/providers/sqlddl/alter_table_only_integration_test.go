package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// ALTER TABLE ONLY regression fixture — HALLAZGO 1
// (discovery/sqlddl-postgres-parser-gaps, obs #1062).
//
// PostgreSQL/pg_dump's standard idiom for EVERY constraint is
// "ALTER TABLE ONLY <table> ADD CONSTRAINT ...". reAlterTable
// (internal/providers/sqlddl/reduce.go) used to special-case only
// "IF EXISTS" after "ALTER TABLE", not "ONLY" — so the literal token "ONLY"
// was captured as the table name. getTable("only") then silently created a
// phantom empty table, and the REAL constraint text (which starts with the
// real table name, e.g. "public.customer ADD CONSTRAINT ...") never matched
// any of applyAlterAction's recognized prefixes ("ADD CONSTRAINT", "ADD
// PRIMARY KEY", ...) — it fell to the default "declared limits, skipped"
// case and was dropped entirely, attached to NEITHER the phantom "only"
// table NOR the real one.
//
// This is not just a parsing gap: it makes the SHIPPED RULES lie. DB-050
// (table-without-PK) false-fires on a table that genuinely has a primary
// key upstream (customer), AND fabricates a phantom finding against a table
// that doesn't exist ("only"). DB-001 (FK-without-covering-index) silently
// stops evaluating a real foreign key (payment_p2022_01's FK to customer)
// instead of correctly recognizing it as covered.
//
// pagila_excerpt.sql vendors two real upstream "ALTER TABLE ONLY ..."
// statements (verbatim, commit 5ba5a57) specifically to exercise this:
// customer's real PK and payment_p2022_01's real FK to customer.

func alterTableOnlySchema(t *testing.T) *db.Schema {
	t.Helper()
	return parseFixture(t, "pagila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
}

// Parser-level: the real table gets its PrimaryKey/ForeignKeys populated,
// and no phantom "only" table is fabricated.
func TestAlterTableOnly_ParserPopulatesRealConstraints_NoPhantomTable(t *testing.T) {
	s := alterTableOnlySchema(t)

	var customer, payment *db.Table
	for i := range s.Tables {
		switch s.Tables[i].Name {
		case "customer":
			customer = &s.Tables[i]
		case "payment_p2022_01":
			payment = &s.Tables[i]
		case "ONLY":
			t.Fatalf("phantom table named %q found in parsed schema — ALTER TABLE ONLY's literal \"ONLY\" token was mis-captured as a table name: %+v", s.Tables[i].Name, s.Tables[i])
		}
	}
	if customer == nil {
		t.Fatal("test setup broken: real customer table not found in pagila_excerpt.sql")
	}
	if payment == nil {
		t.Fatal("test setup broken: real payment_p2022_01 table not found in pagila_excerpt.sql")
	}
	if len(customer.PrimaryKey) != 1 || customer.PrimaryKey[0] != "customer_id" {
		t.Errorf("customer.PrimaryKey = %v, want [customer_id] (real upstream ALTER TABLE ONLY public.customer ADD CONSTRAINT customer_pkey PRIMARY KEY (customer_id))", customer.PrimaryKey)
	}
	if len(payment.ForeignKeys) != 1 {
		t.Fatalf("payment_p2022_01.ForeignKeys = %d, want 1 (real upstream ALTER TABLE ONLY public.payment_p2022_01 ADD CONSTRAINT payment_p2022_01_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES public.customer(customer_id))", len(payment.ForeignKeys))
	} else {
		fk := payment.ForeignKeys[0]
		if len(fk.Columns) != 1 || fk.Columns[0] != "customer_id" || fk.RefTable != "customer" {
			t.Errorf("payment_p2022_01.ForeignKeys[0] = %+v, want Columns=[customer_id] RefTable=customer", fk)
		}
	}
}

// Rule-level: DB-050 must NOT false-fire on customer once its real PK is
// vendored via ALTER TABLE ONLY — customer genuinely has a primary key
// upstream.
func TestDB050_AlterTableOnly_NoFalseFireOnCustomer(t *testing.T) {
	s := alterTableOnlySchema(t)
	fs, _ := dbrules.Run(s)

	for _, f := range fs {
		if f.ID == "DB-050" && f.Description == "Table customer has no primary key." {
			t.Errorf("DB-050 false-fired on customer: %+v — customer.PrimaryKey should be [customer_id] via the real vendored ALTER TABLE ONLY statement", f)
		}
	}
}

// Rule-level: DB-050 must not fabricate a finding against a phantom "ONLY"
// table (the literal, uppercase token as it appears in the real DDL — this
// parser does not case-normalize table names) — no such table should exist
// in the schema, and no finding should reference it.
func TestDB050_AlterTableOnly_NoPhantomOnlyTableFinding(t *testing.T) {
	s := alterTableOnlySchema(t)
	fs, _ := dbrules.Run(s)

	for _, f := range fs {
		if f.ID == "DB-050" && f.Description == "Table ONLY has no primary key." {
			t.Errorf("DB-050 fired a phantom finding against a table named %q: %+v — this table must not exist in the parsed schema at all", "ONLY", f)
		}
	}
}

// Rule-level: DB-001 must evaluate payment_p2022_01's real foreign key to
// customer (present in the schema, not silently dropped) — and, since real
// upstream indexes already cover customer_id, correctly conclude it needs
// no surface item (a covered FK is not a finding).
func TestDB001_AlterTableOnly_EvaluatesRealFK(t *testing.T) {
	s := alterTableOnlySchema(t)

	var payment *db.Table
	for i := range s.Tables {
		if s.Tables[i].Name == "payment_p2022_01" {
			payment = &s.Tables[i]
		}
	}
	if payment == nil {
		t.Fatal("test setup broken: real payment_p2022_01 table not found in pagila_excerpt.sql")
	}
	if len(payment.ForeignKeys) == 0 {
		t.Fatal("payment_p2022_01.ForeignKeys is empty — DB-001 has nothing to evaluate; the real FK to customer was dropped by the ALTER TABLE ONLY bug")
	}

	_, surf := dbrules.Run(s)
	var forPayment []findings.SurfaceItem
	for _, item := range surfaceWithCategoryLocal(surf, surface.CategoryDBFKNoIndex) {
		if item.File == payment.ForeignKeys[0].Pos.File {
			forPayment = append(forPayment, item)
		}
	}
	if len(forPayment) != 0 {
		t.Errorf("DB-001 fired %d surface item(s) for payment_p2022_01's FK, want 0 — it is covered by the real vendored idx_fk_payment_p2022_01_customer_id/payment_p2022_01_customer_id_idx indexes on customer_id: %+v", len(forPayment), forPayment)
	}
}
