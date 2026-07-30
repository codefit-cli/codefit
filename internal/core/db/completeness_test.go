package db_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
)

// D1 — the per-table completeness carrier (design §1), mirroring db.Body's
// {Text,Complete,Note} honesty idiom at TABLE granularity. Complete's zero
// value is false (fail-closed, D1b) — a table nobody explicitly proved
// complete reads as unproven, never as trustworthy by accident.

func TestTable_ZeroValue_IsIncomplete(t *testing.T) {
	var tb db.Table
	if tb.Complete {
		t.Error("Table{} zero value must have Complete=false (fail-closed polarity, design §1-D1b)")
	}
	if tb.StructureProven() {
		t.Error("StructureProven() must be false on the zero value")
	}
}

func TestTable_MarkUnproven_SetsIncompleteAndRecordsRawText(t *testing.T) {
	tb := db.Table{Name: "t", Complete: true}
	pos := db.Pos{File: "x.sql", Line: 12}
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE t OWNER TO x;", pos)

	if tb.Complete {
		t.Error("MarkUnproven must set Complete=false")
	}
	if tb.StructureProven() {
		t.Error("StructureProven() must be false after MarkUnproven")
	}
	if len(tb.Unreduced) != 1 {
		t.Fatalf("Unreduced = %d entries, want 1", len(tb.Unreduced))
	}
	if tb.Unreduced[0].Text != "ALTER TABLE t OWNER TO x;" {
		t.Errorf("Unreduced[0].Text = %q, want the verbatim statement", tb.Unreduced[0].Text)
	}
	if tb.Unreduced[0].Pos != pos {
		t.Errorf("Unreduced[0].Pos = %+v, want %+v", tb.Unreduced[0].Pos, pos)
	}
	if !strings.Contains(tb.Note, db.ReasonUnreducedTableStatement) {
		t.Errorf("Note = %q, want it to contain the reason %q", tb.Note, db.ReasonUnreducedTableStatement)
	}
}

// Deduplicated Note (spec "Domain: Table Completeness Carrier" — "Deduplicated
// Note"): two drops with the SAME reason yield exactly one Note entry for that
// reason, but each drop still appends its own Unreduced statement (both raw
// statements are kept — only the reason prose is deduplicated).
func TestTable_MarkUnproven_DedupesNoteByReason_KeepsEveryUnreducedStatement(t *testing.T) {
	var tb db.Table
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "stmt one", db.Pos{File: "x.sql", Line: 1})
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "stmt two", db.Pos{File: "x.sql", Line: 2})

	if n := strings.Count(tb.Note, db.ReasonUnreducedTableStatement); n != 1 {
		t.Errorf("Note contains the reason %d times, want exactly 1 (deduplicated): %q", n, tb.Note)
	}
	if len(tb.Unreduced) != 2 {
		t.Fatalf("Unreduced = %d entries, want 2 (one per drop, even though the reason dedupes)", len(tb.Unreduced))
	}
}

// A different reason appends a SECOND entry — dedup is per-reason, not global.
func TestTable_MarkUnproven_TwoDistinctReasons_BothRecorded(t *testing.T) {
	var tb db.Table
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "stmt one", db.Pos{File: "x.sql", Line: 1})
	tb.MarkUnproven(db.ReasonMalformedTableBody, "stmt two", db.Pos{File: "x.sql", Line: 2})

	if !strings.Contains(tb.Note, db.ReasonUnreducedTableStatement) {
		t.Errorf("Note = %q, want it to still contain the first reason", tb.Note)
	}
	if !strings.Contains(tb.Note, db.ReasonMalformedTableBody) {
		t.Errorf("Note = %q, want it to also contain the second, distinct reason", tb.Note)
	}
}

func TestTable_StructureProven_TrueWhenComplete(t *testing.T) {
	tb := db.Table{Name: "t", Complete: true}
	if !tb.StructureProven() {
		t.Error("StructureProven() must be true when Complete=true")
	}
}
