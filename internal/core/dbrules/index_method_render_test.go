package dbrules_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// F4 (coordinator review, index-method-capture): a T-SQL CLUSTERED COLUMNSTORE
// INDEX carries db.Index{Columns: nil, Method: "columnstore"}. Before this
// fix, describeIndexLike rendered that entry as the bare literal "[]" — the
// SAME string an actual rendering bug would produce, and indistinguishable
// from "no index at all" other than by careful reading — with no mention of
// Method, hiding the one fact (a columnstore covers every column) the agent
// needs to judge whether DB-001's "should this FK be indexed?" question still
// applies. Disposition CHOSEN: keep coverage semantics unchanged (a
// columnstore correctly never satisfies CoveredByOrderedPrefix/
// CoveredBySetPrefix — it is not an ordered lookup structure) but render the
// zero-column case HONESTLY and SURFACE its Method in the signal, instead of
// hiding it behind a misleading "[]".
func signalValue(it findings.SurfaceItem, key string) string {
	prefix := key + ": "
	for _, s := range it.StructuralSignals {
		if strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return ""
}

func TestDB001_ColumnstoreIndex_RendersHonestly_NotBareBrackets(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "FactSales", Pos: db.Pos{File: "s.sql", Line: 1}, Complete: true,
		ForeignKeys: []db.ForeignKey{
			{Columns: []string{"customerId"}, RefTable: "DimCustomer", RefColumns: []string{"id"}, Pos: db.Pos{File: "s.sql", Line: 5}},
		},
		// A T-SQL CLUSTERED COLUMNSTORE INDEX: zero columns, Method set —
		// exactly what applyCreateColumnstoreIndex (reduce.go) produces.
		Indexes: []db.Index{{Pos: db.Pos{File: "s.sql", Line: 2}, Method: "columnstore"}},
	}}}
	_, items := run(s)
	got := surfaceWithCategory(items, surface.CategoryDBFKNoIndex)
	if len(got) != 1 {
		t.Fatalf("DB-001 surface = %d, want 1 — a columnstore index correctly does not satisfy an ordered FK lookup, so DB-001 must still ask", len(got))
	}
	v := signalValue(got[0], "existing_indexes")
	if strings.Contains(v, "[]") {
		t.Fatalf("existing_indexes = %q, contains a bare \"[]\" — that reads as \"an index covering zero columns\", indistinguishable from a rendering bug", v)
	}
	if !strings.Contains(v, "columnstore") {
		t.Errorf("existing_indexes = %q, want it to mention the columnstore Method so the agent has the fact it needs to judge whether an additional index is still warranted", v)
	}
	if want := "(covers all columns) method=columnstore"; v != want {
		t.Errorf("existing_indexes = %q, want exactly %q", v, want)
	}
}

// TestDB011_TwoZeroColumnIndexes_NeverFlaggedAsDuplicates locks the OTHER
// consumer bug the coordinator flagged: db011 keys duplicate detection by
// {Join(Columns), Unique} with no empty guard, so two DIFFERENT zero-column
// indexes (e.g. two columnstore-shaped statements) would collide and report
// "duplicates another index on the same columns []" — a claim that means
// nothing, since neither index declares any columns to compare.
func TestDB011_TwoZeroColumnIndexes_NeverFlaggedAsDuplicates(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "FactSales", Complete: true,
		Indexes: []db.Index{
			{Pos: db.Pos{File: "s.sql", Line: 3}, Method: "columnstore"},
			{Pos: db.Pos{File: "s.sql", Line: 9}, Method: "columnstore"},
		},
	}}}
	_, items := run(s)
	got := surfaceWithCategory(items, surface.CategoryDBDupIndex)
	if len(got) != 0 {
		t.Errorf("DB-011a fired %d duplicate(s) on two zero-column indexes, want 0 — \"duplicates another index on the same columns []\" is a meaningless claim; got %+v", len(got), got)
	}
}

// Positive control: two REAL indexes with genuinely identical, non-empty
// column lists must still fire — the zero-column guard must not blunt db011
// for the ordinary case it already covers.
func TestDB011_TwoRealIdenticalIndexes_StillFires(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T", Complete: true,
		Indexes: []db.Index{
			{Pos: db.Pos{File: "s.sql", Line: 3}, Columns: []string{"email"}},
			{Pos: db.Pos{File: "s.sql", Line: 9}, Columns: []string{"email"}},
		},
	}}}
	_, items := run(s)
	got := surfaceWithCategory(items, surface.CategoryDBDupIndex)
	if len(got) != 1 {
		t.Errorf("DB-011a fired %d duplicate(s), want 1 (positive control — the zero-column guard must not blunt the ordinary case)", len(got))
	}
}
