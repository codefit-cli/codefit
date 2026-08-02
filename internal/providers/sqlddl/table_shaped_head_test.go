package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// ADR 0043 — the table-shaped-head floor.
//
// reIndexShapedHead has given the CREATE INDEX family an honest-abstention
// floor since ADR 0034: a CREATE INDEX form no branch reduces is RECOGNIZED as
// index-shaped and recorded, instead of falling through apply()'s default: and
// evaporating. There was no table-shaped equivalent, so a CREATE <anything>
// TABLE head no branch reduced left NO trace of any kind — measured through
// the real Sensor.Audit, an UNLOGGED table alone in a schema produced
// Measured=true, Note="", tables=0, Schema.Unreduced=0, findings=0: the false
// "audited, 0 findings" state over a schema codefit never read.
//
// These tests lock the two dispositions that do not need a new carrier:
// UNLOGGED is ADMITTED to the model, and any other unreduced table-shaped head
// is DECLARED on Schema.Unreduced. The third (session-scoped tables, WITHHELD
// with their own trace) is locked in withheld_table_test.go, because "read and
// deliberately not modeled" is a different fact from "could not read".

// An UNLOGGED table is genuinely persistent storage — PostgreSQL only skips
// the WAL for it — so it belongs in the model like any other table, columns,
// keys and all.
func TestSQLDDL_UnloggedTable_IsModeled(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), "CREATE UNLOGGED TABLE events (id integer PRIMARY KEY, payload text);\n")

	if len(s.Tables) != 1 {
		t.Fatalf("tables = %d, want 1 — an unlogged table is persistent and must be modeled: %+v", len(s.Tables), s.Tables)
	}
	got := s.Tables[0]
	if got.Name != "events" {
		t.Errorf("table name = %q, want %q", got.Name, "events")
	}
	if len(got.Columns) != 2 {
		t.Errorf("columns = %d, want 2 (%+v)", len(got.Columns), got.Columns)
	}
	if want := []string{"id"}; len(got.PrimaryKey) != 1 || got.PrimaryKey[0] != want[0] {
		t.Errorf("PrimaryKey = %v, want %v", got.PrimaryKey, want)
	}
	if !got.StructureProven() {
		t.Errorf("StructureProven() = false, want true — nothing about this statement was dropped (Note=%q)", got.Note)
	}
	if len(s.Unreduced) != 0 {
		t.Errorf("Schema.Unreduced = %+v, want empty — the statement WAS reduced", s.Unreduced)
	}
}

// GROUP-NUMBERING lock. reduceCreateTable reads loc[2] for IF NOT EXISTS and
// loc[4]:loc[5] for the name; the UNLOGGED prefix must therefore be a
// NON-capturing group. A capturing one shifts every later group by two and the
// table is named out of the wrong span.
func TestSQLDDL_UnloggedTable_IfNotExists_NameReadFromTheRightGroup(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), "CREATE UNLOGGED TABLE IF NOT EXISTS events_archive (id integer PRIMARY KEY);\n")

	if len(s.Tables) != 1 {
		t.Fatalf("tables = %d, want 1: %+v", len(s.Tables), s.Tables)
	}
	if got := s.Tables[0].Name; got != "events_archive" {
		t.Errorf("table name = %q, want %q — a capturing UNLOGGED group would shift the name span", got, "events_archive")
	}
	if got := len(s.Tables[0].Columns); got != 1 {
		t.Errorf("columns = %d, want 1", got)
	}
}

// reCreateTablePartitionOf carries the same CREATE TABLE prefix, so it needs
// the same widening: PostgreSQL admits CREATE UNLOGGED TABLE ... PARTITION OF.
// Without it the statement falls past the partition-child branch into the
// generic catcher and the child stops being modeled at all.
func TestSQLDDL_UnloggedPartitionChild_StillDispatchesToThePartitionOfBranch(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(),
		"CREATE TABLE parent (id integer, ts date) PARTITION BY RANGE (ts);\n"+
			"CREATE UNLOGGED TABLE child PARTITION OF parent FOR VALUES FROM ('2024-01-01') TO ('2025-01-01');\n")

	child := tableNamed(t, s, "child")
	if child.Partitioning.Of != "parent" {
		t.Errorf("child.Partitioning.Of = %q, want %q — the partition-child branch did not run", child.Partitioning.Of, "parent")
	}
	if child.StructureProven() {
		t.Errorf("child.StructureProven() = true, want false — a partition child inherits its structure (ADR 0034/0038)")
	}
	if len(s.Unreduced) != 0 {
		t.Errorf("Schema.Unreduced = %+v, want empty — the child WAS dispatched", s.Unreduced)
	}
}

// The catcher's own disposition: a CREATE ... TABLE head no branch reduces is
// DECLARED verbatim on Schema.Unreduced. It fabricates nothing — no table is
// registered, and no name is guessed out of a grammar the reducer does not
// know. The verbatim statement is what carries the name to the agent.
func TestSQLDDL_UnrecognizedTableShapedHead_IsDeclaredNeverSilent(t *testing.T) {
	cases := []struct {
		name    string
		dialect sqlddl.Dialect
		sql     string
	}{
		{"pg foreign table", sqlddl.Postgres(), "CREATE FOREIGN TABLE external_orders (id integer) SERVER remote_srv;\n"},
		{"pg create table as select", sqlddl.Postgres(), "CREATE TABLE summary_ctas AS SELECT 1 AS id;\n"},
		{"pg quoted name outside the identifier class", sqlddl.Postgres(), "CREATE TABLE \"#weird\" (id integer);\n"},
		{"mariadb create or replace table", sqlddl.MySQL(), "CREATE OR REPLACE TABLE t (id int);\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := parseSQL(t, tc.dialect, tc.sql)

			if len(s.Tables) != 0 {
				t.Errorf("tables = %+v, want none — the reducer must not fabricate a table out of a head it cannot reduce", s.Tables)
			}
			if len(s.Unreduced) != 1 {
				t.Fatalf("Schema.Unreduced = %+v, want exactly 1 declared statement", s.Unreduced)
			}
			u := s.Unreduced[0]
			if want := strings.TrimSuffix(strings.TrimSpace(tc.sql), ";"); u.Text != want {
				t.Errorf("Unreduced[0].Text = %q, want the verbatim statement %q", u.Text, want)
			}
			if u.Pos.File != "inline.sql" || u.Pos.Line != 1 {
				t.Errorf("Unreduced[0].Pos = %+v, want inline.sql:1", u.Pos)
			}
		})
	}
}

