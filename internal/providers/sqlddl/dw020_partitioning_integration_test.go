package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DW-020 measured against the REAL sqlddl parser and the REAL S1/ADR-0037
// paradigm classifier — never a hand-built db.Schema/db.Table literal. That
// matters more here than for any other DW rule: DW-020's two hardest cases
// (a PARTITION-OF child, and a partitioned parent) are states the PARSER
// produces and a hand-built fixture would have to IMITATE. The imitation is
// exactly where this project has repeatedly locked a reality that does not
// exist — a fixture holding values the production path never sets.
//
// Every fixture below therefore states real DDL and asserts, before the rule
// runs, that the parser and the classifier actually produced the shape the
// test claims to exercise (roles, StructureProven, Partitioning.Of).
//
// The keys are spelled _sk deliberately, for the same schema-gate reason
// dw021_integration_test.go documents: ADR 0037 withholds every warehouse
// role from a schema showing no schema-wide warehouse evidence, and
// surrogate_key_names is the cheapest honest way for constructed DDL to say
// "this is a warehouse".

// dw020Star builds a two-dimension PostgreSQL star. factStmts is spliced in
// verbatim, so each test states exactly the fact-side DDL it is about.
func dw020Star(factStmts string) string {
	return `
CREATE TABLE dim_customer (customer_sk integer PRIMARY KEY);

CREATE TABLE dim_product (product_sk integer PRIMARY KEY);

` + factStmts + `
`
}

// dw020Facts parses src under the default (PostgreSQL) dialect and returns
// the parsed schema plus the REAL classification.
func dw020Parse(t *testing.T, src string) (*db.Schema, paradigm.Classification) {
	t.Helper()
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "schema.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s, paradigm.Detect(s)
}

// dw020Items returns every DW-020 surface item the production rule set emits
// over src, after asserting DW-020 stayed SURFACE-only (ADR 0017).
func dw020Items(t *testing.T, s *db.Schema, c *paradigm.Classification) []findings.SurfaceItem {
	t.Helper()
	fs, surf := dwrules.Run(s, c)
	if len(fs) != 0 {
		t.Fatalf("DW-020 must be SURFACE-only (ADR 0017), got findings: %v", fs)
	}
	var out []findings.SurfaceItem
	for _, it := range surf {
		if it.Category == string(surface.CategoryDWFactsNotPartitioned) {
			out = append(out, it)
		}
	}
	return out
}

// signalValue returns the value of the "<key>: " signal on it, or "" when the
// item carries no such signal.
func signalValue(it findings.SurfaceItem, key string) string {
	for _, sig := range it.StructuralSignals {
		if strings.HasPrefix(sig, key+": ") {
			return strings.TrimPrefix(sig, key+": ")
		}
	}
	return ""
}

// requireRole fails unless the REAL classifier gave name the expected role —
// the guard that keeps every fixture below from passing vacuously.
func requireRole(t *testing.T, c paradigm.Classification, name string, want paradigm.Role) {
	t.Helper()
	if got := c.Roles[name]; got != want {
		t.Fatalf("role[%s] = %q, want %q — the fixture does not have the shape this test claims to exercise", name, got, want)
	}
}

// unpartitionedFacts is the plain two-fact star with NO partitioning
// anywhere: the uniform-absence case, which is what the 26-corpus survey
// measured as the overwhelmingly common state of a real warehouse.
const unpartitionedFacts = `
CREATE TABLE fact_sales (
    sale_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    sale_date date NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_sales ADD CONSTRAINT fs_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales ADD CONSTRAINT fs_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);

CREATE TABLE fact_returns (
    return_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_returns ADD CONSTRAINT fr_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_returns ADD CONSTRAINT fr_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);
`

