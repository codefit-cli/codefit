package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// ADR 0043, the third disposition — WITHHELD.
//
// A temporary table is read correctly and deliberately left out of the model.
// That is NOT the same fact as "the parser could not reduce this", and the two
// must not share a carrier: Schema.Unreduced means codefit is blind, and saying
// so about a statement it read perfectly would be a lie in the other direction.
// Admitting the table instead would be worse still — DB-050 would affirm
// "table without a primary key", at confidence 1.0, over session scratch space.

// Every keyword spelling the three dialects give a session-scoped table, read
// through the real parser. The NAME is asserted because the withholding branch
// matches a grammar whose name position it knows, unlike the table-shaped-head
// floor, which declares without one.
func TestSQLDDL_SessionScopedTables_AreWithheldNotModeled(t *testing.T) {
	// wantText is the statement as the TOKENIZER emits it, which is what every
	// Text field in this model carries: split() canonicalizes every dialect's
	// identifier quoting to ANSI double quotes before the reducer sees a
	// statement, so MySQL's backticks arrive as "..." — transcribed from the
	// source DDL through that one documented transform, not from the reducer.
	cases := []struct {
		label    string
		dialect  sqlddl.Dialect
		sql      string
		name     string
		wantText string
	}{
		{"pg temp", sqlddl.Postgres(), "CREATE TEMP TABLE scratch (id integer);\n", "scratch", "CREATE TEMP TABLE scratch (id integer)"},
		{"pg temporary", sqlddl.Postgres(), "CREATE TEMPORARY TABLE scratch (id integer);\n", "scratch", "CREATE TEMPORARY TABLE scratch (id integer)"},
		{"pg global temporary", sqlddl.Postgres(), "CREATE GLOBAL TEMPORARY TABLE scratch (id integer);\n", "scratch", "CREATE GLOBAL TEMPORARY TABLE scratch (id integer)"},
		{"pg local temporary", sqlddl.Postgres(), "CREATE LOCAL TEMPORARY TABLE scratch (id integer);\n", "scratch", "CREATE LOCAL TEMPORARY TABLE scratch (id integer)"},
		{"pg temporary if not exists", sqlddl.Postgres(), "CREATE TEMPORARY TABLE IF NOT EXISTS scratch (id integer);\n", "scratch", "CREATE TEMPORARY TABLE IF NOT EXISTS scratch (id integer)"},
		{"mysql temporary", sqlddl.MySQL(), "CREATE TEMPORARY TABLE `scratch` (id int);\n", "scratch", `CREATE TEMPORARY TABLE "scratch" (id int)`},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			s := parseSQL(t, tc.dialect, tc.sql)

			if len(s.Tables) != 0 {
				t.Errorf("tables = %+v, want none — a session-scoped table is not part of the persistent schema", s.Tables)
			}
			if len(s.Unreduced) != 0 {
				t.Errorf("Schema.Unreduced = %+v, want empty — this statement was READ, not dropped; reporting it as unreduced would misdescribe a scoping decision as a parser failure", s.Unreduced)
			}
			if len(s.Withheld) != 1 {
				t.Fatalf("Schema.Withheld = %+v, want exactly 1", s.Withheld)
			}
			w := s.Withheld[0]
			if w.Reason != db.ReasonSessionScopedTable {
				t.Errorf("Withheld[0].Reason = %q, want %q", w.Reason, db.ReasonSessionScopedTable)
			}
			if w.Name != tc.name {
				t.Errorf("Withheld[0].Name = %q, want %q", w.Name, tc.name)
			}
			if w.Text != tc.wantText {
				t.Errorf("Withheld[0].Text = %q, want the source statement %q", w.Text, tc.wantText)
			}
			if w.Pos.File != "inline.sql" || w.Pos.Line != 1 {
				t.Errorf("Withheld[0].Pos = %+v, want inline.sql:1", w.Pos)
			}
		})
	}
}

// T-SQL has no TEMPORARY keyword: it marks a temporary table by a '#' (local)
// or '##' (global) NAME prefix, so the keyword widening does nothing for it and
// the recognition is a separate one, gated on a per-dialect DATUM.
//
// The datum is what keeps the recognition honest in the other direction: '#' is
// not a temporary marker in PostgreSQL or MySQL, and a PostgreSQL table
// legitimately named "#weird" must NOT be read as session-scoped. It lands on
// the table-shaped-head floor instead, which is where an unreadable name
// belongs.
func TestSQLDDL_TSQL_HashPrefixedTables_AreWithheld_AndOnlyInThatDialect(t *testing.T) {
	for _, tc := range []struct{ label, sql, name string }{
		{"local", "CREATE TABLE #tmpHoliday (DateId int);\n", "#tmpHoliday"},
		{"global", "CREATE TABLE ##GlobalScratch (Id int);\n", "##GlobalScratch"},
		{"bracket-quoted local", "CREATE TABLE [#tmpHoliday] (DateId int);\n", "#tmpHoliday"},
	} {
		t.Run("tsql/"+tc.label, func(t *testing.T) {
			s := parseSQL(t, sqlddl.SQLServer(), tc.sql)
			if len(s.Tables) != 0 {
				t.Errorf("tables = %+v, want none", s.Tables)
			}
			if len(s.Withheld) != 1 {
				t.Fatalf("Schema.Withheld = %+v, want exactly 1", s.Withheld)
			}
			if got := s.Withheld[0].Reason; got != db.ReasonSessionScopedTable {
				t.Errorf("Withheld[0].Reason = %q, want %q", got, db.ReasonSessionScopedTable)
			}
			if got := s.Withheld[0].Name; got != tc.name {
				t.Errorf("Withheld[0].Name = %q, want %q", got, tc.name)
			}
		})
	}

	// The datum lock: the SAME text under PostgreSQL is not a temporary table.
	s := parseSQL(t, sqlddl.Postgres(), "CREATE TABLE \"#weird\" (id integer);\n")
	if len(s.Withheld) != 0 {
		t.Errorf("PostgreSQL Schema.Withheld = %+v, want empty — '#' is a T-SQL temporary marker, not a PostgreSQL one", s.Withheld)
	}
	if len(s.Unreduced) != 1 {
		t.Errorf("PostgreSQL Schema.Unreduced = %+v, want 1 — the name is outside this reducer's identifier class, which is a READ failure, not a scoping decision", s.Unreduced)
	}
}

