package dwrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

func run010(s *db.Schema, c *paradigm.Classification) []findings.SurfaceItem {
	fs, surf := dw010{}.Check(s, c)
	if len(fs) != 0 {
		panic("DW-010 must be SURFACE-only (ADR 0017), never an affirmation")
	}
	return surf
}

// withIndex returns a copy of tb carrying one extra non-unique index.
func withIndex(tb db.Table, cols ...string) db.Table {
	tb.Indexes = append(tb.Indexes, db.Index{Pos: tb.Pos, Columns: cols})
	return tb
}

// scd2Dim is the canonical SCD-2 dimension used across these cases: a
// surrogate key, the business key, and the three history columns.
//
// DECLARED SYNTHETIC (ADR 0028 fixture-gap policy): no shape in the vendored
// real corpora carries SCD-2 currency columns — AdventureWorksDW's DimProduct
// uses StartDate/EndDate/Status, which is a different vocabulary — so both the
// positive and the trap for DW-010/DW-011 are constructed here rather than
// borrowed from real DDL. Stated, not implied.
func scd2Dim(name string) db.Table {
	return dimTable(name, []string{"customer_sk"},
		dwcol("customer_sk", db.TypeInt),
		dwcol("customer_code", db.TypeString),
		dwcol("valid_from", db.TypeDateTime),
		dwcol("valid_to", db.TypeDateTime),
		dwcol("is_current", db.TypeBool),
	)
}

// The headline positive: an SCD-2 dimension with history columns and nothing
// indexing them. Every "give me the current row" query — which is most of them
// — becomes a full scan of the whole version history.
func TestDW010_SCD2DimensionWithNoCurrencyIndex_Fires(t *testing.T) {
	s, c := onlyDim(scd2Dim("dim_customer"))

	got := itemsOfCategory(run010(s, c), surface.CategoryDWSCD2NoCurrencyIndex)
	if len(got) != 1 {
		t.Fatalf("DW-010 items = %d, want 1", len(got))
	}
	if got[0].StructuralFacts["currency_index_detected"] {
		t.Error("currency_index_detected must be false")
	}
	if v := signalValue(got[0], "currency_columns"); v != "valid_to, is_current" {
		t.Errorf("currency_columns signal = %q, want %q", v, "valid_to, is_current")
	}
	if v := signalValue(got[0], "scd_columns"); v != "valid_from, valid_to, is_current" {
		t.Errorf("scd_columns signal = %q, want all three history columns", v)
	}
}

// THE TRAP: an index on is_current serves the currency lookup, so the rule
// must go quiet. It does not matter that valid_to is still unindexed — one
// index is enough to answer "which row is current".
func TestDW010_IndexOnIsCurrent_DoesNotFire(t *testing.T) {
	s, c := onlyDim(withIndex(scd2Dim("dim_customer"), "is_current"))

	if got := itemsOfCategory(run010(s, c), surface.CategoryDWSCD2NoCurrencyIndex); len(got) != 0 {
		t.Errorf("DW-010 items = %d, want 0 (is_current is indexed)", len(got))
	}
}

// The other currency column serves the same lookup.
func TestDW010_IndexOnValidTo_DoesNotFire(t *testing.T) {
	s, c := onlyDim(withIndex(scd2Dim("dim_customer"), "valid_to"))

	if got := itemsOfCategory(run010(s, c), surface.CategoryDWSCD2NoCurrencyIndex); len(got) != 0 {
		t.Errorf("DW-010 items = %d, want 0 (valid_to is indexed)", len(got))
	}
}

// BOUNDARY: a composite index whose LEADING column is something else does NOT
// cover the currency column — a B-tree serves leading prefixes only. Reusing
// db.CoveredByOrderedPrefix is what keeps this consistent with DB-001/DB-010
// instead of re-deriving coverage semantics per rule.
func TestDW010_CompositeIndexNotLeadingOnCurrency_Fires(t *testing.T) {
	s, c := onlyDim(withIndex(scd2Dim("dim_customer"), "customer_code", "is_current"))

	if got := itemsOfCategory(run010(s, c), surface.CategoryDWSCD2NoCurrencyIndex); len(got) != 1 {
		t.Errorf("DW-010 items = %d, want 1 ([customer_code, is_current] does not serve an is_current lookup)", len(got))
	}
}

