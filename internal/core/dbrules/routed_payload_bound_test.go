package dbrules_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// F6 (4R ledger, obs #1282): routeUnprovenTable copied EVERY
// Unreduced[].Text verbatim with no cap on COUNT or LENGTH, while every
// sibling carrier in this change is capped (sensors/db's inventory: 5
// tables / 3 reasons). An adversarial or merely pathological migration
// (hundreds of dropped ALTERs on one table, or one absurdly long statement)
// would balloon a single surface item unboundedly.

func manyUnreducedTable(count int, textLen int) db.Table {
	tb := db.Table{Name: "t", Pos: db.Pos{File: "x.sql", Line: 1}}
	for i := 0; i < count; i++ {
		tb.MarkUnproven(db.ReasonUnreducedTableStatement, strings.Repeat("X", textLen), db.Pos{File: "x.sql", Line: i + 2})
	}
	return tb
}

func TestRouteUnprovenTable_StatementCountIsBounded(t *testing.T) {
	tb := manyUnreducedTable(9, 10)
	s := &db.Schema{Tables: []db.Table{tb}}
	_, surf := dbrules.Run(s)
	var item *findings.SurfaceItem
	for i := range surf {
		if surf[i].Category == string(surface.CategoryDBTableStructureUnproven) {
			item = &surf[i]
		}
	}
	if item == nil {
		t.Fatal("no routed item found")
	}
	shown := 0
	sawMore := false
	for _, sig := range item.StructuralSignals {
		if strings.HasPrefix(sig, "unreduced_statement: ") {
			shown++
		}
		if strings.Contains(sig, "more unreduced statement") {
			sawMore = true
		}
	}
	if shown > 5 {
		t.Errorf("unreduced_statement signals shown = %d, want capped at 5 (had 9)", shown)
	}
	if !sawMore {
		t.Error("want a signal stating how many additional statements were omitted")
	}
}

func TestRouteUnprovenTable_StatementLengthIsBounded(t *testing.T) {
	tb := manyUnreducedTable(1, 5000)
	s := &db.Schema{Tables: []db.Table{tb}}
	_, surf := dbrules.Run(s)
	var found bool
	for _, it := range surf {
		if it.Category != string(surface.CategoryDBTableStructureUnproven) {
			continue
		}
		for _, sig := range it.StructuralSignals {
			if strings.HasPrefix(sig, "unreduced_statement: ") {
				found = true
				if len(sig) > 600 {
					t.Errorf("unreduced_statement signal length = %d, want a bounded length (was 5000+ chars unbounded)", len(sig))
				}
				if !strings.Contains(sig, "truncated") {
					t.Error("want a truncation marker on an over-length statement")
				}
			}
		}
	}
	if !found {
		t.Fatal("no unreduced_statement signal found")
	}
}
