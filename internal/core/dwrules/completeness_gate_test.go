package dwrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// D4 (design SS4) — the five DW rules ABSTAIN, per table, on an unproven
// table; DW-005 and DW-011 are SCHEMA-LEVEL census judgments and therefore
// abstain the WHOLE rule when ANY relevant table is unproven — a per-table
// continue would silently shrink the census and still emit, a worse lie.

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

func TestDW005_AnyUnprovenFactOrDimension_AbstainsWholeRule(t *testing.T) {
	// A proper time dimension is present, so a PROVEN schema would NOT fire —
	// but one fact table is unproven, so the rule must abstain WHOLE, not
	// silently shrink its census by skipping just that table.
	fact := unprovenDWTable("fact_sales")
	timeDim := db.Table{Name: "dim_date", Complete: true, PrimaryKey: []string{"date_key"},
		Columns: []db.Column{{Name: "date_key", Type: db.TypeDateTime}}}
	s := &db.Schema{Tables: []db.Table{fact, timeDim}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"fact_sales": paradigm.RoleFact, "dim_date": paradigm.RoleDimension}}
	fs, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-005")})
	if len(fs) != 0 || len(surf) != 0 {
		t.Errorf("DW-005 must abstain the WHOLE rule when any fact/dimension table is unproven, got findings=%v surface=%v", fs, surf)
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

func TestDW011_AnyUnprovenDimension_AbstainsWholeRule(t *testing.T) {
	scd2 := db.Table{Name: "dim_product", Complete: true, Columns: []db.Column{{Name: "valid_to", Type: db.TypeDateTime}}}
	scd1 := unprovenDWTable("dim_category")
	s := &db.Schema{Tables: []db.Table{scd2, scd1}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"dim_product": paradigm.RoleDimension, "dim_category": paradigm.RoleDimension}}
	fs, surf := dwrules.RunWith(s, cls, []dwrules.Rule{dwRuleByID(t, "DW-011")})
	if len(fs) != 0 || len(surf) != 0 {
		t.Errorf("DW-011 must abstain the WHOLE rule when any compared dimension is unproven, got findings=%v surface=%v", fs, surf)
	}
}

// Proven-model regression locks: a fully proven schema must behave EXACTLY
// as before this change (byte-identical output), for one representative rule
// (DW-001) since the others already have their own per-rule unit tests.
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
