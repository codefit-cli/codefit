package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
)

// F4 (4R ledger, obs #1282, "the false affirmation survives, path 2"): a
// statement dropped at apply()'s top-level default is recorded nowhere. A
// LATER `CREATE INDEX ... ON x` (or an ALTER TABLE x ...) for a table name
// no CREATE TABLE ever declared then materializes a PHANTOM table via
// getTable — Complete=true (the N1-safe default), zero columns, zero PK —
// and DB-050 affirmed at confidence 1.0 over a table that was NEVER READ.
//
// The fix does NOT touch Complete's default (that is the N1 trap: it would
// mute the whole dimension). It records, at the exact two call sites that
// can materialize a table BEFORE any CREATE TABLE (applyAlterTable,
// applyCreateIndex), that this specific entry was never declared —
// ReasonTableNeverDeclared — via the SAME MarkUnproven contract every other
// drop site already uses, so DB-050 routes instead of affirming.
func TestSQLDDL_PhantomTable_ViaCreateIndex_MarksUnprovenNeverDeclared(t *testing.T) {
	sql := "CREATE INDEX idx_ghost ON ghost_table (id);\n"
	tb := parsePGTableNamed(t, sql, "ghost_table")
	if tb.Complete {
		t.Error("Complete = true, want false — this table has zero genuine structure, no CREATE TABLE ever declared it")
	}
	if !strings.Contains(tb.Note, db.ReasonTableNeverDeclared) {
		t.Errorf("Note = %q, want it to contain ReasonTableNeverDeclared", tb.Note)
	}

	// The invariant F4 actually demands: DB-050 must not affirm over it.
	s := &db.Schema{Tables: []db.Table{tb}}
	fs, _ := dbrules.Run(s)
	for _, f := range fs {
		if f.ID == "DB-050" {
			t.Fatalf("DB-050 affirmed over a table no CREATE TABLE ever declared: %+v", f)
		}
	}
}

func TestSQLDDL_PhantomTable_ViaAlterTable_MarksUnprovenNeverDeclared(t *testing.T) {
	sql := "ALTER TABLE ghost_table ADD COLUMN name text;\n"
	tb := parsePGTableNamed(t, sql, "ghost_table")
	if tb.Complete {
		t.Error("Complete = true, want false — this table has zero genuine structure, no CREATE TABLE ever declared it")
	}
	if !strings.Contains(tb.Note, db.ReasonTableNeverDeclared) {
		t.Errorf("Note = %q, want it to contain ReasonTableNeverDeclared", tb.Note)
	}
}

// Positive control: a table a CREATE TABLE genuinely declares, later
// altered/indexed, is completely UNAFFECTED — Declared-at-creation stays
// Complete=true and DB-050 still affirms a real missing PK on it.
func TestSQLDDL_RealTable_ThenAlteredAndIndexed_StaysComplete(t *testing.T) {
	sql := "CREATE TABLE real_table (id int);\n" +
		"ALTER TABLE real_table ADD COLUMN name text;\n" +
		"CREATE INDEX idx_real ON real_table (name);\n"
	tb := parsePGTableNamed(t, sql, "real_table")
	if !tb.Complete {
		t.Fatalf("Complete = false, want true — a genuinely CREATE TABLE-declared table must be unaffected")
	}
	s := &db.Schema{Tables: []db.Table{tb}}
	fs, _ := dbrules.Run(s)
	var found bool
	for _, f := range fs {
		if f.ID == "DB-050" {
			found = true
		}
	}
	if !found {
		t.Error("DB-050 must still affirm on a genuinely declared table with no PK (positive control)")
	}
}