// TestDW020_RealParser_NoFactPartitioning_EmitsExactlyOneCensusItem is the
// primary positive: TWO unpartitioned fact tables produce exactly ONE item,
// not one per table. That count is the whole architectural point of the rule
// (a per-table rule would fire on 100% of fact tables in 100% of warehouses),
// so it is asserted as an equality, never as "at least one".
func TestDW020_RealParser_NoFactPartitioning_EmitsExactlyOneCensusItem(t *testing.T) {
	s, c := dw020Parse(t, dw020Star(unpartitionedFacts))
	requireRole(t, c, "fact_sales", paradigm.RoleFact)
	requireRole(t, c, "fact_returns", paradigm.RoleFact)

	items := dw020Items(t, s, &c)
	if len(items) != 1 {
		t.Fatalf("DW-020 items = %d, want exactly 1 — DW-020 is a SCHEMA-LEVEL census, never one item per fact table", len(items))
	}
	it := items[0]
	if got := signalValue(it, "unpartitioned_fact_tables"); got != "fact_sales, fact_returns" {
		t.Errorf("unpartitioned_fact_tables = %q, want %q", got, "fact_sales, fact_returns")
	}
	if got := signalValue(it, "partitioned_fact_tables"); got != "(none)" {
		t.Errorf("partitioned_fact_tables = %q, want %q", got, "(none)")
	}
	if it.StructuralFacts["some_fact_tables_partitioned"] {
		t.Error("some_fact_tables_partitioned = true over a schema where none is partitioned")
	}
	if it.StructuralFacts["partition_children_declared"] {
		t.Error("partition_children_declared = true over a schema that declares no PARTITION OF child")
	}
	if it.File != "schema.sql" {
		t.Errorf("item File = %q, want schema.sql", it.File)
	}
}

// mixedPartitioningFacts is the rule's hardest real case, and it is REAL DDL,
// not a contrivance:
//
//   - fact_sales is PARTITION BY RANGE and declares its own foreign keys
//     (PostgreSQL 11+ allows FKs on a partitioned parent);
//   - fact_sales_2024_01 is a genuine PARTITION OF child that ALSO declares
//     its own foreign keys — which PostgreSQL 10 and earlier REQUIRED, since
//     a partitioned parent could not carry them;
//   - fact_returns is an ordinary, unpartitioned fact table.
//
// Measured through the real parser and the real classifier, that child comes
// out FACT-ROLE (its two FKs corroborate the fact_ name) and UNPROVEN
// (db.ReasonPartitionChildInheritsStructure). It is therefore not a
// hypothetical hazard, in either direction: without the census's child
// exclusion it is counted as a fact table of its own (measured by mutation:
// it joins partitioned_fact_tables, since its own PARTITION OF clause is a
// non-empty Declaration — one partitioned fact reported as two), and without
// the completeness gate's child exemption its by-construction unprovenness
// abstains the entire rule on the very schema that DOES partition. Both are
// asserted below.
const mixedPartitioningFacts = `
CREATE TABLE fact_sales (
    sale_sk integer NOT NULL,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    sale_date date NOT NULL,
    amount numeric(12,2)
) PARTITION BY RANGE (sale_date);
ALTER TABLE fact_sales ADD CONSTRAINT fs_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales ADD CONSTRAINT fs_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);

CREATE TABLE fact_sales_2024_01 PARTITION OF fact_sales FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
ALTER TABLE fact_sales_2024_01 ADD CONSTRAINT p1c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales_2024_01 ADD CONSTRAINT p1p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);

CREATE TABLE fact_returns (
    return_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_returns ADD CONSTRAINT fr_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_returns ADD CONSTRAINT fr_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);
`

