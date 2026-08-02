package sqlddl_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Run-on statement separation (ADR 0041). Two CREATE TABLE statements written
// with no ';' and no batch separator between them are valid T-SQL, and used to
// lose every statement after the first one SILENTLY: tables=1, the first table
// still StructureProven, nothing in Schema.Unreduced, an empty completeness
// note — blindness with no trace, the one outcome ADR 0034 exists to prevent.
//
// Every test here drives the REAL parser over either the authored fixture or
// verbatim source bytes; none constructs a db.Table by hand.

func parseSQL(t *testing.T, dialect sqlddl.Dialect, src string) *db.Schema {
	t.Helper()
	p := sqlddl.New(sqlddl.WithDialect(dialect))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "inline.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return content
}

func sortedTableNames(s *db.Schema) []string {
	out := append([]string(nil), tableNames(s)...)
	sort.Strings(out)
	return out
}

func tableByName(s *db.Schema, name string) *db.Table {
	for i := range s.Tables {
		if s.Tables[i].Name == name {
			return &s.Tables[i]
		}
	}
	return nil
}

// TestSQLDDL_RunOn_RecoversEveryStatementAfterTheFirst is the recovery lock.
// Expectations are transcribed from the FIXTURE's own DDL, not from observed
// output.
func TestSQLDDL_RunOn_RecoversEveryStatementAfterTheFirst(t *testing.T) {
	s := parseFixture(t, filepath.Join("tsql", "constructed_runon_no_delimiter.sql"),
		sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))

	want := []string{"Dim_Car", "Dim_Price_Table", "Dim_Renting_Point", "Fact_Reservation"}
	if got := sortedTableNames(s); !equalStrings(got, want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}

	cases := []struct {
		table string
		cols  []string
		pk    []string
	}{
		{"Dim_Price_Table", []string{"Type_SID", "_Type"}, []string{"Type_SID"}},
		{"Dim_Car", []string{"Car_SID", "License_Plate", "Brand", "Type_sid", "Prod_Year"}, []string{"Car_SID"}},
		{"Dim_Renting_Point", []string{"Point_SID", "City"}, []string{"Point_SID"}},
		{"Fact_Reservation", []string{"Reservation_SID", "Car_SID", "Point_SID", "Total_Price"}, []string{"Reservation_SID"}},
	}
	for _, c := range cases {
		tb := tableByName(s, c.table)
		if tb == nil {
			t.Fatalf("table %s missing", c.table)
		}
		if got := columnNames(*tb); !equalStrings(got, c.cols) {
			t.Errorf("%s columns = %v, want %v", c.table, got, c.cols)
		}
		if got := tb.PrimaryKey; !equalStrings(got, c.pk) {
			t.Errorf("%s primary key = %v, want %v", c.table, got, c.pk)
		}
		// The host statement was read from a balanced paren and cut at a
		// keyword that cannot belong to it: nothing about it is unproven.
		if !tb.StructureProven() {
			t.Errorf("%s StructureProven = false (note %q), want true — splitting a "+
				"run-on must not demote a table whose own body was read in full", c.table, tb.Note)
		}
	}

	// The FKs declared inline in the RECOVERED statements must land on the
	// recovered tables, never on the host.
	car := tableByName(s, "Dim_Car")
	if len(car.ForeignKeys) != 1 || car.ForeignKeys[0].RefTable != "Dim_Price_Table" {
		t.Errorf("Dim_Car foreign keys = %+v, want one to Dim_Price_Table", car.ForeignKeys)
	}
	fact := tableByName(s, "Fact_Reservation")
	if len(fact.ForeignKeys) != 2 {
		t.Errorf("Fact_Reservation foreign keys = %+v, want 2", fact.ForeignKeys)
	}
	if price := tableByName(s, "Dim_Price_Table"); len(price.ForeignKeys) != 0 {
		t.Errorf("Dim_Price_Table foreign keys = %+v, want none — the HOST statement "+
			"must not absorb a recovered statement's constraints", price.ForeignKeys)
	}
	if len(s.Unreduced) != 0 {
		t.Errorf("Schema.Unreduced = %+v, want empty — every residual here IS reducible", s.Unreduced)
	}
}