// ORDER lock, and the constraint that makes the catcher admissible: it runs as
// a FALLBACK, after every real table branch. A catcher placed before them
// swallows the working forms — CREATE TABLE, IF NOT EXISTS, and PARTITION OF
// all match a "create ... table" head too.
func TestSQLDDL_TableShapedHeadCatcher_NeverSwallowsAWorkingForm(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(),
		"CREATE TABLE plain (id integer PRIMARY KEY);\n"+
			"CREATE TABLE IF NOT EXISTS guarded (id integer PRIMARY KEY);\n"+
			"CREATE TABLE parent (id integer, ts date) PARTITION BY RANGE (ts);\n"+
			"CREATE TABLE child PARTITION OF parent DEFAULT;\n"+
			"CREATE UNLOGGED TABLE unlogged_one (id integer PRIMARY KEY);\n")

	for _, name := range []string{"plain", "guarded", "parent", "child", "unlogged_one"} {
		tbl := tableNamed(t, s, name)
		if name != "child" && len(tbl.Columns) == 0 {
			t.Errorf("table %q has no columns — the catcher swallowed a form a real branch reduces", name)
		}
	}
	if len(s.Tables) != 5 {
		t.Errorf("tables = %d, want 5: %+v", len(s.Tables), tableNames(s))
	}
	if len(s.Unreduced) != 0 {
		t.Errorf("Schema.Unreduced = %+v, want empty — every statement here has a real branch", s.Unreduced)
	}
}

// SCOPE boundary, declared rather than discovered later. The catcher's
// modifier window is at most TWO words between CREATE and TABLE, which is what
// keeps it from claiming statements that are not table declarations at all: a
// T-SQL user-defined TABLE TYPE, a TABLESPACE, and the SQL-standard CREATE
// SCHEMA element list (three words: "schema s create") each stay out of the
// declared subset and record nothing, exactly as before this slice.
//
// CREATE SCHEMA ... CREATE TABLE ... is a real, still-open gap (ADR 0041
// records it); it is NOT closed here, and this test says so rather than
// letting a reader assume the catcher covers it.
func TestSQLDDL_TableShapedHeadCatcher_DoesNotClaimANonTableStatement(t *testing.T) {
	cases := []struct {
		name    string
		dialect sqlddl.Dialect
		sql     string
	}{
		{"tsql table type", sqlddl.SQLServer(), "CREATE TYPE IdList AS TABLE (id int);\n"},
		{"pg tablespace", sqlddl.Postgres(), "CREATE TABLESPACE fastspace LOCATION '/ssd';\n"},
		{"pg schema element list", sqlddl.Postgres(), "CREATE SCHEMA s CREATE TABLE a (id integer);\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := parseSQL(t, tc.dialect, tc.sql)
			if len(s.Tables) != 0 {
				t.Errorf("tables = %+v, want none", s.Tables)
			}
			if len(s.Unreduced) != 0 {
				t.Errorf("Schema.Unreduced = %+v, want empty — this statement is not a CREATE TABLE head and must stay a declared skip", s.Unreduced)
			}
		})
	}
}

// The authored fixture, end to end. Its CONTENT is verified first (a fixture
// is verified by what it contains, not by its name), then every disposition it
// exercises is asserted at once.
func TestSQLDDL_TableShapedHeads_Fixture_PostgreSQL(t *testing.T) {
	const path = "pg_constructed_table_shaped_heads.sql"
	content := readFixture(t, path)
	text := string(content)
	for _, want := range []string{
		"CREATE UNLOGGED TABLE events (",
		"CREATE UNLOGGED TABLE IF NOT EXISTS events_archive (",
		"CREATE TEMP TABLE scratch_temp",
		"CREATE TEMPORARY TABLE scratch_temporary",
		"CREATE GLOBAL TEMPORARY TABLE scratch_global",
		"CREATE LOCAL TEMPORARY TABLE scratch_local",
		"CREATE TEMPORARY TABLE IF NOT EXISTS scratch_ine",
		"CREATE FOREIGN TABLE external_orders",
		"CREATE TABLE summary_ctas AS SELECT",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("fixture %s does not contain %q — the shape this test claims to exercise is missing", path, want)
		}
	}

	s := parseSQL(t, sqlddl.Postgres(), text)

	// MODELED: the control plus the two unlogged tables, and nothing else.
	if got, want := tableNames(s), []string{"keeper", "events", "events_archive"}; !equalStrings(got, want) {
		t.Errorf("tables = %v, want %v", got, want)
	}
	// DECLARED: the two heads with no branch.
	if len(s.Unreduced) != 2 {
		t.Errorf("Schema.Unreduced = %d entries, want 2 (FOREIGN TABLE + CTAS): %+v", len(s.Unreduced), s.Unreduced)
	}
}