// TestDW020_RealParser_PartitionChildren_ExcludedFromCensusAndFromTheGate is
// the load-bearing test of this slice. It locks THREE decisions at once, and
// each has a distinct mutation that turns it red:
//
//  1. A fact-role PARTITION OF child is EXCLUDED from the census. Mutation:
//     drop the Partitioning.Of guard from the census loop → the child joins
//     unpartitioned_fact_tables and the asserted list grows a third name.
//  2. A fact-role PARTITION OF child is EXEMPT from the completeness gate.
//     Mutation: gate every fact-role table including children → the child is
//     unproven by construction, so the whole rule abstains and the item
//     disappears entirely.
//  3. The rule FIRES when SOME fact tables are partitioned and others are
//     not. Mutation: suppress the item when any fact table is partitioned →
//     no item at all.
func TestDW020_RealParser_PartitionChildren_ExcludedFromCensusAndFromTheGate(t *testing.T) {
	s, c := dw020Parse(t, dw020Star(mixedPartitioningFacts))
	requireRole(t, c, "fact_sales", paradigm.RoleFact)
	requireRole(t, c, "fact_returns", paradigm.RoleFact)
	requireRole(t, c, "fact_sales_2024_01", paradigm.RoleFact)

	child := tableNamed(t, s, "fact_sales_2024_01")
	if child.Partitioning.Of != "fact_sales" {
		t.Fatalf("fact_sales_2024_01.Partitioning.Of = %q, want fact_sales — the parser did not produce the "+
			"partition CHILD this test claims to exercise", child.Partitioning.Of)
	}
	if child.StructureProven() {
		t.Fatal("fact_sales_2024_01.StructureProven() = true, want false — a PARTITION OF child inherits its " +
			"structure from the parent and must be unproven; without that, this test cannot exercise the gate exemption")
	}
	parent := tableNamed(t, s, "fact_sales")
	if parent.Partitioning.Declaration == "" {
		t.Fatal("fact_sales declares no partitioning — the parser did not read the PARTITION BY tail this test needs")
	}

	items := dw020Items(t, s, &c)
	if len(items) != 1 {
		t.Fatalf("DW-020 items = %d, want exactly 1 — a fact-role partition CHILD is unproven by construction and "+
			"must not abstain the rule over the schema that actually partitions", len(items))
	}
	it := items[0]
	if got := signalValue(it, "unpartitioned_fact_tables"); got != "fact_returns" {
		t.Errorf("unpartitioned_fact_tables = %q, want %q — a PARTITION OF child must never be censused as an "+
			"unpartitioned fact table (it IS a partition)", got, "fact_returns")
	}
	if got := signalValue(it, "partitioned_fact_tables"); got != "fact_sales" {
		t.Errorf("partitioned_fact_tables = %q, want %q", got, "fact_sales")
	}
	if !it.StructuralFacts["some_fact_tables_partitioned"] {
		t.Error("some_fact_tables_partitioned = false over a schema where fact_sales IS partitioned — the " +
			"inconsistency is the most interesting thing in this item and must reach the agent")
	}
	if !it.StructuralFacts["partition_children_declared"] {
		t.Error("partition_children_declared = false over a schema that declares a PARTITION OF child")
	}
}

// allFactsPartitionedFacts partitions BOTH fact tables — the negative
// control. Nothing is left to ask, so nothing is emitted.
const allFactsPartitionedFacts = `
CREATE TABLE fact_sales (
    sale_sk integer NOT NULL,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    sale_date date NOT NULL,
    amount numeric(12,2)
) PARTITION BY RANGE (sale_date);
ALTER TABLE fact_sales ADD CONSTRAINT fs_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales ADD CONSTRAINT fs_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);

CREATE TABLE fact_returns (
    return_sk integer NOT NULL,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    return_date date NOT NULL,
    amount numeric(12,2)
) PARTITION BY RANGE (return_date);
ALTER TABLE fact_returns ADD CONSTRAINT fr_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_returns ADD CONSTRAINT fr_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);
`

// TestDW020_RealParser_EveryFactPartitioned_DoesNotFire is the negative
// control the yield measurement depends on: DW-020 must be capable of NOT
// firing. A rule that fires on every schema regardless of what the DDL says
// measures nothing, and its zero-yield corpus runs would prove nothing
// either.
func TestDW020_RealParser_EveryFactPartitioned_DoesNotFire(t *testing.T) {
	s, c := dw020Parse(t, dw020Star(allFactsPartitionedFacts))
	requireRole(t, c, "fact_sales", paradigm.RoleFact)
	requireRole(t, c, "fact_returns", paradigm.RoleFact)
	for _, name := range []string{"fact_sales", "fact_returns"} {
		if tableNamed(t, s, name).Partitioning.Declaration == "" {
			t.Fatalf("%s declares no partitioning — the parser did not produce the shape this test claims to exercise", name)
		}
	}

	if items := dw020Items(t, s, &c); len(items) != 0 {
		t.Errorf("DW-020 items = %d, want 0 — every fact table declares partitioning, so there is nothing to ask: %+v",
			len(items), items)
	}
}

// unprovenFactFacts leaves fact_returns unpartitioned AND unproven, through a
// genuinely unrecognized real DDL shape (PostgreSQL's `CREATE INDEX ... ON
// ONLY`, the same one dw021_integration_test.go uses). fact_sales is proven
// and unpartitioned, so a per-table shrink would still emit an item naming
// only fact_sales.
const unprovenFactFacts = `
CREATE TABLE fact_sales (
    sale_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_sales ADD CONSTRAINT fs_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales ADD CONSTRAINT fs_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);

CREATE TABLE fact_returns (
    return_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_returns ADD CONSTRAINT fr_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_returns ADD CONSTRAINT fr_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);
CREATE INDEX idx_fact_returns_amount ON ONLY fact_returns (amount);
`

