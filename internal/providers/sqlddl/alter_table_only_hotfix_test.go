package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Isolated hotfix regression — HALLAZGO 1 (discovery/sqlddl-postgres-parser-gaps,
// obs #1062).
//
// reAlterTable (internal/providers/sqlddl/reduce.go) used to special-case only
// "IF EXISTS" after "ALTER TABLE", not PostgreSQL/pg_dump's "ONLY" keyword —
// PostgreSQL's standard idiom for EVERY constraint is
// "ALTER TABLE ONLY <table> ADD CONSTRAINT ...". The literal token "ONLY" was
// captured as the table name, so getTable("ONLY") silently created a phantom
// table and the real constraint text (which starts with the real table name)
// never matched applyAlterAction's recognized prefixes — it was dropped
// entirely, attached to NEITHER the phantom table NOR the real one.
//
// This makes the SHIPPED RULES lie: DB-050 false-fires on a table that
// genuinely has a primary key upstream, and DB-001 silently stops evaluating
// a real foreign key instead of correctly reasoning about it.
//
// Self-contained: the DDL below is an inline Go string constant, read from no
// fixture file, so this test proves the fix in isolation of any committed
// testdata. Deliberately named/scoped to NOT collide with
// alter_table_only_integration_test.go (feat/db-body-capture, real Pagila
// fixture) so both files coexist post-merge.
const alterTableOnlyHotfixDDL = `
CREATE TABLE customer (customer_id integer NOT NULL, name text);
CREATE TABLE rental (rental_id integer NOT NULL, customer_id integer);
ALTER TABLE ONLY customer ADD CONSTRAINT customer_pkey PRIMARY KEY (customer_id);
ALTER TABLE ONLY rental ADD CONSTRAINT rental_customer_fk FOREIGN KEY (customer_id) REFERENCES customer(customer_id);
`

func alterTableOnlyHotfixSchema(t *testing.T) *db.Schema {
	t.Helper()
	srcs := []providers.SourceFile{{Path: "inline.sql", Content: []byte(alterTableOnlyHotfixDDL)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	return s
}

// Parser-level: no phantom "ONLY" table is fabricated, and the real tables
// get their PrimaryKey/ForeignKeys populated from the ALTER TABLE ONLY
// statements instead of having the constraint silently dropped.
func TestAlterTableOnly_Inline_NoPhantomOnlyTable(t *testing.T) {
	s := alterTableOnlyHotfixSchema(t)

	var customer, rental *db.Table
	for i := range s.Tables {
		switch s.Tables[i].Name {
		case "customer":
			customer = &s.Tables[i]
		case "rental":
			rental = &s.Tables[i]
		case "ONLY":
			t.Fatalf("phantom table named %q found in parsed schema — ALTER TABLE ONLY's literal \"ONLY\" token was mis-captured as a table name: %+v", s.Tables[i].Name, s.Tables[i])
		}
	}
	if customer == nil {
		t.Fatal("test setup broken: customer table not found in parsed schema")
	}
	if rental == nil {
		t.Fatal("test setup broken: rental table not found in parsed schema")
	}
	if len(customer.PrimaryKey) != 1 || customer.PrimaryKey[0] != "customer_id" {
		t.Errorf("customer.PrimaryKey = %v, want [customer_id] (real ALTER TABLE ONLY customer ADD CONSTRAINT customer_pkey PRIMARY KEY (customer_id))", customer.PrimaryKey)
	}
	if len(rental.ForeignKeys) != 1 {
		t.Fatalf("rental.ForeignKeys = %d, want 1 (real ALTER TABLE ONLY rental ADD CONSTRAINT rental_customer_fk FOREIGN KEY (customer_id) REFERENCES customer(customer_id))", len(rental.ForeignKeys))
	} else {
		fk := rental.ForeignKeys[0]
		if len(fk.Columns) != 1 || fk.Columns[0] != "customer_id" || fk.RefTable != "customer" {
			t.Errorf("rental.ForeignKeys[0] = %+v, want Columns=[customer_id] RefTable=customer", fk)
		}
	}
}

// Rule-level: DB-050 must NOT false-fire on customer (it genuinely has a
// primary key via the real ALTER TABLE ONLY statement) and must NOT fabricate
// a finding against a phantom "ONLY" table.
func TestAlterTableOnly_Inline_DB050NoFalseFire(t *testing.T) {
	s := alterTableOnlyHotfixSchema(t)
	fs, _ := dbrules.Run(s)

	for _, f := range fs {
		if f.ID != "DB-050" {
			continue
		}
		if f.Description == "Table customer has no primary key." {
			t.Errorf("DB-050 false-fired on customer: %+v — customer.PrimaryKey should be [customer_id] via the real ALTER TABLE ONLY statement", f)
		}
		if f.Description == "Table ONLY has no primary key." {
			t.Errorf("DB-050 fired a phantom finding against a table named %q: %+v — this table must not exist in the parsed schema at all", "ONLY", f)
		}
	}
}

// Rule-level: DB-001 must evaluate rental's real foreign key to customer
// (present in the schema, not silently dropped) — since no index covers
// rental.customer_id in this minimal schema, DB-001 must surface it as a
// question for the agent to reason about.
func TestAlterTableOnly_Inline_DB001EvaluatesFK(t *testing.T) {
	s := alterTableOnlyHotfixSchema(t)

	var rental *db.Table
	for i := range s.Tables {
		if s.Tables[i].Name == "rental" {
			rental = &s.Tables[i]
		}
	}
	if rental == nil {
		t.Fatal("test setup broken: rental table not found in parsed schema")
	}
	if len(rental.ForeignKeys) == 0 {
		t.Fatal("rental.ForeignKeys is empty — DB-001 has nothing to evaluate; the real FK to customer was dropped by the ALTER TABLE ONLY bug")
	}

	_, surf := dbrules.Run(s)
	var forRental []string
	for _, item := range surf {
		if item.Category != string(surface.CategoryDBFKNoIndex) {
			continue
		}
		if item.File == rental.ForeignKeys[0].Pos.File && item.Line == rental.ForeignKeys[0].Pos.Line {
			forRental = append(forRental, item.ReasonToReview)
		}
	}
	if len(forRental) != 1 {
		t.Errorf("DB-001 surfaced %d item(s) for rental's FK to customer, want exactly 1 (uncovered FK on customer_id must be reasoned about, not silently dropped): %+v", len(forRental), forRental)
	}
}
