package crossrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/query"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// col builds a column with a schema position, so an emitted item can be checked to
// anchor at the column's line.
func col(name string, line int) db.Column {
	return db.Column{Name: name, Pos: db.Pos{File: "schema.prisma", Line: line}}
}

// tbl builds a table with positioned columns; indexes/PK are set by the caller.
func tbl(name string, cols ...db.Column) db.Table {
	return db.Table{Name: name, Pos: db.Pos{File: "schema.prisma", Line: 1}, Columns: cols}
}

func filter(model string, cols ...string) query.QueryFilter {
	return query.QueryFilter{Model: model, Columns: cols, Composite: len(cols) > 1}
}

func run010(s *db.Schema, filters ...query.QueryFilter) []findings.SurfaceItem {
	_, surf := db010{}.Check(s, filters)
	return surf
}

// TestDB010_UncoveredColumnEmits — a filtered column with no index at all is
// surfaced, anchored to the schema column, with the cross category. Findings stay
// nil (surface, not a deterministic finding — ADR 0030).
func TestDB010_UncoveredColumnEmits(t *testing.T) {
	s := schema(tbl("User", col("id", 2), col("status", 5)))
	s.Tables[0].PrimaryKey = []string{"id"}

	fs, surf := db010{}.Check(s, []query.QueryFilter{filter("User", "status")})
	if fs != nil {
		t.Errorf("DB-010 must emit no findings (surface only), got %v", fs)
	}
	if len(surf) != 1 {
		t.Fatalf("want 1 surface item, got %d: %+v", len(surf), surf)
	}
	if surf[0].Category != string(surface.CategoryDBFilteredColumnNoIndex) {
		t.Errorf("category = %q, want %q", surf[0].Category, surface.CategoryDBFilteredColumnNoIndex)
	}
	if surf[0].File != "schema.prisma" || surf[0].Line != 5 {
		t.Errorf("anchor = %s:%d, want schema.prisma:5 (the column's schema position)", surf[0].File, surf[0].Line)
	}
	if surf[0].StructuralFacts["covering_index_detected"] {
		t.Error("covering_index_detected must be false")
	}
}

// TestDB010_PrimaryKeyCovers — a filter on a PK column emits nothing: the PK is an
// implicit index (the trap: a rule looking only at explicit @@index would false-
// positive on every id).
func TestDB010_PrimaryKeyCovers(t *testing.T) {
	s := schema(tbl("User", col("id", 2)))
	s.Tables[0].PrimaryKey = []string{"id"}
	if got := run010(s, filter("User", "id")); len(got) != 0 {
		t.Errorf("PK column is implicitly indexed → must emit nothing, got %+v", got)
	}
}

// TestDB010_UniqueCovers — a filter on a @unique/@@unique column emits nothing.
func TestDB010_UniqueCovers(t *testing.T) {
	s := schema(tbl("User", col("id", 2), col("email", 3)))
	s.Tables[0].PrimaryKey = []string{"id"}
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"email"}, Unique: true}}
	if got := run010(s, filter("User", "email")); len(got) != 0 {
		t.Errorf("unique column is indexed → must emit nothing, got %+v", got)
	}
}

// TestDB010_CompositeLeftmostIsTheHeart — a composite index [a,b] covers a filter
// on the LEADING column a (emit nothing) but NOT on the trailing column b (emit).
// The same index covers one column and not the other, by position.
func TestDB010_CompositeLeftmostIsTheHeart(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("tenantId", 3), col("status", 4)))
	s.Tables[0].PrimaryKey = []string{"id"}
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"tenantId", "status"}}}

	if got := run010(s, filter("Event", "tenantId")); len(got) != 0 {
		t.Errorf("leading column of composite [tenantId,status] is covered → must emit nothing, got %+v", got)
	}
	got := run010(s, filter("Event", "status"))
	if len(got) != 1 {
		t.Fatalf("trailing column of composite [tenantId,status] is NOT covered → must emit 1, got %+v", got)
	}
	if got[0].Line != 4 {
		t.Errorf("anchor line = %d, want 4 (status column)", got[0].Line)
	}
}

