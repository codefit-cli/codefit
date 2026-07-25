package dwrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

func run005(s *db.Schema, c *paradigm.Classification) []findings.SurfaceItem {
	fs, surf := dw005{}.Check(s, c)
	if len(fs) != 0 {
		panic("DW-005 must be SURFACE-only (ADR 0017), never an affirmation")
	}
	return surf
}

// starWith builds a fact + the given dimensions, classified accordingly.
func starWith(fact db.Table, dims ...db.Table) (*db.Schema, *paradigm.Classification) {
	roles := map[string]paradigm.Role{fact.Name: paradigm.RoleFact}
	tables := []db.Table{fact}
	for _, d := range dims {
		tables = append(tables, d)
		roles[d.Name] = paradigm.RoleDimension
	}
	return &db.Schema{Tables: tables}, cls(paradigm.ParadigmOLAP, roles)
}

// A warehouse whose facts cannot be sliced by time is barely a warehouse:
// almost every analytic question is "per day / per month / year over year".
func TestDW005_FactsWithoutTimeDimension_Fires(t *testing.T) {
	s, c := starWith(
		dimTable("fact_sales", []string{"id"}, dwcol("id", db.TypeInt)),
		dimTable("dim_customer", []string{"customer_key"}, dwcol("customer_key", db.TypeInt)),
		dimTable("dim_product", []string{"product_key"}, dwcol("product_key", db.TypeInt)),
	)

	got := itemsOfCategory(run005(s, c), surface.CategoryDWNoTimeDimension)
	if len(got) != 1 {
		t.Fatalf("DW-005 items = %d, want exactly 1 (a schema-level absence, not one per fact)", len(got))
	}
	if got[0].StructuralFacts["time_dimension_detected"] {
		t.Error("time_dimension_detected must be false")
	}
	if v := signalValue(got[0], "fact_tables"); v != "fact_sales" {
		t.Errorf("fact_tables signal = %q, want %q", v, "fact_sales")
	}
	if v := signalValue(got[0], "dimensions"); v != "dim_customer, dim_product" {
		t.Errorf("dimensions signal = %q, want both dimensions listed", v)
	}
}

// TRAP 1 — the conventional name. dim_date is the Kimball convention; seeing
// it must suppress the rule outright.
func TestDW005_DimDateByName_DoesNotFire(t *testing.T) {
	s, c := starWith(
		dimTable("fact_sales", []string{"id"}, dwcol("id", db.TypeInt)),
		dimTable("dim_date", []string{"date_key"}, dwcol("date_key", db.TypeInt)),
	)

	if got := itemsOfCategory(run005(s, c), surface.CategoryDWNoTimeDimension); len(got) != 0 {
		t.Errorf("DW-005 items = %d, want 0 (dim_date is the time dimension)", len(got))
	}
}

// dim_time and dim_calendar are the other two conventional spellings.
func TestDW005_AlternativeTimeDimensionNames_DoNotFire(t *testing.T) {
	for _, name := range []string{"dim_time", "dim_calendar"} {
		t.Run(name, func(t *testing.T) {
			s, c := starWith(
				dimTable("fact_sales", []string{"id"}, dwcol("id", db.TypeInt)),
				dimTable(name, []string{"k"}, dwcol("k", db.TypeInt)),
			)
			if got := itemsOfCategory(run005(s, c), surface.CategoryDWNoTimeDimension); len(got) != 0 {
				t.Errorf("DW-005 items = %d, want 0 (%s is a time dimension by name)", len(got), name)
			}
		})
	}
}

// TRAP 2 — the STRUCTURAL signal, with no conventional name at all. A
// dimension keyed by a single date/datetime column IS date-grained: one row
// per point in time. Name-only detection would miss it and produce a false
// positive on a perfectly good warehouse.
func TestDW005_DateGrainedDimensionWithoutTheName_DoesNotFire(t *testing.T) {
	s, c := starWith(
		dimTable("fact_sales", []string{"id"}, dwcol("id", db.TypeInt)),
		dimTable("dim_reporting_period", []string{"period_start"},
			dwcol("period_start", db.TypeDateTime),
			dwcol("label", db.TypeString),
		),
	)

	if got := itemsOfCategory(run005(s, c), surface.CategoryDWNoTimeDimension); len(got) != 0 {
		t.Errorf("DW-005 items = %d, want 0 (a dimension keyed by a date column is date-grained)", len(got))
	}
}

