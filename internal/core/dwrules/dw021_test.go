package dwrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// --- DW-021: fact table with no dialect-recognized columnar index ---
//
// DECLARED SYNTHETIC (ADR 0028 fixture-gap policy), same convention as
// dw001_test.go's family-wide note: every schema below is constructed, not
// taken from a real corpus.

func run021(s *db.Schema, c *paradigm.Classification) []findings.SurfaceItem {
	fs, surf := dw021{}.Check(s, c)
	if len(fs) != 0 {
		panic("DW-021 must be SURFACE-only (ADR 0017), never an affirmation")
	}
	return surf
}

// Positive: a fact table whose only index has no recognized columnar method
// (a plain default index) must fire.
func TestDW021_FactWithNoColumnarIndex_Fires(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.Indexes = []db.Index{{Pos: db.Pos{File: "schema.sql", Line: 1}, Columns: []string{"amount"}}}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
	if len(got) != 1 {
		t.Fatalf("DW-021 items = %d, want 1 (fact_sales has only a default-method index)", len(got))
	}
	if tbl := signalValue(got[0], "table"); tbl != "fact_sales" {
		t.Errorf("table signal = %q, want %q", tbl, "fact_sales")
	}
	if got[0].StructuralFacts["columnar_index_detected"] {
		t.Error("columnar_index_detected must be false when no index carries a recognized columnar method")
	}
}

// THE TRAP: a fact table WITH a brin index must not fire — proves the rule
// actually reads Method, not just index-existence.
func TestDW021_FactWithBrinIndex_DoesNotFire(t *testing.T) {
	tb := dwtbl("fact_events", dwcol("created_at", db.TypeDateTime))
	tb.Indexes = []db.Index{{Pos: db.Pos{File: "schema.sql", Line: 1}, Columns: []string{"created_at"}, Method: "brin"}}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_events": paradigm.RoleFact})

	if got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex); len(got) != 0 {
		t.Errorf("DW-021 items = %d, want 0 (fact_events has a brin index)", len(got))
	}
}

// A second trap: gin is also recognized columnar vocabulary.
func TestDW021_FactWithGinIndex_DoesNotFire(t *testing.T) {
	tb := dwtbl("fact_logs", dwcol("payload", db.TypeJSON))
	tb.Indexes = []db.Index{{Pos: db.Pos{File: "schema.sql", Line: 1}, Columns: []string{"payload"}, Method: "gin"}}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_logs": paradigm.RoleFact})

	if got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex); len(got) != 0 {
		t.Errorf("DW-021 items = %d, want 0 (fact_logs has a gin index)", len(got))
	}
}

// A btree index alone must still fire — btree is the ordinary, non-columnar
// default, not a member of the recognized vocabulary.
func TestDW021_FactWithOnlyBtreeIndex_Fires(t *testing.T) {
	tb := dwtbl("fact_orders", dwcol("id", db.TypeInt))
	tb.Indexes = []db.Index{{Pos: db.Pos{File: "schema.sql", Line: 1}, Columns: []string{"id"}, Method: "btree"}}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_orders": paradigm.RoleFact})

	got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
	if len(got) != 1 {
		t.Fatalf("DW-021 items = %d, want 1 (btree is not columnar vocabulary)", len(got))
	}
	if v := signalValue(got[0], "existing_index_methods"); v != "btree" {
		t.Errorf("existing_index_methods signal = %q, want %q", v, "btree")
	}
}

// EDGE: a fact table with zero indexes at all must fire and say so plainly.
func TestDW021_FactWithNoIndexesAtAll_Fires(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{dwtbl("fact_events", dwcol("amount", db.TypeFloat))}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_events": paradigm.RoleFact})

	got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
	if len(got) != 1 {
		t.Fatalf("DW-021 items = %d, want 1 (fact_events has zero indexes)", len(got))
	}
	if v := signalValue(got[0], "existing_index_methods"); v != "(none)" {
		t.Errorf("existing_index_methods signal = %q, want %q", v, "(none)")
	}
	if got[0].StructuralFacts["has_any_index"] {
		t.Error("has_any_index must be false when the table has zero indexes")
	}
}

// EDGE: a non-fact-role table is never evaluated, even with no columnar index.
func TestDW021_NoFactRoleTables_EmitsNothing(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		dwtbl("dim_customer", dwcol("customer_key", db.TypeInt)),
		dwtbl("users", dwcol("id", db.TypeInt)),
	}}
	c := cls(paradigm.ParadigmOLTP, map[string]paradigm.Role{
		"dim_customer": paradigm.RoleDimension,
		"users":        paradigm.RoleUnclassified,
	})

	if got := run021(s, c); len(got) != 0 {
		t.Errorf("DW-021 items = %d, want 0 (no fact-role table in the schema)", len(got))
	}
}

func TestDW021_ID(t *testing.T) {
	if got := (dw021{}).ID(); got != "DW-021" {
		t.Errorf("ID() = %q, want %q", got, "DW-021")
	}
}

func TestDW021_RegisteredInAll(t *testing.T) {
	if !registeredIn("DW-021") {
		t.Error("DW-021 is not registered in dwrules.All() — the sensor would never run it")
	}
}

// --- Declared limit 2: dialect-wide MySQL abstain ---
//
// db.Index.Method is systematically unreadable on MySQL (USING can appear
// AFTER the column list, a grammar position the SQL-DDL regex does not
// capture — see db.Index's doc comment). This mutation-checked before
// shipping: commenting out the `s.Dialect == "mysql"` guard makes this test
// FAIL (the same fact_sales-with-no-columnar-index shape would otherwise
// fire), restoring it makes it PASS again — recorded in the commit message.

func TestDW021_MySQLDialect_AbstainsWholeRule(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.Indexes = []db.Index{{Pos: db.Pos{File: "schema.sql", Line: 1}, Columns: []string{"amount"}}}
	s := &db.Schema{Dialect: "mysql", Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	fs, surf := dw021{}.Check(s, c)
	if len(fs) != 0 || len(surf) != 0 {
		t.Errorf("DW-021 on Schema.Dialect=mysql = (findings=%v, surface=%v), want (nil, nil) — the parser "+
			"cannot reliably read an index's method on this dialect", fs, surf)
	}
}

// Positive control: the IDENTICAL shape under an empty/PostgreSQL dialect
// still fires — proves the mysql check is dialect-SPECIFIC, not a rule that
// silently stopped firing altogether.
func TestDW021_NonMySQLDialect_StillFires(t *testing.T) {
	for _, dialect := range []string{"", "postgresql"} {
		t.Run("dialect="+dialect, func(t *testing.T) {
			tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
			tb.Indexes = []db.Index{{Pos: db.Pos{File: "schema.sql", Line: 1}, Columns: []string{"amount"}}}
			s := &db.Schema{Dialect: dialect, Tables: []db.Table{tb}}
			c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

			got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
			if len(got) != 1 {
				t.Errorf("DW-021 items = %d, want 1 (dialect=%q must not abstain)", len(got), dialect)
			}
		})
	}
}
