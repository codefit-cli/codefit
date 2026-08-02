package dwrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// --- shared helpers for the DW rule tests (the crossrules convention: the
// first rule's test file owns the helpers, the siblings reuse them) ---
//
// These tests hand-build the paradigm.Classification instead of calling
// paradigm.Detect. That is DELIBERATE: a rule's unit test must fail for the
// RULE's reason, not for a detection-heuristic reason, and Detect's
// corroboration thresholds (FK fan-out / fan-in) are S1's contract with their
// own tests. The REAL Detect→rule path is proven end-to-end twice: in
// internal/sensors/db (a star schema audited through Sensor.Audit) and in
// internal/providers/sqlddl (the vendored AdventureWorksDW DDL).
//
// DECLARED SYNTHETIC (ADR 0028 fixture-gap policy), stated once for the whole
// DW rule-test family rather than implied: EVERY schema in these unit tests is
// constructed, not taken from a real corpus. The vendored real warehouse
// (AdventureWorksDW) IS now reached end to end — both of the limits that used
// to keep it silent are closed (the role-name vocabulary recognizes its
// PascalCase Kimball spelling, and the T-SQL reducer reads the
// ALTER TABLE ... ADD CONSTRAINT shapes it uses), so it classifies olap with
// real fact/dimension roles, locked by
// TestDW_AdventureWorksDW_StarIsVisible_AsVendored in
// internal/providers/sqlddl/dw_integration_test.go. What that corpus still
// does NOT carry is the per-rule fire paths: NONE of the five S2 rules
// (DW-001/002/005/010/011) fires on it, so the positive and TRAP (negative)
// cases below stay constructed for each rule. DW-002 used to fire there, but
// for a PARSER reason rather than a modelling one — the delimited [int] type
// read as unknown, so its two real integer surrogates could not be proven —
// and that gap is closed (tsql-bracketed-type-names), which removed the item
// rather than adding a fire path. (S3's DW-021 does fire on that corpus — one
// dw-fact-no-columnar-index item — but it is not one of the five this file's
// family covers.)

// dwtbl builds a positioned table.
func dwtbl(name string, cols ...db.Column) db.Table {
	return db.Table{Name: name, Pos: db.Pos{File: "schema.sql", Line: 1}, Complete: true, Columns: cols}
}

// dwcol builds a typed column.
func dwcol(name string, t db.Type) db.Column {
	return db.Column{Name: name, Type: t, RawType: string(t), Pos: db.Pos{File: "schema.sql", Line: 1}}
}

// dwfk builds a single-column foreign key to refTable.
func dwfk(col, refTable, refCol string) db.ForeignKey {
	return db.ForeignKey{
		Pos:        db.Pos{File: "schema.sql", Line: 1},
		Columns:    []string{col},
		RefTable:   refTable,
		RefColumns: []string{refCol},
	}
}

// cls builds a Classification from an explicit table→role map.
func cls(p paradigm.Paradigm, roles map[string]paradigm.Role) *paradigm.Classification {
	return &paradigm.Classification{Paradigm: p, Roles: roles}
}

// itemsOfCategory filters surface items by category.
func itemsOfCategory(items []findings.SurfaceItem, c surface.Category) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == string(c) {
			out = append(out, it)
		}
	}
	return out
}

// signalValue returns the value of the "<key>: " StructuralSignal, or "" when absent.
func signalValue(it findings.SurfaceItem, key string) string {
	prefix := key + ": "
	for _, sig := range it.StructuralSignals {
		if len(sig) > len(prefix) && sig[:len(prefix)] == prefix {
			return sig[len(prefix):]
		}
	}
	return ""
}

func run001(s *db.Schema, c *paradigm.Classification) []findings.SurfaceItem {
	fs, surf := dw001{}.Check(s, c)
	if len(fs) != 0 {
		panic("DW-001 must be SURFACE-only (ADR 0017), never an affirmation")
	}
	return surf
}

// --- DW-001: fact table with no foreign key to any dimension ---

// A fact-role table whose foreign keys reach only NON-dimension tables is a
// broken star: the star's whole point is that measures join to dimensions.
// This is the rule's headline positive.
func TestDW001_FactWithoutDimensionFK_Fires(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		func() db.Table {
			tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
			tb.ForeignKeys = []db.ForeignKey{dwfk("staging_id", "stg_import", "id")}
			return tb
		}(),
		dwtbl("stg_import", dwcol("id", db.TypeInt)),
	}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{
		"fact_sales": paradigm.RoleFact,
		"stg_import": paradigm.RoleStaging,
	})

	got := itemsOfCategory(run001(s, c), surface.CategoryDWNoFactDimensionFK)
	if len(got) != 1 {
		t.Fatalf("DW-001 items = %d, want 1 (fact_sales has no FK to a dimension)", len(got))
	}
	if tbl := signalValue(got[0], "table"); tbl != "fact_sales" {
		t.Errorf("table signal = %q, want %q", tbl, "fact_sales")
	}
	if got[0].StructuralFacts["dimension_fk_detected"] {
		t.Error("dimension_fk_detected must be false when no FK reaches a dimension")
	}
}

