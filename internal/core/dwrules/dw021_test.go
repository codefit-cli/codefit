package dwrules

import (
	"strings"
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
// real `USING brin` index, and separately a real T-SQL `columnstore`
// index), and the end-to-end abstention over a real genuinely-unrecognized
// index shape (`ON ONLY`), per the task's "at minimum one real-parser test
// for each fire path" requirement. `gin` is DELIBERATELY excluded from the
// vocabulary (4R review, coordinator round, C1) — see dw021.go's doc
// comment for why — so it has NO real-parser trap test; the hand-built
// TestDW021_FactWithGINIndex_Fires below locks the opposite direction
// instead.

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

// BOUNDARY (C1, 4R review — architect decision): gin is DELIBERATELY
// excluded from the vocabulary despite being a real PostgreSQL access
// method, because no coherent "columnar/analytic" criterion admits gin
// while rejecting its siblings gist/spgist (see dw021.go's doc comment). A
// fact table whose only index is GIN must therefore FIRE, exactly like any
// other non-columnar method — the inverse of what this test asserted before
// C1. (Renamed from TestDW021_FactWithGINIndex_DoesNotFire.)
func TestDW021_FactWithGINIndex_Fires(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	tb.Indexes = []db.Index{dwidx("gin", "amount")}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	if got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex); len(got) != 1 {
		t.Errorf("DW-021 items = %d, want 1 (gin is deliberately NOT a recognized columnar method)", len(got))
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

// W1 (4R review, REL-002): a fact table keyed ONLY by a primary key, with no
// secondary index at all, still has an index-like structure — db.IndexLike's
// "the PK counts as an implicit index" convention, already shared by
// DB-001/DB-010/DB-011b/DW-010 — so has_any_index must read true and the
// signal must MENTION the PK, never claim "(none)". The fire decision itself
// is unchanged: a PK carries no Method, so it can never satisfy the
// columnar/analytic vocabulary.
func TestDW021_FactWithOnlyPrimaryKey_FiresAndReportsThePK(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("sale_id", db.TypeInt))
	tb.PrimaryKey = []string{"sale_id"}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
	if len(got) != 1 {
		t.Fatalf("DW-021 items = %d, want 1 (a PK carries no columnar method, so it still fires)", len(got))
	}
	if !got[0].StructuralFacts["has_any_index"] {
		t.Error("has_any_index must be true — the primary key IS an index-like structure (db.IndexLike's convention)")
	}
	if v := signalValue(got[0], "existing_index_methods"); v == "(none)" || v == "" {
		t.Errorf("existing_index_methods signal = %q, must mention the primary key rather than claim there is none", v)
	}
}

// S3 (4R review, RES-001): describeIndexMethods must cap its rendered list,
// the same convention dbrules.routeUnprovenTable already uses for its own
// unreduced-statement signal, so a pathological table cannot balloon a
// single surface item unboundedly.
func TestDW021_ManyIndexes_MethodsListIsCapped(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	for i := 0; i < columnarIndexSignalCap+2; i++ {
		tb.Indexes = append(tb.Indexes, dwidx("btree", "amount"))
	}
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
	if len(got) != 1 {
		t.Fatalf("DW-021 items = %d, want 1", len(got))
	}
	v := signalValue(got[0], "existing_index_methods")
	if !strings.Contains(v, "more") {
		t.Errorf("existing_index_methods signal = %q, want it capped with an \"N more\" suffix "+
			"(%d indexes present, cap is %d)", v, columnarIndexSignalCap+2, columnarIndexSignalCap)
	}
	if got := strings.Count(v, "btree"); got != columnarIndexSignalCap {
		t.Errorf("rendered %d btree entries, want exactly the cap (%d)", got, columnarIndexSignalCap)
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

// S1 (4R review READ-005): ReasonToReview's vocabulary mention must be
// DERIVED from columnarIndexMethods, not restated by hand — otherwise the
// doc comment's "defined ONCE" claim is false for the agent-facing text.
// Locked structurally (every current vocabulary word present, the
// deliberately-excluded gin absent) rather than as an exact-string match, so
// a future vocabulary change updates this test's premise without editing its
// assertions.
func TestDW021_ReasonToReview_DerivesVocabularyFromTheMap(t *testing.T) {
	tb := dwtbl("fact_sales", dwcol("amount", db.TypeFloat))
	s := &db.Schema{Tables: []db.Table{tb}}
	c := cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{"fact_sales": paradigm.RoleFact})

	got := itemsOfCategory(run021(s, c), surface.CategoryDWNoColumnarIndex)
	if len(got) != 1 {
		t.Fatalf("DW-021 items = %d, want 1", len(got))
	}
	reason := got[0].ReasonToReview
	for method := range columnarIndexMethods {
		if !strings.Contains(reason, method) {
			t.Errorf("ReasonToReview = %q, missing vocabulary word %q — it must be derived from "+
				"columnarIndexMethods, not restated by hand", reason, method)
		}
	}
	if strings.Contains(reason, "gin") {
		t.Errorf("ReasonToReview = %q, must NOT mention gin — it is deliberately excluded from the vocabulary", reason)
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