// ADR 0041's protected case, asserted over REAL vendored DDL rather than
// assumed: a CREATE TEMPORARY TABLE nested inside a routine body is legitimate
// body CONTENT, not a statement of its own. This is the one case with real
// prevalence in this repo — both fixtures below genuinely contain one, which is
// why the fixture CONTENT is verified first.
//
// WHAT THIS LOCKS, stated from the mutation rather than from the design: TWO
// guards stand between a routine body and this branch, and the test is
// insensitive to either one ALONE.
//
//   - Removing the ^ anchor from reSessionScopedTable does NOT break it. The
//     routine branch (reRoutine/reTrigger) claims the statement first, so an
//     unanchored regex is never consulted. The anchor is belt-and-braces here,
//     not the load-bearing guard.
//   - Removing the ^ anchor AND hoisting the withholding branch above the
//     routine branch DOES break it, on both fixtures.
//
// So what is locked is the CONJUNCTION — dispatch order plus anchoring — and a
// future refactor that keeps one while dropping the other is exactly what this
// test is here to stop.
func TestSQLDDL_NestedTemporaryTableInARoutineBody_IsNeverWithheld(t *testing.T) {
	for _, tc := range []struct {
		path    string
		dialect sqlddl.Dialect
	}{
		{"pagila_real_objects.sql", sqlddl.Postgres()},
		{"mysql/sakila_real_objects.sql", sqlddl.MySQL()},
	} {
		t.Run(tc.path, func(t *testing.T) {
			text := string(readFixture(t, tc.path))
			if !strings.Contains(text, "CREATE TEMPORARY TABLE") {
				t.Fatalf("fixture %s contains no CREATE TEMPORARY TABLE — this test would pass vacuously", tc.path)
			}

			s := parseSQL(t, tc.dialect, text)
			if len(s.Withheld) != 0 {
				t.Errorf("Schema.Withheld = %+v, want empty — the temporary table here is inside a routine body, not a top-level statement", s.Withheld)
			}
			// Positive control on the same parse: the body that CONTAINS it is
			// still captured, so the assertion above is not passing because the
			// whole routine went missing.
			found := false
			for _, p := range s.Procedures {
				if strings.Contains(p.Body.Text, "CREATE TEMPORARY TABLE") {
					found = true
				}
			}
			if !found {
				t.Errorf("no captured procedure body contains the nested CREATE TEMPORARY TABLE — Withheld being empty proves nothing here")
			}
		})
	}
}

// The authored T-SQL fixture, end to end, including the ordinary table that
// must keep reducing beside the two withheld ones.
func TestSQLDDL_TempTableNames_Fixture_TSQL(t *testing.T) {
	const path = "tsql/constructed_temp_table_names.sql"
	text := string(readFixture(t, path))
	for _, want := range []string{"CREATE TABLE #tmpHoliday", "CREATE TABLE ##GlobalScratch", "CREATE TABLE dbo.Keeper"} {
		if !strings.Contains(text, want) {
			t.Fatalf("fixture %s does not contain %q", path, want)
		}
	}

	s := parseSQL(t, sqlddl.SQLServer(), text)
	if got, want := tableNames(s), []string{"Keeper"}; !equalStrings(got, want) {
		t.Errorf("tables = %v, want %v", got, want)
	}
	if len(s.Withheld) != 2 {
		t.Fatalf("Schema.Withheld = %+v, want 2", s.Withheld)
	}
	if got, want := s.Withheld[0].Name, "#tmpHoliday"; got != want {
		t.Errorf("Withheld[0].Name = %q, want %q", got, want)
	}
	if got, want := s.Withheld[1].Name, "##GlobalScratch"; got != want {
		t.Errorf("Withheld[1].Name = %q, want %q", got, want)
	}
	if len(s.Unreduced) != 0 {
		t.Errorf("Schema.Unreduced = %+v, want empty", s.Unreduced)
	}
}

// The PostgreSQL fixture's withheld half, paired with the modeled half locked
// in table_shaped_head_test.go.
func TestSQLDDL_TableShapedHeads_Fixture_WithheldHalf(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), string(readFixture(t, "pg_constructed_table_shaped_heads.sql")))

	var names []string
	for _, w := range s.Withheld {
		if w.Reason != db.ReasonSessionScopedTable {
			t.Errorf("Withheld %q carries reason %q, want %q", w.Name, w.Reason, db.ReasonSessionScopedTable)
		}
		names = append(names, w.Name)
	}
	want := []string{"scratch_temp", "scratch_temporary", "scratch_global", "scratch_local", "scratch_ine"}
	if !equalStrings(names, want) {
		t.Errorf("withheld names = %v, want %v", names, want)
	}
}