// TestDB010_AbstainsOnInexactMatch — reconcile abstaining (missing column, cross-
// naming-space physical-vs-logical, unknown model) emits nothing (Complete==false).
func TestDB010_AbstainsOnInexactMatch(t *testing.T) {
	s := schema(tbl("User", col("id", 2), col("user_email", 3))) // SQL-DDL: physical name
	s.Tables[0].PrimaryKey = []string{"id"}

	cases := []struct {
		name   string
		filter query.QueryFilter
	}{
		{"unknown model", filter("Ghost", "x")},
		{"column does not exist", filter("User", "nope")},
		{"cross-naming-space: field email vs column user_email", filter("User", "email")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := run010(s, c.filter); len(got) != 0 {
				t.Errorf("abstain must emit nothing, got %+v", got)
			}
		})
	}
}

// TestDB010_DedupByTableColumn — two queries filtering the same uncovered column
// yield ONE item (add one index), not one per query.
func TestDB010_DedupByTableColumn(t *testing.T) {
	s := schema(tbl("User", col("id", 2), col("email", 3)))
	s.Tables[0].PrimaryKey = []string{"id"}
	got := run010(s, filter("User", "email"), filter("User", "email"))
	if len(got) != 1 {
		t.Errorf("two filters on the same uncovered column → 1 deduped item, got %+v", got)
	}
}

// TestDB010_DefersMultiColumnFiltersToDB013 fixes the DB-010/DB-013 precedence
// (ADR 0031, option b): a multi-column WHERE is DEFERRED WHOLE to DB-013, never
// split into per-column DB-010 concerns — a multi-column filter wants a composite
// index, not a standalone index per column. DB-010 owns single-column filters only.
func TestDB010_DefersMultiColumnFiltersToDB013(t *testing.T) {
	s := schema(tbl("User", col("id", 2), col("a", 3), col("b", 4)))
	s.Tables[0].PrimaryKey = []string{"id"}
	if got := run010(s, filter("User", "a", "b")); len(got) != 0 {
		t.Fatalf("a multi-column filter (a AND b) is DB-013's — DB-010 must emit nothing, got %+v", got)
	}
}

// TestDB010_OverlapCaseGoesWholeToDB013 is the exact overlap the precedence
// resolves: index [a], a query filtering (a AND b). The trailing column b is
// uncovered, but DB-010 must NOT emit on it — the whole multi-column filter is
// DB-013's, whose composite recommendation (@@index([a,b])) is the single correct
// fix. Without this, one filter would yield two surface items for one index change.
func TestDB010_OverlapCaseGoesWholeToDB013(t *testing.T) {
	s := schema(tbl("User", col("id", 2), col("a", 3), col("b", 4)))
	s.Tables[0].PrimaryKey = []string{"id"}
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"a"}}}
	if got := run010(s, filter("User", "a", "b")); len(got) != 0 {
		t.Fatalf("index [a] + filter (a,b): DB-010 must defer the whole filter to DB-013, got %+v", got)
	}
}

// TestDB010_SingleColumnFilterStillCaught guards the other side of the split: a
// column the code ALSO filters on its own arrives as its own single-column filter
// and IS surfaced, even when a multi-column filter over it exists too.
func TestDB010_SingleColumnFilterStillCaught(t *testing.T) {
	s := schema(tbl("User", col("id", 2), col("a", 3), col("b", 4)))
	s.Tables[0].PrimaryKey = []string{"id"}
	got := run010(s, filter("User", "a", "b"), filter("User", "b"))
	if len(got) != 1 {
		t.Fatalf("the standalone filter on b must be caught (the multi-column one deferred), got %+v", got)
	}
	if got[0].StructuralSignals[1] != "filtered_column: b" {
		t.Errorf("expected the single-column concern on b, got %+v", got[0].StructuralSignals)
	}
}