// TestDW020_RealParser_UnprovenCensusMember_AbstainsTheWholeRule locks the
// schema-level abstention DW-005/DW-011 established and ADR 0034 §2.5
// mandates for a census judgment: a shrunken census still emits, and an item
// that looks authoritative over an undercounted schema is a worse lie than
// silence.
//
// Discriminating, and here is why it cannot pass vacuously: fact_sales is
// proven, unpartitioned and fact-role, so it WOULD fire on its own (that is
// exactly TestDW020_RealParser_NoFactPartitioning_EmitsExactlyOneCensusItem).
// A per-table `continue` therefore emits one item naming fact_sales, and
// removing the gate outright emits one item naming both — both are non-zero
// and both fail this assertion.
func TestDW020_RealParser_UnprovenCensusMember_AbstainsTheWholeRule(t *testing.T) {
	s, c := dw020Parse(t, dw020Star(unprovenFactFacts))
	requireRole(t, c, "fact_sales", paradigm.RoleFact)
	requireRole(t, c, "fact_returns", paradigm.RoleFact)
	if tableNamed(t, s, "fact_sales").StructureProven() != true {
		t.Fatal("fact_sales must be PROVEN for this test to discriminate — it is the table a per-table shrink would emit")
	}
	if tableNamed(t, s, "fact_returns").StructureProven() {
		t.Fatal("fact_returns.StructureProven() = true, want false — `ON ONLY` is the genuinely unrecognized " +
			"CREATE INDEX shape this test needs; without it the census has nothing to abstain over")
	}

	if items := dw020Items(t, s, &c); len(items) != 0 {
		t.Errorf("DW-020 items = %d, want 0 — a census judgment abstains as a WHOLE when any census member is "+
			"unproven; a shrunken census that still emits is the worse lie (ADR 0034 §2.5): %+v", len(items), items)
	}
}

// closedGateDDL names its tables fact_/dim_ but shows NONE of the three
// deciding warehouse signals (ADR 0037): the keys are spelled _id rather than
// _sk, there is no calendar table, and the audit-timestamp columns flatten the
// type profile. The gate therefore closes and every warehouse role is
// withheld.
const closedGateDDL = `
CREATE TABLE dim_customer (customer_id integer PRIMARY KEY, name text, created_at timestamp, updated_at timestamp);

CREATE TABLE dim_product (product_id integer PRIMARY KEY, label text, created_at timestamp, updated_at timestamp);

CREATE TABLE fact_sales (
    sale_id integer PRIMARY KEY,
    customer_id integer NOT NULL,
    product_id integer NOT NULL,
    note text,
    created_at timestamp,
    updated_at timestamp
);
ALTER TABLE fact_sales ADD CONSTRAINT fs_c FOREIGN KEY (customer_id) REFERENCES dim_customer(customer_id);
ALTER TABLE fact_sales ADD CONSTRAINT fs_p FOREIGN KEY (product_id) REFERENCES dim_product(product_id);
`

// TestDW020_RealParser_ClosedSchemaGate_DoesNotFire locks the ADR 0037
// consequence deliberately: with the gate CLOSED, no table holds a warehouse
// role, so DW-020's census is empty and the rule says nothing. Partitioning
// is a WAREHOUSE question; asking it of a schema codefit did not judge to be
// a warehouse would re-open the exact hole the schema gate closed.
//
// Discriminating: fact_sales carries a recognized fact name, two corroborating
// foreign keys, proven structure and no partitioning — everything except an
// open gate. A rule that read names instead of cls.Roles, or that skipped the
// role filter, emits an item here.
func TestDW020_RealParser_ClosedSchemaGate_DoesNotFire(t *testing.T) {
	s, c := dw020Parse(t, closedGateDDL)
	if c.Gate.Open {
		t.Fatalf("schema gate is OPEN (deciding=%v) — this fixture must close it or the test proves nothing", c.Gate.Deciding)
	}
	if c.Gate.Withheld["fact_sales"] != paradigm.RoleFact {
		t.Fatalf("gate withheld %v from fact_sales, want the fact role — without a withheld fact role this "+
			"fixture does not exercise the gate at all", c.Gate.Withheld["fact_sales"])
	}
	requireRole(t, c, "fact_sales", paradigm.RoleUnclassified)

	if items := dw020Items(t, s, &c); len(items) != 0 {
		t.Errorf("DW-020 items = %d, want 0 — a closed schema gate means no warehouse role exists, so there is "+
			"no fact-table census to take: %+v", len(items), items)
	}
}