// The primary key counts as an implicit index (db.IndexLike), so a dimension
// keyed on the currency column is covered.
func TestDW010_PrimaryKeyOnCurrencyColumn_DoesNotFire(t *testing.T) {
	tb := scd2Dim("dim_customer")
	tb.PrimaryKey = []string{"is_current", "customer_sk"}
	s, c := onlyDim(tb)

	if got := itemsOfCategory(run010(s, c), surface.CategoryDWSCD2NoCurrencyIndex); len(got) != 0 {
		t.Errorf("DW-010 items = %d, want 0 (the PK is an implicit index leading on is_current)", len(got))
	}
}

// EDGE: a dimension with NO slowly-changing columns is not an SCD-2 table and
// must never be evaluated — asking it for a currency index is meaningless.
func TestDW010_DimensionWithoutSCDColumns_NotEvaluated(t *testing.T) {
	s, c := onlyDim(dimTable("dim_customer", []string{"customer_sk"},
		dwcol("customer_sk", db.TypeInt),
		dwcol("name", db.TypeString),
	))

	if got := run010(s, c); len(got) != 0 {
		t.Errorf("DW-010 items = %d, want 0 (not an SCD-2 dimension)", len(got))
	}
}

// EDGE: only ONE of the two currency columns present — the table is still
// evaluated, against whichever exists.
func TestDW010_OnlyValidToPresent_StillEvaluated(t *testing.T) {
	tb := dimTable("dim_customer", []string{"customer_sk"},
		dwcol("customer_sk", db.TypeInt),
		dwcol("valid_from", db.TypeDateTime),
		dwcol("valid_to", db.TypeDateTime),
	)
	s, c := onlyDim(tb)

	got := itemsOfCategory(run010(s, c), surface.CategoryDWSCD2NoCurrencyIndex)
	if len(got) != 1 {
		t.Fatalf("DW-010 items = %d, want 1 (valid_to alone is still a currency column)", len(got))
	}
	if v := signalValue(got[0], "currency_columns"); v != "valid_to" {
		t.Errorf("currency_columns signal = %q, want %q", v, "valid_to")
	}
}

// EDGE: SCD columns present but NEITHER currency column — there is no lookup
// to index, so the rule abstains rather than inventing a demand.
func TestDW010_HistoryColumnsButNoCurrencyColumn_Abstains(t *testing.T) {
	s, c := onlyDim(dimTable("dim_customer", []string{"customer_sk"},
		dwcol("customer_sk", db.TypeInt),
		dwcol("valid_from", db.TypeDateTime),
		dwcol("effective_date", db.TypeDateTime),
	))

	if got := run010(s, c); len(got) != 0 {
		t.Errorf("DW-010 items = %d, want 0 (no valid_to / is_current to index)", len(got))
	}
}

// Column names are matched with separators stripped, so validTo / IsCurrent
// (camelCase, as a Prisma model would spell them) are recognized too.
func TestDW010_CamelCaseSCDColumnNames_Recognized(t *testing.T) {
	s, c := onlyDim(dimTable("dim_customer", []string{"customerSk"},
		dwcol("customerSk", db.TypeInt),
		dwcol("validFrom", db.TypeDateTime),
		dwcol("validTo", db.TypeDateTime),
		dwcol("isCurrent", db.TypeBool),
	))

	if got := itemsOfCategory(run010(s, c), surface.CategoryDWSCD2NoCurrencyIndex); len(got) != 1 {
		t.Errorf("DW-010 items = %d, want 1 (camelCase SCD columns must be recognized)", len(got))
	}
}

// Only DIMENSION-role tables are evaluated — a fact table with an is_current
// flag is none of DW-010's business.
func TestDW010_NonDimensionRole_EmitsNothing(t *testing.T) {
	tb := scd2Dim("fact_sales")
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	if got := run010(s, c); len(got) != 0 {
		t.Errorf("DW-010 items = %d, want 0 (not a dimension)", len(got))
	}
}

func TestDW010_ID(t *testing.T) {
	if got := (dw010{}).ID(); got != "DW-010" {
		t.Errorf("ID() = %q, want %q", got, "DW-010")
	}
}

func TestDW010_RegisteredInAll(t *testing.T) {
	if !registeredIn("DW-010") {
		t.Error("DW-010 is not registered in dwrules.All() — the sensor would never run it")
	}
}
