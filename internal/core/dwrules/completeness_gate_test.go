package dwrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// D4 (design SS4) — all seven DW rules (DW-001/002/005/010/011/020/021)
// ABSTAIN, per table, on an unproven table; DW-005, DW-011 and DW-020 are
// SCHEMA-LEVEL census judgments and therefore abstain the WHOLE rule when ANY
// relevant table is unproven — a per-table continue would silently shrink the
// census and still emit, a worse lie.
//
// DW-020's gate is NOT locked here, and that is deliberate rather than an
// omission. Its census excludes declared PARTITION CHILDREN, and a child is
// unproven BY CONSTRUCTION (db.ReasonPartitionChildInheritsStructure) — a
// state only the real parser produces, from real DDL. A hand-built db.Table
// carrying that reason would be the exact "fixture holding values the
// production path never sets" this project names as its most recurring test
// defect. Both halves of DW-020's gate (whole-rule abstention, and the child
// exemption that keeps it from abstaining on every partitioned warehouse) are
// locked through the real parser in
// internal/providers/sqlddl/dw020_partitioning_integration_test.go.

func unprovenDWTable(name string) db.Table {
	tb := db.Table{Name: name, Pos: db.Pos{File: "x.sql", Line: 1}}
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE "+name+" ...;", db.Pos{File: "x.sql", Line: 2})
	return tb
}

func TestDW001_UnprovenFactTable_Abstains(t *testing.T) {
	tb := unprovenDWTable("fact_sales")
	s := &db.Schema{Tables: []db.Table{tb}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"fact_sales": paradigm.RoleFact}}
	_, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-001")})
	if len(surf) != 0 {
		t.Errorf("DW-001 must abstain on an unproven fact table, got: %+v", surf)
	}
}

func TestDW002_UnprovenDimensionTable_Abstains(t *testing.T) {
	tb := unprovenDWTable("dim_customer")
	tb.PrimaryKey = []string{"customer_key"} // has a PK, so it would fire on the surrogate test if proven
	s := &db.Schema{Tables: []db.Table{tb}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"dim_customer": paradigm.RoleDimension}}
	_, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-002")})
	if len(surf) != 0 {
		t.Errorf("DW-002 must abstain on an unproven dimension table (guard BEFORE the PK==0 check), got: %+v", surf)
	}
}

// TestDW005_AnyUnprovenFactOrDimension_AbstainsWholeRule is a DISCRIMINATING
// fixture (sdd-verify C1, obs #1279): the original version used a proven
// dim_date, which short-circuits identically under EVERY implementation
// (timeDim != "" is decided before completeness is even consulted) — the
// test passed against the shipped code, a per-table shrink, AND the gate
// removed entirely, so it proved nothing.
//
// dim_period is deliberately UNPROVEN, non-time-NAMED (does not match
// dim_date/dim_time/dim_calendar) and carries NO primary key / columns — this
// models the real target scenario: the ALTER TABLE that would have declared
// its date-grained key was dropped, so as CURRENTLY parsed it looks like
// nothing at all. Whether the real schema's dim_period genuinely is a time
// dimension is exactly what codefit cannot tell — which is why the whole
// rule must abstain, not guess from a hole in the model.
//
//   - Shipped code: dim_period is Dimension-role and !StructureProven() ->
//     the pre-scan returns nil,nil before any census is built. No item.
//   - Per-table-shrink mutation (the natural "continue instead of abstain"
//     rewrite): dim_period is skipped from the census entirely -> dims=(none),
//     timeDim="" -> WRONGLY fires "no time dimension" over a schema where a
//     table that dim_period is not disproven from ever being reported.
//   - Gate-removed-entirely mutation: dim_period is evaluated at face value
//     (StructureProven() forced true) -> included in dims, but its empty
//     PK/columns don't match isTimeDimension by name or grain -> timeDim
//     stays "" -> WRONGLY fires the same false item, just naming dim_period.
//
// Both wrong outcomes are a `len(surf) != 0` this test catches; the correct
// outcome (abstain) is `len(surf) == 0`. Proven live by mutation before this
// test shipped — see the commit message for the exact FAIL/PASS transcript.
func TestDW005_AnyUnprovenFactOrDimension_AbstainsWholeRule(t *testing.T) {
	fact := db.Table{Name: "fact_sales", Complete: true, Pos: db.Pos{File: "x.sql", Line: 1}}
	dimPeriod := db.Table{Name: "dim_period", Pos: db.Pos{File: "x.sql", Line: 2}}
	dimPeriod.MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE dim_period ADD CONSTRAINT ...;", db.Pos{File: "x.sql", Line: 3})
	s := &db.Schema{Tables: []db.Table{fact, dimPeriod}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"fact_sales": paradigm.RoleFact, "dim_period": paradigm.RoleDimension}}
	fs, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-005")})
	if len(fs) != 0 || len(surf) != 0 {
		t.Errorf("DW-005 must abstain the WHOLE rule when any fact/dimension table is unproven, got findings=%v surface=%v", fs, surf)
	}
}

