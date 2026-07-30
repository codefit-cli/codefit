package dbrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// D4 (design SS4) — the two-way rule gate. DB-050 (an AFFIRMATION) ROUTES to a
// dedicated surface category on an unproven table instead of affirming; every
// other absence-based rule (DB-001, DB-052, and the five DW rules in the
// sibling package) ABSTAINS silently.

func unprovenTable(name string) db.Table {
	tb := db.Table{Name: name, Pos: db.Pos{File: "x.sql", Line: 1}}
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE "+name+" ADD CONSTRAINT ...;", db.Pos{File: "x.sql", Line: 3})
	return tb
}

func TestDB050_UnprovenTable_RoutesToSurface_NeverAffirms(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{unprovenTable("orders")}}
	fs, surf := dbrules.Run(s)

	for _, f := range fs {
		if f.ID == "DB-050" {
			t.Fatalf("DB-050 affirmed on an unproven table: %+v — must route to surface instead", f)
		}
	}
	var found bool
	for _, it := range surf {
		if it.Category == string(surface.CategoryDBTableStructureUnproven) {
			found = true
			if it.File != "x.sql" || it.Line != 1 {
				t.Errorf("routed item File/Line = %s:%d, want the table's CREATE TABLE position x.sql:1", it.File, it.Line)
			}
			if it.StructuralFacts["table_structure_proven_complete"] {
				t.Error("table_structure_proven_complete must be false")
			}
			foundStmt := false
			for _, sig := range it.StructuralSignals {
				if sig == "unreduced_statement: ALTER TABLE orders ADD CONSTRAINT ...;" {
					foundStmt = true
				}
			}
			if !foundStmt {
				t.Errorf("StructuralSignals = %v, want the verbatim unreduced statement", it.StructuralSignals)
			}
		}
	}
	if !found {
		t.Fatal("no db-table-structure-unproven surface item was emitted")
	}
}

func TestDB050_ProvenTable_MissingPK_StillAffirms_PositiveControl(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{Name: "t", Complete: true, Pos: db.Pos{File: "x.sql", Line: 1}}}}
	fs, _ := dbrules.Run(s)
	var found bool
	for _, f := range fs {
		if f.ID == "DB-050" {
			found = true
			if f.Confidence != 1.0 || f.Probabilistic {
				t.Errorf("Confidence=%v Probabilistic=%v, want 1.0/false", f.Confidence, f.Probabilistic)
			}
		}
	}
	if !found {
		t.Fatal("DB-050 must still affirm a genuine missing PK over a PROVEN model (positive control)")
	}
}

func TestDB050_ProvenTable_WithPK_NoFindingNoSurface(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{Name: "t", Complete: true, PrimaryKey: []string{"id"}}}}
	fs, surf := dbrules.Run(s)
	for _, f := range fs {
		if f.ID == "DB-050" {
			t.Errorf("unexpected DB-050 affirmation on a table with a PK: %+v", f)
		}
	}
	for _, it := range surf {
		if it.Category == string(surface.CategoryDBTableStructureUnproven) {
			t.Errorf("unexpected routed item on a proven table with a PK: %+v", it)
		}
	}
}

// DB-001 (FK without covering index) ABSTAINS, per table, on an unproven
// table — a dropped statement might have declared the covering index.
func TestDB001_UnprovenTable_Abstains(t *testing.T) {
	tb := unprovenTable("orders")
	tb.ForeignKeys = []db.ForeignKey{{Pos: db.Pos{File: "x.sql", Line: 2}, Columns: []string{"customer_id"}, RefTable: "customers"}}
	s := &db.Schema{Tables: []db.Table{tb}}
	_, surf := dbrules.Run(s)
	for _, it := range surf {
		if it.Category == string(surface.CategoryDBFKNoIndex) {
			t.Errorf("DB-001 must abstain on an unproven table, got: %+v", it)
		}
	}
}

func TestDB001_ProvenTable_StillFires(t *testing.T) {
	tb := db.Table{Name: "orders", Complete: true}
	tb.ForeignKeys = []db.ForeignKey{{Pos: db.Pos{File: "x.sql", Line: 2}, Columns: []string{"customer_id"}, RefTable: "customers"}}
	s := &db.Schema{Tables: []db.Table{tb}}
	_, surf := dbrules.Run(s)
	var found bool
	for _, it := range surf {
		if it.Category == string(surface.CategoryDBFKNoIndex) {
			found = true
		}
	}
	if !found {
		t.Error("DB-001 must still fire on a PROVEN table with an uncovered FK")
	}
}

// DB-052 (missing audit timestamps) ABSTAINS, per table, on an unproven
// table — a dropped ADD COLUMN might have declared created_at/updated_at.
func TestDB052_UnprovenTable_Abstains(t *testing.T) {
	tb := unprovenTable("orders")
	s := &db.Schema{Tables: []db.Table{tb}}
	_, surf := dbrules.Run(s)
	for _, it := range surf {
		if it.Category == string(surface.CategoryDBNoTimestamps) {
			t.Errorf("DB-052 must abstain on an unproven table, got: %+v", it)
		}
	}
}

func TestDB052_ProvenTable_StillFires(t *testing.T) {
	tb := db.Table{Name: "orders", Complete: true, Pos: db.Pos{File: "x.sql", Line: 1}}
	s := &db.Schema{Tables: []db.Table{tb}}
	_, surf := dbrules.Run(s)
	var found bool
	for _, it := range surf {
		if it.Category == string(surface.CategoryDBNoTimestamps) {
			found = true
		}
	}
	if !found {
		t.Error("DB-052 must still fire on a PROVEN table with no timestamps")
	}
}