// TestSQLDDL_RunOn_LineNumbersFollowTheSource locks that a recovered statement
// reports ITS OWN line, not the host's — an agent reading the routed surface
// item has to be able to find it.
func TestSQLDDL_RunOn_LineNumbersFollowTheSource(t *testing.T) {
	s := parseFixture(t, filepath.Join("tsql", "constructed_runon_no_delimiter.sql"),
		sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	// Transcribed from the fixture: the four CREATE TABLE keywords sit on
	// lines 18, 22, 30 and 34.
	want := map[string]int{
		"Dim_Price_Table":   18,
		"Dim_Car":           22,
		"Dim_Renting_Point": 30,
		"Fact_Reservation":  34,
	}
	for name, line := range want {
		tb := tableByName(s, name)
		if tb == nil {
			t.Fatalf("table %s missing", name)
		}
		if tb.Pos.Line != line {
			t.Errorf("%s Pos.Line = %d, want %d", name, tb.Pos.Line, line)
		}
	}
}

// TestSQLDDL_RunOn_NeverCutsInsideAStringIdentifierOrParens is the FABRICATION
// guard. A boundary rule that guesses can invent a table that the DDL never
// declares — worse than the blindness it replaces. Each source below contains
// the trigger keyword in a position where it is NOT a statement head.
func TestSQLDDL_RunOn_NeverCutsInsideAStringIdentifierOrParens(t *testing.T) {
	cases := []struct {
		name    string
		dialect sqlddl.Dialect
		src     string
		wantCol []string
	}{
		{
			// MySQL table COMMENT: the keyword lives inside a string literal.
			name:    "keyword inside a string literal",
			dialect: sqlddl.MySQL(),
			src: "CREATE TABLE widget (\n" +
				"  id INT NOT NULL PRIMARY KEY,\n" +
				"  label VARCHAR(40)\n" +
				") ENGINE=InnoDB COMMENT='create table ghost (gid INT)';\n",
			wantCol: []string{"id", "label"},
		},
		{
			// T-SQL filegroup named with a delimited identifier.
			name:    "keyword inside a quoted identifier",
			dialect: sqlddl.SQLServer(),
			src: "CREATE TABLE widget (\n" +
				"  id INT NOT NULL PRIMARY KEY,\n" +
				"  label VARCHAR(40)\n" +
				") ON [create table ghost (gid INT)]\n",
			wantCol: []string{"id", "label"},
		},
		{
			// Paren depth. This one is ADVERSARIAL rather than transcribed:
			// no legal PostgreSQL/MySQL/T-SQL tail clause puts a bare CREATE
			// inside a paren group (the near-misses — toast.create_table,
			// DROP_EXISTING, AUTO_CREATE — are all defeated by the word
			// boundary instead, and the "keyword is only a substring" case
			// below covers that). It is kept because the depth guard is the
			// one that would silently stop protecting anything, and this input
			// still travels the REAL tokenizer and reducer.
			name:    "keyword nested in parentheses",
			dialect: sqlddl.Postgres(),
			src: "CREATE TABLE widget (\n" +
				"  id INT NOT NULL PRIMARY KEY,\n" +
				"  label TEXT\n" +
				") WITH (autovacuum_enabled = off, note = create table ghost (gid INT));\n",
			wantCol: []string{"id", "label"},
		},
		{
			// Word boundary: the keyword is a PREFIX of a longer word.
			name:    "keyword is only a substring of a longer word",
			dialect: sqlddl.SQLServer(),
			src: "CREATE TABLE widget (\n" +
				"  id INT NOT NULL PRIMARY KEY,\n" +
				"  label VARCHAR(40)\n" +
				") ON CREATEDFILEGROUP\n",
			wantCol: []string{"id", "label"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := parseSQL(t, c.dialect, c.src)
			if got := sortedTableNames(s); !equalStrings(got, []string{"widget"}) {
				t.Fatalf("tables = %v, want exactly [widget] — a fabricated table is "+
					"strictly worse than the silent loss this rule replaces", got)
			}
			tb := tableByName(s, "widget")
			if got := columnNames(*tb); !equalStrings(got, c.wantCol) {
				t.Errorf("widget columns = %v, want %v", got, c.wantCol)
			}
			if !tb.StructureProven() {
				t.Errorf("widget StructureProven = false (note %q), want true", tb.Note)
			}
			// A cut that lands inside a string, an identifier or a paren group
			// does not always FABRICATE a table — it can also produce a
			// fragment no dispatch branch reduces, which then lands on the
			// abstention floor. That is still a wrong cut, and asserting the
			// floor is EMPTY is what catches it: without this line the
			// identifier case passes even with the identifier guard removed
			// (measured).
			if len(s.Unreduced) != 0 {
				t.Errorf("Schema.Unreduced = %+v, want empty — nothing here is a run-on, "+
					"so nothing may be cut off the statement at all", s.Unreduced)
			}
		})
	}
}

// TestSQLDDL_RunOn_UnrecognizedResidualIsDeclaredNeverSilent is the honesty
// floor: when the residual statement IS detected but the reducer's dispatch has
// no branch for it, it must reach Schema.Unreduced verbatim rather than vanish.
func TestSQLDDL_RunOn_UnrecognizedResidualIsDeclaredNeverSilent(t *testing.T) {
	src := "CREATE TABLE widget (\n" +
		"  id INT NOT NULL PRIMARY KEY\n" +
		")\n" +
		"CREATE TYPE mood AS ENUM ('sad', 'ok')\n"
	s := parseSQL(t, sqlddl.Postgres(), src)

	if got := sortedTableNames(s); !equalStrings(got, []string{"widget"}) {
		t.Fatalf("tables = %v, want exactly [widget]", got)
	}
	if len(s.Unreduced) != 1 {
		t.Fatalf("Schema.Unreduced = %+v, want exactly 1 entry for the CREATE TYPE residual", s.Unreduced)
	}
	if got := s.Unreduced[0].Text; !strings.HasPrefix(got, "CREATE TYPE mood") {
		t.Errorf("Schema.Unreduced[0].Text = %q, want the verbatim residual statement", got)
	}
	if got := s.Unreduced[0].Pos.Line; got != 4 {
		t.Errorf("Schema.Unreduced[0].Pos.Line = %d, want 4", got)
	}
	// The host is genuinely readable: an unreducible NEIGHBOUR is not a
	// reason to demote it (ADR 0034 §2.4's false-demotion trap).
	if tb := tableByName(s, "widget"); !tb.StructureProven() {
		t.Errorf("widget StructureProven = false (note %q), want true", tb.Note)
	}
}

// TestSQLDDL_RunOn_HostNeverAbsorbsTheResidualsTailClauses is the second
// fabrication guard, on the other side of the cut. A CREATE TABLE's TAIL is
// read for partitioning, so a host that keeps the residual's text attached
// would read the RESIDUAL's PARTITION BY clause and report it as its own —
// inventing partitioning on a table that declares none. Truncating the host at
// the boundary is what prevents that, and this is what locks it.
func TestSQLDDL_RunOn_HostNeverAbsorbsTheResidualsTailClauses(t *testing.T) {
	src := "CREATE TABLE plain (id INT NOT NULL PRIMARY KEY)\n" +
		"CREATE TABLE part (id INT, ts DATE) PARTITION BY RANGE (ts);\n"
	s := parseSQL(t, sqlddl.Postgres(), src)

	if got := sortedTableNames(s); !equalStrings(got, []string{"part", "plain"}) {
		t.Fatalf("tables = %v, want [part plain]", got)
	}
	plain := tableByName(s, "plain")
	if p := plain.Partitioning; p.Declaration != "" || p.Strategy != "" || len(p.Key) != 0 || p.Of != "" {
		t.Errorf("plain.Partitioning = %+v, want the zero value — the PARTITION BY clause "+
			"belongs to the NEXT statement, and reading it here would fabricate", plain.Partitioning)
	}
	part := tableByName(s, "part")
	if part.Partitioning.Strategy != "range" || !equalStrings(part.Partitioning.Key, []string{"ts"}) {
		t.Errorf("part.Partitioning = %+v, want strategy range over [ts]", part.Partitioning)
	}
}

// TestSQLDDL_RunOn_AppliesTheResidualAfterTheHost locks the ORDER. This reducer
// is incremental — it replays statements in source order — so a recovered
// residual must land after the statement it was glued to. Applying it first
// would make an ALTER TABLE materialize the table before its own CREATE TABLE,
// which the reducer then treats as a duplicate declaration and drops.
func TestSQLDDL_RunOn_AppliesTheResidualAfterTheHost(t *testing.T) {
	src := "CREATE TABLE widget (id INT NOT NULL PRIMARY KEY)\n" +
		"ALTER TABLE widget ADD label TEXT;\n"
	s := parseSQL(t, sqlddl.Postgres(), src)

	if got := sortedTableNames(s); !equalStrings(got, []string{"widget"}) {
		t.Fatalf("tables = %v, want [widget]", got)
	}
	tb := tableByName(s, "widget")
	if got := columnNames(*tb); !equalStrings(got, []string{"id", "label"}) {
		t.Errorf("widget columns = %v, want [id label]", got)
	}
	if !equalStrings(tb.PrimaryKey, []string{"id"}) {
		t.Errorf("widget primary key = %v, want [id]", tb.PrimaryKey)
	}
	if !tb.StructureProven() {
		t.Errorf("widget StructureProven = false (note %q), want true", tb.Note)
	}
}

// TestSQLDDL_RunOn_DelimitedInputIsUnaffected is the positive control for the
// whole rule: the SAME four tables written with ordinary ';' terminators parse
// identically. If this ever diverges from the run-on fixture, the boundary rule
// is reading something the delimiter path does not.
//
// HONEST STATUS, measured rather than assumed: every mutation tried against
// this change that this test caught (disabling the cut, mis-placing the cut)
// was ALSO caught by TestSQLDDL_RunOn_RecoversEveryStatementAfterTheFirst, and
// no mutation was found that it catches alone. It is therefore redundant
// coverage, not independent coverage — kept because the equivalence it states
// is the rule's whole claim and it fails loudly rather than vacuously, but it
// should not be counted twice. Its one genuinely local guard is the terminator
// count below, which fails if the control input stops being a control.
func TestSQLDDL_RunOn_DelimitedInputIsUnaffected(t *testing.T) {
	runOn := parseFixture(t, filepath.Join("tsql", "constructed_runon_no_delimiter.sql"),
		sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))

	content := readFixture(t, filepath.Join("tsql", "constructed_runon_no_delimiter.sql"))
	// Turn every run-on boundary into an ordinary terminator.
	delimited := strings.ReplaceAll(string(content), "\n\nCREATE TABLE", ";\n\nCREATE TABLE") + ";"
	if got := strings.Count(delimited, ";\n\nCREATE TABLE"); got != 4 {
		t.Fatalf("control setup produced %d statement terminators, want 4 — the control "+
			"is not exercising what it claims", got)
	}
	ref := parseSQL(t, sqlddl.SQLServer(), delimited)

	if !equalStrings(sortedTableNames(runOn), sortedTableNames(ref)) {
		t.Fatalf("run-on tables %v != delimited tables %v", sortedTableNames(runOn), sortedTableNames(ref))
	}
	for _, name := range sortedTableNames(ref) {
		a, b := tableByName(runOn, name), tableByName(ref, name)
		if !equalStrings(columnNames(*a), columnNames(*b)) {
			t.Errorf("%s columns: run-on %v != delimited %v", name, columnNames(*a), columnNames(*b))
		}
		if !equalStrings(a.PrimaryKey, b.PrimaryKey) {
			t.Errorf("%s primary key: run-on %v != delimited %v", name, a.PrimaryKey, b.PrimaryKey)
		}
		if len(a.ForeignKeys) != len(b.ForeignKeys) {
			t.Errorf("%s foreign keys: run-on %d != delimited %d", name, len(a.ForeignKeys), len(b.ForeignKeys))
		}
		if a.StructureProven() != b.StructureProven() {
			t.Errorf("%s StructureProven: run-on %v != delimited %v", name, a.StructureProven(), b.StructureProven())
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