// A dimension that merely CONTAINS a datetime column (an audit timestamp) is
// not a time dimension — the key has to be the date. Without this boundary the
// structural signal would suppress the rule on nearly every schema.
func TestDW005_DimensionWithAnIncidentalDateColumn_StillFires(t *testing.T) {
	s, c := starWith(
		dimTable("fact_sales", []string{"id"}, dwcol("id", db.TypeInt)),
		dimTable("dim_customer", []string{"customer_key"},
			dwcol("customer_key", db.TypeInt),
			dwcol("updated_at", db.TypeDateTime),
		),
	)

	if got := itemsOfCategory(run005(s, c), surface.CategoryDWNoTimeDimension); len(got) != 1 {
		t.Errorf("DW-005 items = %d, want 1 (an audit timestamp is not a time dimension)", len(got))
	}
}

// EDGE: no fact-role table means there is no warehouse to judge. The rule must
// not fire on a schema that merely happens to contain a dimension-shaped table.
func TestDW005_NoFactTables_EmitsNothing(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		dimTable("dim_customer", []string{"customer_key"}, dwcol("customer_key", db.TypeInt)),
		dimTable("users", []string{"id"}, dwcol("id", db.TypeInt)),
	}}
	c := cls(paradigm.ParadigmMixed, map[string]paradigm.Role{
		"dim_customer": paradigm.RoleDimension,
		"users":        paradigm.RoleUnclassified,
	})

	if got := run005(s, c); len(got) != 0 {
		t.Errorf("DW-005 items = %d, want 0 (no fact table — nothing to slice by time)", len(got))
	}
}

// EDGE: facts with NO dimensions at all still fire once, and say so plainly
// rather than emitting an empty dimension list.
func TestDW005_FactsWithNoDimensionsAtAll_FiresOnce(t *testing.T) {
	s, c := starWith(dimTable("fact_events", []string{"id"}, dwcol("id", db.TypeInt)))

	got := itemsOfCategory(run005(s, c), surface.CategoryDWNoTimeDimension)
	if len(got) != 1 {
		t.Fatalf("DW-005 items = %d, want 1", len(got))
	}
	if v := signalValue(got[0], "dimensions"); v != "(none)" {
		t.Errorf("dimensions signal = %q, want %q", v, "(none)")
	}
}

// The single emitted item anchors on the FIRST fact table in schema order, so
// its baseline fingerprint is stable across runs.
func TestDW005_MultipleFacts_AnchorsOnTheFirst(t *testing.T) {
	first := dimTable("fact_sales", []string{"id"}, dwcol("id", db.TypeInt))
	first.Pos = db.Pos{File: "warehouse.sql", Line: 10}
	second := dimTable("fact_returns", []string{"id"}, dwcol("id", db.TypeInt))
	second.Pos = db.Pos{File: "warehouse.sql", Line: 90}

	s := &db.Schema{Tables: []db.Table{first, second}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{
		"fact_sales":   paradigm.RoleFact,
		"fact_returns": paradigm.RoleFact,
	})

	got := itemsOfCategory(run005(s, c), surface.CategoryDWNoTimeDimension)
	if len(got) != 1 {
		t.Fatalf("DW-005 items = %d, want 1 (schema-level, not per fact)", len(got))
	}
	if got[0].File != "warehouse.sql" || got[0].Line != 10 {
		t.Errorf("anchor = %s:%d, want warehouse.sql:10 (the first fact table)", got[0].File, got[0].Line)
	}
	if v := signalValue(got[0], "fact_tables"); v != "fact_sales, fact_returns" {
		t.Errorf("fact_tables signal = %q, want both facts listed in schema order", v)
	}
}

func TestDW005_ID(t *testing.T) {
	if got := (dw005{}).ID(); got != "DW-005" {
		t.Errorf("ID() = %q, want %q", got, "DW-005")
	}
}

func TestDW005_RegisteredInAll(t *testing.T) {
	if !registeredIn("DW-005") {
		t.Error("DW-005 is not registered in dwrules.All() — the sensor would never run it")
	}
}
