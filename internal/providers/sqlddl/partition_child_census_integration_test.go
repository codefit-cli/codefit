package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// The partition-child census exemption, measured against the REAL sqlddl
// parser and the REAL ADR-0037 classifier — never a hand-built db.Table. That
// is not a stylistic preference here, it is the only way to reach the state
// under test at all: a declared PostgreSQL partition child is unproven BY
// CONSTRUCTION (db.ReasonPartitionChildInheritsStructure), a value only
// applyCreateTablePartitionOf ever sets. A hand-built table carrying it would
// be the "fixture holding values the production path never sets" defect this
// project names as its most recurring one.
//
// Every fixture below asserts, BEFORE the rule runs, that the parser and the
// classifier actually produced the shape the test claims to exercise (roles,
// StructureProven, Partitioning.Of). Without those guards a fixture that
// stopped producing a partition child would keep passing, vacuously.

// dwItemsOfCategory returns every surface item of one category the production
// DW rule set emits over s, after asserting the DW family stayed SURFACE-only
// (ADR 0017).
func dwItemsOfCategory(t *testing.T, s *db.Schema, c *paradigm.Classification, cat surface.Category) []findings.SurfaceItem {
	t.Helper()
	fs, surf := dwrules.Run(s, c)
	if len(fs) != 0 {
		t.Fatalf("the DW family must be SURFACE-only (ADR 0017), got findings: %v", fs)
	}
	var out []findings.SurfaceItem
	for _, it := range surf {
		if it.Category == string(cat) {
			out = append(out, it)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// DW-005 — the false negative ADR 0038 measured and left open
// ---------------------------------------------------------------------------

// TestDW005_RealParser_FactPartitionChild_DoesNotAbstainTheWholeRule is the
// regression this slice exists for. ADR 0038 §4 measured it and declared it
// open: over dw020Star(mixedPartitioningFacts), DW-005 emits
// dw-no-time-dimension WITHOUT the partition child and does NOT emit it WITH
// the child, because DW-005's gate covered every fact- and dimension-role
// table and a child is unproven by construction. The schemas that declare
// partition children are exactly the declaratively partitioned warehouses, so
// the rule went silent on a whole class of real schemas.
//
// Discriminating rather than vacuous: this same star with the child REMOVED
// fires (TestDW005_RealParser_SameStarWithoutTheChild_AlsoFires below is the
// paired control), and the emitted census must name the two REAL fact tables
// and NOT the child — an item naming fact_sales_2024_01 would be the census
// inflation the exclusion exists to prevent.
func TestDW005_RealParser_FactPartitionChild_DoesNotAbstainTheWholeRule(t *testing.T) {
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
		t.Fatal("fact_sales_2024_01.StructureProven() = true, want false — without a by-construction unproven " +
			"child this test cannot exercise the gate exemption at all")
	}

	items := dwItemsOfCategory(t, s, &c, surface.CategoryDWNoTimeDimension)
	if len(items) != 1 {
		t.Fatalf("DW-005 items = %d, want exactly 1 — a fact-role partition CHILD is unproven BY CONSTRUCTION "+
			"and must not abstain a rule it is not a census member of (this is the false negative ADR 0038 §4 "+
			"measured and left open)", len(items))
	}
	if got := signalValue(items[0], "fact_tables"); got != "fact_sales, fact_returns" {
		t.Errorf("fact_tables = %q, want %q — a PARTITION OF child is not an independent fact table and must "+
			"never be censused as one", got, "fact_sales, fact_returns")
	}
	if got := signalValue(items[0], "dimensions"); got != "dim_customer, dim_product" {
		t.Errorf("dimensions = %q, want %q", got, "dim_customer, dim_product")
	}
}

// TestDW005_RealParser_SameStarWithoutTheChild_AlsoFires is the paired
// control that makes the test above a measurement rather than an assertion:
// the SAME star, minus only the partition child, must produce the SAME census.
// If this one ever stops firing, the test above is no longer evidence that the
// child was what silenced DW-005.
func TestDW005_RealParser_SameStarWithoutTheChild_AlsoFires(t *testing.T) {
	s, c := dw020Parse(t, dw020Star(partitionedParentNoChildFacts))
	requireRole(t, c, "fact_sales", paradigm.RoleFact)
	requireRole(t, c, "fact_returns", paradigm.RoleFact)
	for _, tb := range s.Tables {
		if tb.Partitioning.Of != "" {
			t.Fatalf("%s is a partition child — this control fixture must declare NONE", tb.Name)
		}
	}

	items := dwItemsOfCategory(t, s, &c, surface.CategoryDWNoTimeDimension)
	if len(items) != 1 {
		t.Fatalf("DW-005 items = %d, want exactly 1 over the child-free control", len(items))
	}
	if got := signalValue(items[0], "fact_tables"); got != "fact_sales, fact_returns" {
		t.Errorf("fact_tables = %q, want %q", got, "fact_sales, fact_returns")
	}
}

// partitionedParentNoChildFacts is mixedPartitioningFacts with the PARTITION
// OF child (and its two ALTER TABLEs) removed, and NOTHING else changed.
const partitionedParentNoChildFacts = `
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
    return_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_returns ADD CONSTRAINT fr_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_returns ADD CONSTRAINT fr_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);
`

// TestDW005_RealParser_UnprovenNonChildCensusMember_StillAbstainsTheWholeRule
// proves the exemption opened no hole in ADR 0034 §2.5. A GENUINE parser
// failure on a census member — PostgreSQL's `CREATE INDEX ... ON ONLY`, a shape
// this reducer does not recognize — must still abstain DW-005 as a whole.
//
// Discriminating: fact_sales is proven, and the schema has no time dimension,
// so a per-table shrink (or a removed gate) emits an item here. Only the
// whole-rule abstention yields zero.
func TestDW005_RealParser_UnprovenNonChildCensusMember_StillAbstainsTheWholeRule(t *testing.T) {
	s, c := dw020Parse(t, dw020Star(unprovenFactFacts))
	requireRole(t, c, "fact_sales", paradigm.RoleFact)
	requireRole(t, c, "fact_returns", paradigm.RoleFact)
	broken := tableNamed(t, s, "fact_returns")
	if broken.StructureProven() {
		t.Fatal("fact_returns.StructureProven() = true, want false — `ON ONLY` is the genuinely unrecognized " +
			"CREATE INDEX shape this test needs")
	}
	if broken.Partitioning.Of != "" {
		t.Fatalf("fact_returns.Partitioning.Of = %q, want empty — this table must be unproven for a PARSER "+
			"reason, not by construction, or the test does not discriminate", broken.Partitioning.Of)
	}

	if items := dwItemsOfCategory(t, s, &c, surface.CategoryDWNoTimeDimension); len(items) != 0 {
		t.Errorf("DW-005 items = %d, want 0 — a census judgment still abstains as a WHOLE when a census member "+
			"is unproven for a PARSER reason (ADR 0034 §2.5): %+v", len(items), items)
	}
}

// ---------------------------------------------------------------------------
// DW-011 — the false negative ADR 0038 asserted did NOT exist
// ---------------------------------------------------------------------------

// scdStarBase is a warehouse with a real SCD-1/SCD-2 split among its
// dimensions (dim_customer keeps history, dim_product overwrites) and a
// calendar, so DW-005 is quiet and DW-011 is the rule under observation.
const scdStarBase = `
CREATE TABLE dim_date (date_sk integer PRIMARY KEY, day date);

CREATE TABLE dim_customer (
    customer_sk integer PRIMARY KEY,
    name text,
    valid_from date,
    valid_to date,
    is_current boolean
);
CREATE INDEX dc_cur ON dim_customer (is_current);

CREATE TABLE dim_product (product_sk integer PRIMARY KEY, label text);

CREATE TABLE fact_sales (
    sale_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    date_sk integer NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_sales ADD CONSTRAINT fs_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales ADD CONSTRAINT fs_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);
ALTER TABLE fact_sales ADD CONSTRAINT fs_d FOREIGN KEY (date_sk) REFERENCES dim_date(date_sk);
`

// scdStarWithDimensionChild partitions a DIMENSION and has the fact reference
// the CHILD rather than the parent. That is not a contrivance: before
// PostgreSQL 12 a foreign key could not target a partitioned parent at all, so
// referencing a specific partition is what the DDL of that era looks like. The
// fan-in lands on the child, which is what earns the child a DIMENSION role.
const scdStarWithDimensionChild = scdStarBase + `
CREATE TABLE dim_region (region_sk integer NOT NULL, country text) PARTITION BY LIST (country);

CREATE TABLE dim_region_us PARTITION OF dim_region FOR VALUES IN ('US');

CREATE TABLE fact_shipments (
    shipment_sk integer PRIMARY KEY,
    region_sk integer NOT NULL,
    customer_sk integer NOT NULL,
    weight numeric(12,2)
);
ALTER TABLE fact_shipments ADD CONSTRAINT fsh_r FOREIGN KEY (region_sk) REFERENCES dim_region_us(region_sk);
ALTER TABLE fact_shipments ADD CONSTRAINT fsh_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
`

// TestDW011_RealParser_DimensionPartitionChild_DoesNotAbstainTheWholeRule
// corrects ADR 0038 §4, which stated "DW-011's gate reads dimension-role tables
// only, so a fact-role child never reaches it" — true, and incomplete. A
// DIMENSION can be partitioned too, and a dimension-role child DOES reach
// DW-011's gate. Measured through the real parser: over this schema DW-011
// emitted dw-mixed-scd-strategies before the child was added and stopped
// emitting it after.
//
// It also locks the OTHER half, the one that would be a new false affirmation:
// the child must not be COUNTED. Its columns live on its parent, so as parsed
// it carries no SCD marker at all and would join the SCD-1 group — a phantom
// SCD-1 dimension that can flip DW-011 from silent to firing over a schema
// whose dimensions are uniformly SCD-2. Exempting the gate without excluding
// the census is exactly that bug.
func TestDW011_RealParser_DimensionPartitionChild_DoesNotAbstainTheWholeRule(t *testing.T) {
	s, c := dw020Parse(t, scdStarWithDimensionChild)
	requireRole(t, c, "dim_region_us", paradigm.RoleDimension)
	child := tableNamed(t, s, "dim_region_us")
	if child.Partitioning.Of != "dim_region" {
		t.Fatalf("dim_region_us.Partitioning.Of = %q, want dim_region — the parser did not produce the "+
			"DIMENSION-role partition child this test claims to exercise", child.Partitioning.Of)
	}
	if child.StructureProven() {
		t.Fatal("dim_region_us.StructureProven() = true, want false — without a by-construction unproven " +
			"dimension child this test cannot exercise DW-011's gate at all")
	}

	items := dwItemsOfCategory(t, s, &c, surface.CategoryDWMixedSCDStrategies)
	if len(items) != 1 {
		t.Fatalf("DW-011 items = %d, want exactly 1 — a dimension-role partition CHILD is unproven by "+
			"construction and must not abstain a rule it is not a census member of", len(items))
	}
	if got := signalValue(items[0], "scd2_dimensions"); got != "dim_customer" {
		t.Errorf("scd2_dimensions = %q, want %q", got, "dim_customer")
	}
	if got := signalValue(items[0], "scd1_dimensions"); got != "dim_product" {
		t.Errorf("scd1_dimensions = %q, want %q — a PARTITION OF child carries no columns of its own, so "+
			"counting it would fabricate an SCD-1 dimension out of a partition", got, "dim_product")
	}
}

// TestDW011_RealParser_SameWarehouseWithoutTheChild_AlsoFires is the paired
// control: the same warehouse with the fact referencing the partitioned PARENT
// (PostgreSQL 12+) instead of the child.
func TestDW011_RealParser_SameWarehouseWithoutTheChild_AlsoFires(t *testing.T) {
	s, c := dw020Parse(t, scdStarBase+`
CREATE TABLE dim_region (region_sk integer NOT NULL, country text) PARTITION BY LIST (country);

CREATE TABLE fact_shipments (
    shipment_sk integer PRIMARY KEY,
    region_sk integer NOT NULL,
    customer_sk integer NOT NULL,
    weight numeric(12,2)
);
ALTER TABLE fact_shipments ADD CONSTRAINT fsh_r FOREIGN KEY (region_sk) REFERENCES dim_region(region_sk);
ALTER TABLE fact_shipments ADD CONSTRAINT fsh_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
`)
	for _, tb := range s.Tables {
		if tb.Partitioning.Of != "" {
			t.Fatalf("%s is a partition child — this control fixture must declare NONE", tb.Name)
		}
	}
	if items := dwItemsOfCategory(t, s, &c, surface.CategoryDWMixedSCDStrategies); len(items) != 1 {
		t.Fatalf("DW-011 items = %d, want exactly 1 over the child-free control", len(items))
	}
}

// TestDW011_RealParser_UnprovenNonChildDimension_StillAbstainsTheWholeRule is
// DW-011's half of the "no hole opened" lock: a dimension unproven for a real
// PARSER reason still abstains the whole rule.
func TestDW011_RealParser_UnprovenNonChildDimension_StillAbstainsTheWholeRule(t *testing.T) {
	s, c := dw020Parse(t, scdStarBase+`
CREATE INDEX idx_dim_product_label ON ONLY dim_product (label);
`)
	requireRole(t, c, "dim_product", paradigm.RoleDimension)
	broken := tableNamed(t, s, "dim_product")
	if broken.StructureProven() {
		t.Fatal("dim_product.StructureProven() = true, want false — `ON ONLY` is the genuinely unrecognized " +
			"CREATE INDEX shape this test needs")
	}
	if broken.Partitioning.Of != "" {
		t.Fatalf("dim_product.Partitioning.Of = %q, want empty — it must be unproven for a PARSER reason, "+
			"not by construction", broken.Partitioning.Of)
	}

	if items := dwItemsOfCategory(t, s, &c, surface.CategoryDWMixedSCDStrategies); len(items) != 0 {
		t.Errorf("DW-011 items = %d, want 0 — a census judgment still abstains as a WHOLE when a compared "+
			"dimension is unproven for a PARSER reason (ADR 0034 §2.5): %+v", len(items), items)
	}
}

// ---------------------------------------------------------------------------
// The cost of the exemption, paid explicitly rather than discovered later
// ---------------------------------------------------------------------------

// partitionedCalendarDDL is the one schema shape where the gate exemption
// would COST accuracy instead of buying it: the schema's CALENDAR is itself
// partitioned and the fact references the child, so ADR 0033's fan-in
// corroboration demotes the parent dim_date to unclassified and DW-005 never
// sees it among the dimension-role tables. Before the exemption the child's
// by-construction unprovenness abstained the whole rule, masking that by
// accident; after it, DW-005 would emit "this schema has no time dimension"
// over DDL that plainly declares dim_date.
const partitionedCalendarDDL = `
CREATE TABLE dim_customer (customer_sk integer PRIMARY KEY, name text);

CREATE TABLE dim_date (date_sk integer NOT NULL, day date) PARTITION BY RANGE (day);

CREATE TABLE dim_date_2024 PARTITION OF dim_date FOR VALUES FROM ('2024-01-01') TO ('2025-01-01');

CREATE TABLE fact_sales (
    sale_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    date_sk integer NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_sales ADD CONSTRAINT fs_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales ADD CONSTRAINT fs_d FOREIGN KEY (date_sk) REFERENCES dim_date_2024(date_sk);
`

// TestDW005_RealParser_PartitionedCalendar_ReadsTheParentName locks the
// mitigation that keeps the exemption from trading a measured false negative
// for a new false positive. A partition child RESTATES its parent, and the
// parent's NAME is in the model (db.Partitioning.Of) — it is the very same
// evidence DW-005's name signal already reads. So a child of a
// calendar-NAMED parent is enough for the schema to have a time dimension,
// even when the parent's own role was withheld.
//
// Discriminating: the fixture is asserted to reach exactly the demoted-parent
// state (dim_date unclassified, dim_date_2024 a dimension-role child), and
// dim_customer is a proven, fact-referenced dimension, so the rule has a real
// census and would emit here if the parent name were not consulted.
func TestDW005_RealParser_PartitionedCalendar_ReadsTheParentName(t *testing.T) {
	s, c := dw020Parse(t, partitionedCalendarDDL)
	requireRole(t, c, "dim_date", paradigm.RoleUnclassified)
	requireRole(t, c, "dim_date_2024", paradigm.RoleDimension)
	requireRole(t, c, "fact_sales", paradigm.RoleFact)
	child := tableNamed(t, s, "dim_date_2024")
	if child.Partitioning.Of != "dim_date" {
		t.Fatalf("dim_date_2024.Partitioning.Of = %q, want dim_date — the fixture does not have the shape "+
			"this test claims to exercise", child.Partitioning.Of)
	}

	if items := dwItemsOfCategory(t, s, &c, surface.CategoryDWNoTimeDimension); len(items) != 0 {
		t.Errorf("DW-005 items = %d, want 0 — dim_date_2024 is declared PARTITION OF dim_date, and dim_date "+
			"is a calendar by the same name vocabulary DW-005 already reads; claiming this schema has no time "+
			"dimension is a false affirmation the exemption must not introduce: %+v", len(items), items)
	}
}

// ---------------------------------------------------------------------------
// ADR 0038's declared limit, until now recorded only in prose
// ---------------------------------------------------------------------------

// fksOnChildrenOnlyDDL is the PostgreSQL <= 10 pattern: the partitioned PARENT
// carries no foreign keys of its own because that release could not put them
// there, so every FK lives on the children.
const fksOnChildrenOnlyDDL = `
CREATE TABLE dim_customer (customer_sk integer PRIMARY KEY);

CREATE TABLE dim_product (product_sk integer PRIMARY KEY);

CREATE TABLE fact_sales (
    sale_sk integer NOT NULL,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    sale_date date NOT NULL,
    amount numeric(12,2)
) PARTITION BY RANGE (sale_date);

CREATE TABLE fact_sales_2024_01 PARTITION OF fact_sales FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
ALTER TABLE fact_sales_2024_01 ADD CONSTRAINT p1c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales_2024_01 ADD CONSTRAINT p1p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);
`

// TestDW020_RealParser_PartitionedParentWithFKsOnChildrenOnly_EmitsNothing
// gives ADR 0038's third declared limit the executable lock it never had. The
// limit lived in prose in dw020.go and dbcoverage.go and was "verified by
// direct probe" — that is, by a run nobody can repeat. A declared limit
// without a test is an intention, not a control (ADR 0034 §2.7): if role
// classification ever starts corroborating a partitioned parent from its
// children's foreign keys, DW-020 silently changes behaviour on this exact
// shape and the two prose statements silently become false.
//
// The limit itself: the parent has FK fan-out 0, below paradigm's
// factFanOutMin, so ADR 0033's corroboration gate demotes it to unclassified.
// The only fact-role table left is the CHILD, which the census excludes. The
// census is therefore empty and DW-020 says nothing about a schema that
// visibly partitions its fact table.
//
// The fixture is asserted to be a WAREHOUSE with an OPEN schema gate first, so
// this cannot pass for ADR 0037's reason instead of the one it claims.
func TestDW020_RealParser_PartitionedParentWithFKsOnChildrenOnly_EmitsNothing(t *testing.T) {
	s, c := dw020Parse(t, fksOnChildrenOnlyDDL)
	if !c.Gate.Open {
		t.Fatalf("schema gate is CLOSED (deciding=%v) — this fixture must open it, or DW-020's silence would "+
			"be ADR 0037's doing and this test would prove nothing about the fan-out limit", c.Gate.Deciding)
	}
	requireRole(t, c, "fact_sales", paradigm.RoleUnclassified)
	requireRole(t, c, "fact_sales_2024_01", paradigm.RoleFact)

	parent := tableNamed(t, s, "fact_sales")
	if parent.Partitioning.Declaration == "" {
		t.Fatal("fact_sales declares no partitioning — the parser did not read the PARTITION BY tail")
	}
	if len(parent.ForeignKeys) != 0 {
		t.Fatalf("fact_sales declares %d foreign key(s), want 0 — the whole point of this fixture is a parent "+
			"with fan-out 0", len(parent.ForeignKeys))
	}
	if !parent.StructureProven() {
		t.Fatal("fact_sales.StructureProven() = false — the parent must be PROVEN, or DW-020's silence could " +
			"be the completeness gate rather than the missing fact role")
	}
	child := tableNamed(t, s, "fact_sales_2024_01")
	if len(child.ForeignKeys) != 2 {
		t.Fatalf("fact_sales_2024_01 declares %d foreign key(s), want 2 — the child is what carries the "+
			"fan-out in this pattern", len(child.ForeignKeys))
	}

	if items := dwItemsOfCategory(t, s, &c, surface.CategoryDWFactsNotPartitioned); len(items) != 0 {
		t.Errorf("DW-020 items = %d, want 0 — ADR 0038's declared limit: a partitioned parent whose foreign "+
			"keys live on its children has fan-out 0, loses its fact role to ADR 0033's corroboration gate, "+
			"and never enters the census: %+v", len(items), items)
	}
}