// TestDW021_UnprovenFactTable_Abstains proves the S3 thesis end to end: a
// per-table StructureProven() gate abstains automatically on an unproven fact
// table, with no dialect branch anywhere in dw021.go. This unproven table has
// NO indexes at all (which would otherwise fire deterministically per
// TestDW021_FactWithNoIndexesAtAll_Fires) — the dropped statement could have
// been the very CREATE INDEX declaring its columnar index.
func TestDW021_UnprovenFactTable_Abstains(t *testing.T) {
	tb := unprovenDWTable("fact_sales")
	s := &db.Schema{Tables: []db.Table{tb}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"fact_sales": paradigm.RoleFact}}
	_, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-021")})
	if len(surf) != 0 {
		t.Errorf("DW-021 must abstain on an unproven fact table, got: %+v", surf)
	}
}

func TestDW010_UnprovenSCD2Dimension_Abstains(t *testing.T) {
	tb := unprovenDWTable("dim_product")
	tb.Columns = []db.Column{{Name: "valid_to", Type: db.TypeDateTime}}
	s := &db.Schema{Tables: []db.Table{tb}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"dim_product": paradigm.RoleDimension}}
	_, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-010")})
	if len(surf) != 0 {
		t.Errorf("DW-010 must abstain on an unproven SCD-2 dimension, got: %+v", surf)
	}
}

// TestDW011_AnyUnprovenDimension_AbstainsWholeRule is a DISCRIMINATING
// fixture (sdd-verify C1, obs #1279): the original 2-dimension version left
// scd1 EMPTY after a per-table shrink (only the unproven table would have
// been scd1), so `len(scd1)==0` short-circuited identically under the
// shipped code AND the mutation — it never reached the branch it claimed to
// protect.
//
// This version carries THREE dimensions: dim_product (PROVEN SCD-2, has
// valid_to), dim_category (PROVEN SCD-1, no history columns), and
// dim_region (UNPROVEN, no history columns as parsed — the same "the
// declaring statement was dropped" scenario as DW-005's fixture above).
//
//   - Shipped code: dim_region is Dimension-role, non-time, and
//     !StructureProven() -> the pre-scan returns nil,nil. No item.
//   - Per-table-shrink mutation: dim_region is skipped from the census ->
//     scd2=[dim_product] (non-empty, from the ALREADY-proven dimension),
//     scd1=[dim_category] (non-empty, likewise) -> WRONGLY fires, having
//     never asked whether dim_region's true (unread) shape would have
//     changed the count.
//   - Gate-removed mutation: dim_region is evaluated at face value -> no
//     scd markers as parsed -> joins scd1 -> STILL fires (scd2 non-empty,
//     scd1 now [dim_category, dim_region]) — the same wrong "yes, mixed"
//     conclusion reached over an incomplete census, just with one more name
//     in it.
//
// Proven live by mutation before this test shipped — see the commit message.
func TestDW011_AnyUnprovenDimension_AbstainsWholeRule(t *testing.T) {
	scd2 := db.Table{Name: "dim_product", Complete: true, Pos: db.Pos{File: "x.sql", Line: 1},
		Columns: []db.Column{{Name: "valid_to", Type: db.TypeDateTime}}}
	scd1 := db.Table{Name: "dim_category", Complete: true, Pos: db.Pos{File: "x.sql", Line: 2}}
	dimRegion := db.Table{Name: "dim_region", Pos: db.Pos{File: "x.sql", Line: 3}}
	dimRegion.MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE dim_region ADD CONSTRAINT ...;", db.Pos{File: "x.sql", Line: 4})
	s := &db.Schema{Tables: []db.Table{scd2, scd1, dimRegion}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{
		"dim_product": paradigm.RoleDimension, "dim_category": paradigm.RoleDimension, "dim_region": paradigm.RoleDimension,
	}}
	fs, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-011")})
	if len(fs) != 0 || len(surf) != 0 {
		t.Errorf("DW-011 must abstain the WHOLE rule when any compared dimension is unproven, got findings=%v surface=%v", fs, surf)
	}
}

// Proven-model regression lock (4R repair, obs #1282 — the doc comment
// previously overclaimed "byte-identical output"; corrected to match what
// this test actually asserts): a fully proven schema must still fire DW-001
// on a fact table with no dimension FK — a PRESENCE check only (category
// membership), for one representative rule since the others already have
// their own per-rule unit tests. Full signal-content correctness for DW-001
// (table name, foreign_keys text, StructuralFacts) is separately covered by
// dw001_test.go's TestDW001_FactWithoutDimensionFK_Fires and siblings, which
// this test does not duplicate.
func TestDW001_ProvenFactTable_StillFires(t *testing.T) {
	tb := db.Table{Name: "fact_sales", Complete: true, Pos: db.Pos{File: "x.sql", Line: 1}}
	s := &db.Schema{Tables: []db.Table{tb}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"fact_sales": paradigm.RoleFact}}
	_, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-001")})
	var found bool
	for _, it := range surf {
		if it.Category == string(surface.CategoryDWNoFactDimensionFK) {
			found = true
		}
	}
	if !found {
		t.Error("DW-001 must still fire on a PROVEN fact table with no dimension FK")
	}
}

func dwRuleByID(t *testing.T, id string) dwrules.Rule {
	t.Helper()
	for _, r := range dwrules.All() {
		if r.ID() == id {
			return r
		}
	}
	t.Fatalf("no DW rule with ID %q in dwrules.All()", id)
	return nil
}