// THE TRAP: a fact table that DOES reference a dimension must not fire, even
// when it also carries foreign keys to non-dimension tables. One dimension FK
// is enough to make it a star.
func TestDW001_FactWithOneDimensionFK_DoesNotFire(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		func() db.Table {
			tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
			tb.ForeignKeys = []db.ForeignKey{
				dwfk("staging_id", "stg_import", "id"),
				dwfk("customer_key", "dim_customer", "customer_key"),
			}
			return tb
		}(),
		dwtbl("dim_customer", dwcol("customer_key", db.TypeInt)),
		dwtbl("stg_import", dwcol("id", db.TypeInt)),
	}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{
		"fact_sales":   paradigm.RoleFact,
		"dim_customer": paradigm.RoleDimension,
		"stg_import":   paradigm.RoleStaging,
	})

	if got := itemsOfCategory(run001(s, c), surface.CategoryDWNoFactDimensionFK); len(got) != 0 {
		t.Errorf("DW-001 items = %d, want 0 (fact_sales joins dim_customer)", len(got))
	}
}

// A fact table referencing ANOTHER FACT (a common factless-bridge shape) is
// still not a star join — the second trap named in the task list.
func TestDW001_FactReferencingOnlyAnotherFact_Fires(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		func() db.Table {
			tb := dwtbl("fact_sales_reason", dwcol("reason", db.TypeString))
			tb.ForeignKeys = []db.ForeignKey{dwfk("order_no", "fact_sales", "order_no")}
			return tb
		}(),
		dwtbl("fact_sales", dwcol("order_no", db.TypeString)),
	}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{
		"fact_sales_reason": paradigm.RoleFact,
		"fact_sales":        paradigm.RoleFact,
	})

	got := itemsOfCategory(run001(s, c), surface.CategoryDWNoFactDimensionFK)
	if len(got) != 2 {
		t.Fatalf("DW-001 items = %d, want 2 (neither fact reaches a dimension)", len(got))
	}
}

// EDGE: a fact with no declared foreign keys at all — a degenerate fact — must
// fire too, and say so plainly rather than silently passing on an empty list.
func TestDW001_FactWithNoForeignKeysAtAll_Fires(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{dwtbl("fact_events", dwcol("amount", db.TypeFloat))}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_events": paradigm.RoleFact})

	got := itemsOfCategory(run001(s, c), surface.CategoryDWNoFactDimensionFK)
	if len(got) != 1 {
		t.Fatalf("DW-001 items = %d, want 1 (degenerate fact, zero FKs)", len(got))
	}
	if fks := signalValue(got[0], "foreign_keys"); fks != "(none)" {
		t.Errorf("foreign_keys signal = %q, want %q", fks, "(none)")
	}
}

// EDGE: a schema with no fact-role table has nothing for DW-001 to say — the
// rule must not reach for unclassified or dimension tables.
func TestDW001_NoFactRoleTables_EmitsNothing(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		dwtbl("users", dwcol("id", db.TypeInt)),
		dwtbl("dim_customer", dwcol("customer_key", db.TypeInt)),
	}}
	c := cls(paradigm.ParadigmOLTP, map[string]paradigm.Role{
		"users":        paradigm.RoleUnclassified,
		"dim_customer": paradigm.RoleDimension,
	})

	if got := run001(s, c); len(got) != 0 {
		t.Errorf("DW-001 items = %d, want 0 (no fact-role table in the schema)", len(got))
	}
}

// The rule ID is part of the contract (coverage docs and the task list both
// name it).
func TestDW001_ID(t *testing.T) {
	if got := (dw001{}).ID(); got != "DW-001" {
		t.Errorf("ID() = %q, want %q", got, "DW-001")
	}
}

// registeredIn reports whether All() contains a rule with the given ID. A rule
// that is implemented and tested but never wired into All() is dead code the
// sensor never runs — the one failure mode a per-rule unit test cannot catch.
func registeredIn(id string) bool {
	for _, r := range All() {
		if r.ID() == id {
			return true
		}
	}
	return false
}

func TestDW001_RegisteredInAll(t *testing.T) {
	if !registeredIn("DW-001") {
		t.Error("DW-001 is not registered in dwrules.All() — the sensor would never run it")
	}
}

// A fact declaring TWO foreign keys to the SAME table must report that table
// once, not twice — the signal is a set of referenced tables, and a duplicate
// would misrepresent the fan-out to the agent. (AdventureWorksDW's real
// FactInternetSales does exactly this: three separate FKs to DimDate.)
func TestDW001_RepeatedForeignKeyToSameTable_ReportedOnce(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.ForeignKeys = []db.ForeignKey{
		dwfk("order_date_key", "stg_dates", "id"),
		dwfk("ship_date_key", "stg_dates", "id"),
	}
	s := &db.Schema{Tables: []db.Table{tb, dwtbl("stg_dates", dwcol("id", db.TypeInt))}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{
		"fact_sales": paradigm.RoleFact,
		"stg_dates":  paradigm.RoleStaging,
	})

	got := itemsOfCategory(run001(s, c), surface.CategoryDWNoFactDimensionFK)
	if len(got) != 1 {
		t.Fatalf("DW-001 items = %d, want 1", len(got))
	}
	if v := signalValue(got[0], "foreign_keys"); v != "stg_dates" {
		t.Errorf("foreign_keys signal = %q, want %q (deduplicated)", v, "stg_dates")
	}
}
