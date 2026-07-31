package dwrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// DECLARED SYNTHETIC (ADR 0028 fixture-gap policy), same convention as the
// sibling DW rule test files: these hand-built cases isolate the RULE's own
// vocabulary/gating logic from the parser's ability to populate
// db.Index.Method, which is proven separately by REAL-parser tests in
// internal/providers/sqlddl (dw021_integration_test.go) — the positive fire
// path (no columnar index, real PostgreSQL DDL), the negative/trap path (a
// real `USING brin`/`USING gin`/T-SQL `columnstore` index), and the
// end-to-end abstention over a real genuinely-unrecognized index shape
// (`ON ONLY`), per the task's "at minimum one real-parser test for each fire
// path" requirement.

func run021(s *db.Schema, c *paradigm.Classification) []findings.SurfaceItem {
	fs, surf := dw021{}.Check(s, c)
	if len(fs) != 0 {
		panic("DW-021 must be SURFACE-only (ADR 0017), never an affirmation")
	}
	return surf
}

// dwidx builds an index carrying method m over cols.
func dwidx(m string, cols ...string) db.Index {
	return db.Index{Pos: db.Pos{File: "schema.sql", Line: 1}, Columns: cols, Method: m}
}

// The headline positive: a fact table with only a plain (no-method) index has
// nothing serving a columnar/analytic scan.
func TestDW021_FactWithOnlyPlainIndex_Fires(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.Indexes = []db.Index{dwidx("", "amount")}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
	if len(got) != 1 {
		t.Fatalf("DW-021 items = %d, want 1", len(got))
	}
	if tbl := signalValue(got[0], "table"); tbl != "fact_sales" {
		t.Errorf("table signal = %q, want %q", tbl, "fact_sales")
	}
	if got[0].StructuralFacts["columnar_index_detected"] {
		t.Error("columnar_index_detected must be false")
	}
	if v := signalValue(got[0], "existing_index_methods"); v != "(unspecified)" {
		t.Errorf("existing_index_methods signal = %q, want %q", v, "(unspecified)")
	}
}

// EDGE: a fact table with NO indexes at all must fire too, stating the
// absence explicitly rather than silently passing on an empty list.
func TestDW021_FactWithNoIndexesAtAll_Fires(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
	if len(got) != 1 {
		t.Fatalf("DW-021 items = %d, want 1 (no indexes at all)", len(got))
	}
	if got[0].StructuralFacts["has_any_index"] {
		t.Error("has_any_index must be false")
	}
	if v := signalValue(got[0], "existing_index_methods"); v != "(none)" {
		t.Errorf("existing_index_methods signal = %q, want %q", v, "(none)")
	}
}

// THE TRAP (PostgreSQL): a BRIN index is a recognized columnar/analytic
// method — the rule must go quiet.
func TestDW021_FactWithBRINIndex_DoesNotFire(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.Indexes = []db.Index{dwidx("brin", "amount")}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	if got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex); len(got) != 0 {
		t.Errorf("DW-021 items = %d, want 0 (a BRIN index is columnar)", len(got))
	}
}

// THE TRAP (PostgreSQL): GIN is the other recognized PostgreSQL method.
func TestDW021_FactWithGINIndex_DoesNotFire(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.Indexes = []db.Index{dwidx("gin", "amount")}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	if got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex); len(got) != 0 {
		t.Errorf("DW-021 items = %d, want 0 (a GIN index is columnar)", len(got))
	}
}

// THE TRAP (T-SQL): a columnstore index, captured verbatim by the parser as
// of index-method-capture (PR #79), is the T-SQL recognized method.
func TestDW021_FactWithColumnstoreIndex_DoesNotFire(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.Indexes = []db.Index{dwidx("columnstore")} // T-SQL's own grammar names no column
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	if got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex); len(got) != 0 {
		t.Errorf("DW-021 items = %d, want 0 (a columnstore index is columnar)", len(got))
	}
}

// BOUNDARY: an unrecognized method (e.g. plain btree/hash) does not count,
// even sitting right next to a real column — only the recognized vocabulary
// counts.
func TestDW021_FactWithOnlyBtreeIndex_Fires(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.Indexes = []db.Index{dwidx("btree", "amount")}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	if got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex); len(got) != 1 {
		t.Errorf("DW-021 items = %d, want 1 (btree is not a columnar method)", len(got))
	}
}

// EDGE: a fact table with a mix of a plain and a columnar index must not
// fire — one columnar index is enough, exactly the "either column" shape
// DW-010 already uses.
func TestDW021_FactWithMixedIndexes_DoesNotFire(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat), dwcol("region", db.TypeString))
	tb.Indexes = []db.Index{dwidx("", "amount"), dwidx("brin", "region")}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	if got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex); len(got) != 0 {
		t.Errorf("DW-021 items = %d, want 0 (one columnar index among several is enough)", len(got))
	}
}

// Only FACT-role tables are evaluated — a dimension with no columnar index is
// none of DW-021's business.
func TestDW021_NonFactRole_EmitsNothing(t *testing.T) {
	tb := dwtbl("dim_customer", dwcol("name", db.TypeString))
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"dim_customer": paradigm.RoleDimension})

	if got := run021(s, c); len(got) != 0 {
		t.Errorf("DW-021 items = %d, want 0 (not a fact table)", len(got))
	}
}

// EDGE: a schema with no fact-role table at all has nothing to say.
func TestDW021_NoFactRoleTables_EmitsNothing(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{dwtbl("users", dwcol("id", db.TypeInt))}}
	c := cls(paradigm.ParadigmOLTP, map[string]paradigm.Role{"users": paradigm.RoleUnclassified})

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
