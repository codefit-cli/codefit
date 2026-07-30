package dwrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

func run011(s *db.Schema, c *paradigm.Classification) []findings.SurfaceItem {
	fs, surf := dw011{}.Check(s, c)
	if len(fs) != 0 {
		panic("DW-011 must be SURFACE-only (ADR 0017), never an affirmation")
	}
	return surf
}

// scd1Dim is an overwrite-in-place dimension: a surrogate key and attributes,
// no history columns at all. DECLARED SYNTHETIC, same rationale as scd2Dim.
func scd1Dim(name string) db.Table {
	return dimTable(name, []string{"product_sk"},
		dwcol("product_sk", db.TypeInt),
		dwcol("product_code", db.TypeString),
		dwcol("category", db.TypeString),
	)
}

// dimsOnly builds a dimension-only schema with all tables classified as
// dimensions.
func dimsOnly(tables ...db.Table) (*db.Schema, *paradigm.Classification) {
	roles := map[string]paradigm.Role{}
	for _, t := range tables {
		roles[t.Name] = paradigm.RoleDimension
	}
	return &db.Schema{Tables: tables}, cls(paradigm.ParadigmOLAP, roles)
}

// The headline positive: one dimension versions its rows, another overwrites
// them. A report joining both silently mixes point-in-time and as-of-today
// attributes, and the numbers stop reconciling.
func TestDW011_MixedSCD1AndSCD2_FiresOnce(t *testing.T) {
	s, c := dimsOnly(scd2Dim("dim_customer"), scd1Dim("dim_product"))

	got := itemsOfCategory(run011(s, c), surface.CategoryDWMixedSCDStrategies)
	if len(got) != 1 {
		t.Fatalf("DW-011 items = %d, want exactly 1 (a schema-level inconsistency)", len(got))
	}
	if v := signalValue(got[0], "scd2_dimensions"); v != "dim_customer" {
		t.Errorf("scd2_dimensions signal = %q, want %q", v, "dim_customer")
	}
	if v := signalValue(got[0], "scd1_dimensions"); v != "dim_product" {
		t.Errorf("scd1_dimensions signal = %q, want %q", v, "dim_product")
	}
	if !got[0].StructuralFacts["mixed_scd_strategies"] {
		t.Error("mixed_scd_strategies must be true")
	}
}

// TRAP: a consistently SCD-2 warehouse is a DELIBERATE, correct choice and
// must be silent.
func TestDW011_AllDimensionsSCD2_DoesNotFire(t *testing.T) {
	s, c := dimsOnly(scd2Dim("dim_customer"), scd2Dim("dim_supplier"))

	if got := itemsOfCategory(run011(s, c), surface.CategoryDWMixedSCDStrategies); len(got) != 0 {
		t.Errorf("DW-011 items = %d, want 0 (consistently SCD-2)", len(got))
	}
}

// TRAP: so is a consistently SCD-1 warehouse.
func TestDW011_AllDimensionsSCD1_DoesNotFire(t *testing.T) {
	s, c := dimsOnly(scd1Dim("dim_product"), scd1Dim("dim_supplier"))

	if got := itemsOfCategory(run011(s, c), surface.CategoryDWMixedSCDStrategies); len(got) != 0 {
		t.Errorf("DW-011 items = %d, want 0 (consistently SCD-1)", len(got))
	}
}

// EDGE: with a single dimension there is no convention to be inconsistent
// with — nothing to compare.
func TestDW011_SingleDimension_DoesNotFire(t *testing.T) {
	s, c := dimsOnly(scd2Dim("dim_customer"))

	if got := run011(s, c); len(got) != 0 {
		t.Errorf("DW-011 items = %d, want 0 (one dimension cannot be inconsistent with itself)", len(got))
	}
}

// A TIME DIMENSION is excluded from the comparison. A calendar is not a
// slowly-changing dimension by definition — dates do not get new versions — so
// counting it as "SCD-1" would make this rule fire on essentially every
// correctly built warehouse, which is exactly the noise a name-shaped rule
// must avoid.
func TestDW011_TimeDimensionExcludedFromComparison(t *testing.T) {
	s, c := dimsOnly(
		scd2Dim("dim_customer"),
		dimTable("dim_date", []string{"date_key"}, dwcol("date_key", db.TypeInt)),
	)

	if got := itemsOfCategory(run011(s, c), surface.CategoryDWMixedSCDStrategies); len(got) != 0 {
		t.Errorf("DW-011 items = %d, want 0 (dim_date is not a slowly-changing dimension)", len(got))
	}
}

// ... but excluding the calendar must not hide a REAL inconsistency between
// two genuine dimensions that happen to share the schema with it.
func TestDW011_TimeDimensionPresentButRealMixRemains_Fires(t *testing.T) {
	s, c := dimsOnly(
		scd2Dim("dim_customer"),
		scd1Dim("dim_product"),
		dimTable("dim_date", []string{"date_key"}, dwcol("date_key", db.TypeInt)),
	)

	got := itemsOfCategory(run011(s, c), surface.CategoryDWMixedSCDStrategies)
	if len(got) != 1 {
		t.Fatalf("DW-011 items = %d, want 1 (dim_customer vs dim_product is a real mix)", len(got))
	}
	if v := signalValue(got[0], "scd1_dimensions"); v != "dim_product" {
		t.Errorf("scd1_dimensions signal = %q, want only dim_product (dim_date is excluded)", v)
	}
}

// Only DIMENSION-role tables take part: a fact table with history columns and
// an unclassified OLTP table are both ignored.
func TestDW011_NonDimensionRolesIgnored(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		scd2Dim("fact_sales"),
		scd1Dim("users"),
		scd1Dim("dim_product"),
	}}
	c := cls(paradigm.ParadigmMixed, map[string]paradigm.Role{
		"fact_sales":  paradigm.RoleFact,
		"users":       paradigm.RoleUnclassified,
		"dim_product": paradigm.RoleDimension,
	})

	if got := run011(s, c); len(got) != 0 {
		t.Errorf("DW-011 items = %d, want 0 (only one dimension-role table)", len(got))
	}
}

// The single item anchors on the first COMPARED dimension in schema order, so
// its baseline fingerprint is stable.
func TestDW011_AnchorsOnFirstComparedDimension(t *testing.T) {
	first := scd2Dim("dim_customer")
	first.Pos = db.Pos{File: "warehouse.sql", Line: 42}
	second := scd1Dim("dim_product")
	second.Pos = db.Pos{File: "warehouse.sql", Line: 200}

	s, c := dimsOnly(first, second)

	got := itemsOfCategory(run011(s, c), surface.CategoryDWMixedSCDStrategies)
	if len(got) != 1 {
		t.Fatalf("DW-011 items = %d, want 1", len(got))
	}
	if got[0].File != "warehouse.sql" || got[0].Line != 42 {
		t.Errorf("anchor = %s:%d, want warehouse.sql:42", got[0].File, got[0].Line)
	}
}

func TestDW011_ID(t *testing.T) {
	if got := (dw011{}).ID(); got != "DW-011" {
		t.Errorf("ID() = %q, want %q", got, "DW-011")
	}
}

func TestDW011_RegisteredInAll(t *testing.T) {
	if !registeredIn("DW-011") {
		t.Error("DW-011 is not registered in dwrules.All() — the sensor would never run it")
	}
}
