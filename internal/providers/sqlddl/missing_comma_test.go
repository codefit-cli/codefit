package sqlddl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// dialectsUnderTest is every dialect this reducer supports. The missing-comma
// boundary rule is dialect-free CODE (ADR 0015: the rule reasons over the
// neutral model, the dialect only supplies data), and the grammar fact it rests
// on — an INLINE PRIMARY KEY/UNIQUE takes no bare parenthesized column list,
// and FOREIGN KEY (…) is never a column constraint — holds in all three. Every
// case below therefore runs under all three, so a rule that quietly became
// dialect-dependent fails here.
func dialectsUnderTest() []struct {
	name    string
	dialect sqlddl.Dialect
} {
	return []struct {
		name    string
		dialect sqlddl.Dialect
	}{
		{"postgres", sqlddl.Postgres()},
		{"mysql", sqlddl.MySQL()},
		{"sqlserver", sqlddl.SQLServer()},
	}
}

func columnByName(t *db.Table, name string) *db.Column {
	for i := range t.Columns {
		if t.Columns[i].Name == name {
			return &t.Columns[i]
		}
	}
	return nil
}

func indexStrings(t *db.Table) []string {
	out := make([]string, 0, len(t.Indexes))
	for _, ix := range t.Indexes {
		s := ""
		for i, c := range ix.Columns {
			if i > 0 {
				s += ","
			}
			s += c
		}
		if ix.Unique {
			s += " UNIQUE"
		}
		out = append(out, s)
	}
	return out
}

func fkStrings(t *db.Table) []string {
	out := make([]string, 0, len(t.ForeignKeys))
	for _, fk := range t.ForeignKeys {
		s := ""
		for i, c := range fk.Columns {
			if i > 0 {
				s += ","
			}
			s += c
		}
		s += "->" + fk.RefTable
		out = append(out, s)
	}
	return out
}

// TestSQLDDL_MissingCommaBeforeTableLevelKey_RecoversTheDeclaredConstraint is
// the ROOT lock of this change. A CREATE TABLE body item missing its separating
// comma before a TABLE-LEVEL key constraint used to be glued onto the preceding
// column definition, so the constraint was read as that column's INLINE
// constraint — inventing a single-column key/index/foreign key the DDL never
// declares, on a table that still reported StructureProven()==true (a
// FABRICATION of the ADR 0034 §2.6 class, which the completeness contract
// structurally cannot see because nothing is dropped).
//
// The boundary is DECIDABLE, which is why recovery rather than abstention is
// admissible here: in PostgreSQL, MySQL and T-SQL alike an INLINE `PRIMARY KEY`
// or `UNIQUE` column constraint takes NO bare parenthesized column list (every
// parenthesised continuation is keyword-introduced — `WITH (…)`, `INCLUDE (…)`,
// `ON scheme (…)`), and `FOREIGN KEY (…)` is not a column-constraint form at
// all. So `<column definition> <HEAD> (` has exactly one legal reading: a
// missing comma.
//
// Each case asserts FOUR things, because three of them were wrong before:
// the declared constraint is recovered on its OWN columns; the preceding
// column survives with its real RawType (the FOREIGN KEY cases used to corrupt
// it to "INT\nFOREIGN KEY(a)"); no phantom key/index is left behind; and the
// table stays proven, because nothing was dropped.
func TestSQLDDL_MissingCommaBeforeTableLevelKey_RecoversTheDeclaredConstraint(t *testing.T) {
	cases := []struct {
		name    string
		tail    string
		wantPK  []string
		wantIdx []string
		wantFK  []string
	}{
		{
			name:   "PRIMARY KEY",
			tail:   "PRIMARY KEY(a, b)",
			wantPK: []string{"a", "b"},
		},
		{
			name:   "PRIMARY KEY, space before the list",
			tail:   "PRIMARY KEY (a, b)",
			wantPK: []string{"a", "b"},
		},
		{
			name:   "PRIMARY KEY CLUSTERED (T-SQL spelling)",
			tail:   "PRIMARY KEY CLUSTERED (a, b)",
			wantPK: []string{"a", "b"},
		},
		{
			name:   "CONSTRAINT <name> PRIMARY KEY",
			tail:   "CONSTRAINT pk_t PRIMARY KEY (a, b)",
			wantPK: []string{"a", "b"},
		},
		{
			name:    "UNIQUE",
			tail:    "UNIQUE (a, b)",
			wantIdx: []string{"a,b UNIQUE"},
		},
		{
			name:    "UNIQUE, no space before the list",
			tail:    "UNIQUE(a, b)",
			wantIdx: []string{"a,b UNIQUE"},
		},
		{
			name:    "UNIQUE KEY <name> (MySQL spelling)",
			tail:    "UNIQUE KEY u_ab (a, b)",
			wantIdx: []string{"a,b UNIQUE"},
		},
		{
			name:    "CONSTRAINT <name> UNIQUE",
			tail:    "CONSTRAINT u_t UNIQUE (a, b)",
			wantIdx: []string{"a,b UNIQUE"},
		},
		{
			name:   "FOREIGN KEY",
			tail:   "FOREIGN KEY (a) REFERENCES o(x)",
			wantFK: []string{"a->o"},
		},
		{
			name:   "FOREIGN KEY <index name> (MySQL spelling)",
			tail:   "FOREIGN KEY fk_a (a) REFERENCES o(x)",
			wantFK: []string{"a->o"},
		},
		{
			name:   "CONSTRAINT <name> FOREIGN KEY",
			tail:   "CONSTRAINT fk_t FOREIGN KEY (a) REFERENCES o(x)",
			wantFK: []string{"a->o"},
		},
	}
	for _, d := range dialectsUnderTest() {
		for _, c := range cases {
			t.Run(d.name+"/"+c.name, func(t *testing.T) {
				// NOTE the missing comma after "b INT" — that, and nothing
				// else, is what this test is about.
				src := "CREATE TABLE t(\na INT,\nb INT\n" + c.tail + "\n);\n"
				s := parseSQL(t, d.dialect, src)
				tb := tableByName(s, "t")
				if tb == nil {
					t.Fatalf("table t missing; have %v", sortedTableNames(s))
				}
				if !equalStrings(tb.PrimaryKey, c.wantPK) {
					t.Errorf("PrimaryKey = %v, want %v", tb.PrimaryKey, c.wantPK)
				}
				if !equalStrings(indexStrings(tb), c.wantIdx) {
					t.Errorf("Indexes = %v, want %v", indexStrings(tb), c.wantIdx)
				}
				if !equalStrings(fkStrings(tb), c.wantFK) {
					t.Errorf("ForeignKeys = %v, want %v", fkStrings(tb), c.wantFK)
				}
				// The host column must survive intact. Before this change the
				// FOREIGN KEY cases swallowed the constraint into b's TYPE.
				if got := columnNames(*tb); !equalStrings(got, []string{"a", "b"}) {
					t.Errorf("columns = %v, want [a b]", got)
				}
				if col := columnByName(tb, "b"); col == nil || col.RawType != "INT" {
					t.Errorf("column b RawType = %q, want \"INT\" (the constraint must not be swallowed into the type)", col.RawType)
				}
				// Nothing was dropped, so nothing is demoted (ADR 0034 §2.4 /
				// ADR 0041 §2.5: a false demotion is its own defect).
				if !tb.StructureProven() {
					t.Errorf("StructureProven = false (note %q, unreduced %+v), want true — nothing was dropped", tb.Note, tb.Unreduced)
				}
			})
		}
	}
}

// TestSQLDDL_MissingCommaCut_ReportsTheConstraintsOwnLine locks that a
// recovered constraint carries the position of the line it is WRITTEN on, not
// the line of the column definition it was glued to. An agent reading a routed
// surface item is told to look at file:line; pointing it at the wrong line is
// a smaller lie than a fabricated key, but it is still a lie.
func TestSQLDDL_MissingCommaCut_ReportsTheConstraintsOwnLine(t *testing.T) {
	// Line 1 CREATE, line 2 "a INT,", line 3 "b INT", line 4 the constraint.
	src := "CREATE TABLE t(\na INT,\nb INT\nFOREIGN KEY (a) REFERENCES o(x)\n);\n"
	s := parseSQL(t, sqlddl.Postgres(), src)
	tb := tableByName(s, "t")
	if tb == nil {
		t.Fatalf("table t missing; have %v", sortedTableNames(s))
	}
	if len(tb.ForeignKeys) != 1 {
		t.Fatalf("ForeignKeys = %+v, want exactly one", tb.ForeignKeys)
	}
	if got := tb.ForeignKeys[0].Pos.Line; got != 4 {
		t.Errorf("recovered FOREIGN KEY line = %d, want 4 (the line the constraint is written on)", got)
	}
}

// TestSQLDDL_LegalInlineKeyConstraint_IsNeverCut is the FALSE-CUT control, and
// it carries as much weight as the recovery cases: a boundary rule that
// recovers a fabricated key by breaking `id INTEGER PRIMARY KEY` would be a
// strictly worse trade. Every shape here is LEGAL column-constraint syntax in
// at least one supported dialect and must reduce exactly as it did before.
//
// The parenthesised continuations are the sharp ones: PostgreSQL's
// `PRIMARY KEY WITH (…)` / `UNIQUE WITH (…)` (index_parameters) and T-SQL's
// `PRIMARY KEY CLUSTERED WITH (…)` put a '(' after an INLINE key — they are
// exactly why the rule requires the parenthesis to follow the head keywords
// IMMEDIATELY, with no intervening identifier.
func TestSQLDDL_LegalInlineKeyConstraint_IsNeverCut(t *testing.T) {
	cases := []struct {
		name    string
		def     string
		wantPK  []string
		wantIdx []string
		wantFK  []string
	}{
		{name: "bare inline PRIMARY KEY", def: "id INTEGER PRIMARY KEY", wantPK: []string{"id"}},
		{name: "inline PRIMARY KEY CLUSTERED", def: "id INT PRIMARY KEY CLUSTERED", wantPK: []string{"id"}},
		{name: "inline PRIMARY KEY WITH (storage parameters)", def: "id INT PRIMARY KEY WITH (fillfactor = 70)", wantPK: []string{"id"}},
		{name: "inline PRIMARY KEY INCLUDE (covering columns)", def: "id INT PRIMARY KEY INCLUDE (a)", wantPK: []string{"id"}},
		{name: "inline PRIMARY KEY ON a partition scheme", def: "id INT PRIMARY KEY CLUSTERED ON ps_name (a)", wantPK: []string{"id"}},
		{name: "inline UNIQUE WITH (storage parameters)", def: "id INT UNIQUE WITH (fillfactor = 70)", wantIdx: []string{"id UNIQUE"}},
		// The two shapes that prove reservedHeadContinuation: a ONE-word legal
		// continuation followed directly by a parenthesis is exactly what the
		// optional index-name slot would otherwise swallow. A false cut here
		// invents a unique index over the parentheses' contents ("other", "0").
		{name: "inline UNIQUE KEY then CHECK (…)", def: "id INT UNIQUE KEY CHECK (other > 0)", wantIdx: []string{"id UNIQUE"}},
		{name: "inline UNIQUE KEY then DEFAULT (…)", def: "id INT UNIQUE KEY DEFAULT (0)", wantIdx: []string{"id UNIQUE"}},
		{name: "inline CONSTRAINT <name> PRIMARY KEY", def: "id INT CONSTRAINT pk_t PRIMARY KEY", wantPK: []string{"id"}},
		{name: "T-SQL inline FOREIGN KEY REFERENCES", def: "id INT FOREIGN KEY REFERENCES o(x)", wantFK: []string{"id->o"}},
		{name: "T-SQL inline FOREIGN KEY REFERENCES, quoted table", def: `id INT FOREIGN KEY REFERENCES "o"(x)`, wantFK: []string{"id->o"}},
	}
	for _, d := range dialectsUnderTest() {
		for _, c := range cases {
			t.Run(d.name+"/"+c.name, func(t *testing.T) {
				s := parseSQL(t, d.dialect, "CREATE TABLE t(\n"+c.def+",\nname TEXT\n);\n")
				tb := tableByName(s, "t")
				if tb == nil {
					t.Fatalf("table t missing; have %v", sortedTableNames(s))
				}
				if !equalStrings(tb.PrimaryKey, c.wantPK) {
					t.Errorf("PrimaryKey = %v, want %v", tb.PrimaryKey, c.wantPK)
				}
				if !equalStrings(indexStrings(tb), c.wantIdx) {
					t.Errorf("Indexes = %v, want %v", indexStrings(tb), c.wantIdx)
				}
				if !equalStrings(fkStrings(tb), c.wantFK) {
					t.Errorf("ForeignKeys = %v, want %v", fkStrings(tb), c.wantFK)
				}
				if got := columnNames(*tb); !equalStrings(got, []string{"id", "name"}) {
					t.Errorf("columns = %v, want [id name] — the column must not be cut in half", got)
				}
				if !tb.StructureProven() {
					t.Errorf("StructureProven = false (note %q), want true — this is legal inline syntax", tb.Note)
				}
			})
		}
	}
}

// TestSQLDDL_MissingCommaCut_FabricationGuards proves the three exclusions the
// boundary walk applies, each against a shape a dialect actually writes. Every
// case here FABRICATES a key that appears nowhere in the DDL when its guard is
// removed — that is what makes them controls rather than decoration, and each
// was mutation-proven that way.
func TestSQLDDL_MissingCommaCut_FabricationGuards(t *testing.T) {
	cases := []struct {
		name string
		def  string
	}{
		{
			// MySQL writes column comments as COMMENT='…'. A head inside a
			// string literal is text, not syntax.
			name: "inside a single-quoted string literal",
			def:  "b INT DEFAULT 0 COMMENT 'PRIMARY KEY (a, b)'",
		},
		{
			// split() canonicalizes `x` and [x] to "x", so a column literally
			// named PRIMARY KEY (or UNIQUE) arrives quoted.
			name: "inside a canonical quoted identifier",
			def:  `"PRIMARY KEY (a, b)" INT`,
		},
		{
			// A head nested at depth > 0 belongs to the enclosing expression,
			// never to the table. Unlike the two above this input is
			// DELIBERATELY ADVERSARIAL and is labelled as such: the word
			// boundary and the type/name shape of real DDL already defeat the
			// realistic candidates, so the depth guard has to be exercised
			// directly. It carries no single-quoted string on purpose, so it
			// tests the depth exclusion and nothing else.
			name: "at paren depth greater than zero",
			def:  "b INT CHECK (b > 0 AND PRIMARY KEY (a, b) IS NULL)",
		},
	}
	for _, d := range dialectsUnderTest() {
		for _, c := range cases {
			t.Run(d.name+"/"+c.name, func(t *testing.T) {
				s := parseSQL(t, d.dialect, "CREATE TABLE t(\na INT,\n"+c.def+"\n);\n")
				tb := tableByName(s, "t")
				if tb == nil {
					t.Fatalf("table t missing; have %v", sortedTableNames(s))
				}
				if len(tb.PrimaryKey) != 0 {
					t.Errorf("PrimaryKey = %v, want none — the DDL declares no key here; the cut fabricated one", tb.PrimaryKey)
				}
				if len(tb.Indexes) != 0 {
					t.Errorf("Indexes = %v, want none — the DDL declares no index here", indexStrings(tb))
				}
			})
		}
	}
}

// TestSQLDDL_ProperTableConstraintItem_IsNotCutAtItsOwnHead locks the
// termination property AND the CONSTRAINT-preamble extension: a body item that
// IS a table constraint already starts at its own head, so it must never be
// cut. The named form is the load-bearing one — its head sits at offset 15, not
// 0, and only the backward walk over `CONSTRAINT <name>` keeps it whole.
func TestSQLDDL_ProperTableConstraintItem_IsNotCutAtItsOwnHead(t *testing.T) {
	cases := []struct {
		name   string
		item   string
		wantPK []string
	}{
		{name: "bare table-level PRIMARY KEY", item: "PRIMARY KEY (a, b)", wantPK: []string{"a", "b"}},
		{name: "named table-level PRIMARY KEY", item: "CONSTRAINT pk_t PRIMARY KEY (a, b)", wantPK: []string{"a", "b"}},
	}
	for _, d := range dialectsUnderTest() {
		for _, c := range cases {
			t.Run(d.name+"/"+c.name, func(t *testing.T) {
				s := parseSQL(t, d.dialect, "CREATE TABLE t(\na INT,\nb INT,\n"+c.item+"\n);\n")
				tb := tableByName(s, "t")
				if tb == nil {
					t.Fatalf("table t missing; have %v", sortedTableNames(s))
				}
				if !equalStrings(tb.PrimaryKey, c.wantPK) {
					t.Errorf("PrimaryKey = %v, want %v", tb.PrimaryKey, c.wantPK)
				}
				if got := columnNames(*tb); !equalStrings(got, []string{"a", "b"}) {
					t.Errorf("columns = %v, want [a b] — no phantom column may be created", got)
				}
				// The sharp assertion for the named form: cutting it at
				// PRIMARY leaves "CONSTRAINT pk_t" behind as an item the
				// dispatch cannot classify, which routes the whole table to
				// the abstention floor — a false demotion caused by the fix.
				if !tb.StructureProven() {
					t.Errorf("StructureProven = false (note %q, unreduced %+v), want true — a properly "+
						"comma-separated constraint must not be cut in half", tb.Note, tb.Unreduced)
				}
			})
		}
	}
}

// TestSQLDDL_MissingComma_RecoversEveryConstraintInARun locks the recursion: a
// body item carrying MORE than one missing comma re-enters the boundary rule,
// and the residual is strictly shorter each time, so the walk terminates.
func TestSQLDDL_MissingComma_RecoversEveryConstraintInARun(t *testing.T) {
	for _, d := range dialectsUnderTest() {
		t.Run(d.name, func(t *testing.T) {
			s := parseSQL(t, d.dialect,
				"CREATE TABLE t(\na INT,\nb INT\nPRIMARY KEY (a, b)\nUNIQUE (b)\nFOREIGN KEY (a) REFERENCES o(x)\n);\n")
			tb := tableByName(s, "t")
			if tb == nil {
				t.Fatalf("table t missing; have %v", sortedTableNames(s))
			}
			if !equalStrings(tb.PrimaryKey, []string{"a", "b"}) {
				t.Errorf("PrimaryKey = %v, want [a b]", tb.PrimaryKey)
			}
			if !equalStrings(indexStrings(tb), []string{"b UNIQUE"}) {
				t.Errorf("Indexes = %v, want [b UNIQUE]", indexStrings(tb))
			}
			if !equalStrings(fkStrings(tb), []string{"a->o"}) {
				t.Errorf("ForeignKeys = %v, want [a->o]", fkStrings(tb))
			}
			if got := columnNames(*tb); !equalStrings(got, []string{"a", "b"}) {
				t.Errorf("columns = %v, want [a b]", got)
			}
		})
	}
}

// TestSQLDDL_MissingComma_RealFixture drives the REAL parser over an authored
// fixture transcribing the shape found on a public warehouse script (kenapDW's
// Fact_Reservation), which reported PrimaryKey=[Profit] against a DDL declaring
// a multi-column key. It uses ordinary ';' terminators on purpose: the defect
// is delimiter-independent and has nothing to do with ADR 0041's run-on
// separation, which only made it reachable on that corpus.
func TestSQLDDL_MissingComma_RealFixture(t *testing.T) {
	path := filepath.Join("testdata", "tsql", "constructed_missing_comma_table_constraint.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: path, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	tb := tableByName(s, "Fact_Reservation")
	if tb == nil {
		t.Fatalf("table Fact_Reservation missing; have %v", sortedTableNames(s))
	}
	want := []string{"Car_sid", "Date_from", "Date_to", "Customer_sid"}
	if !equalStrings(tb.PrimaryKey, want) {
		t.Errorf("PrimaryKey = %v, want %v (the fixture declares it on its own line, with no separating comma)", tb.PrimaryKey, want)
	}
	wantCols := []string{"Car_sid", "Date_from", "Date_to", "Customer_sid", "Time_of_reservation", "Profit"}
	if got := columnNames(*tb); !equalStrings(got, wantCols) {
		t.Errorf("columns = %v, want %v", got, wantCols)
	}
	if col := columnByName(tb, "Profit"); col == nil || col.RawType != "INT" {
		t.Errorf("Profit RawType = %q, want \"INT\"", col.RawType)
	}
	// The four inline T-SQL FOREIGN KEY REFERENCES forms must survive.
	if len(tb.ForeignKeys) != 4 {
		t.Errorf("ForeignKeys = %v, want 4 inline T-SQL references", fkStrings(tb))
	}
	if !tb.StructureProven() {
		t.Errorf("StructureProven = false (note %q), want true", tb.Note)
	}
	if len(s.Unreduced) != 0 {
		t.Errorf("Schema.Unreduced = %+v, want empty", s.Unreduced)
	}
}

// TestSQLDDL_MissingComma_ShapesThatStayUnrecovered_AreDeclared is a
// CHARACTERIZATION test, and it is deliberate. The boundary rule covers only
// the heads whose reading the grammar DECIDES; the shapes below either have a
// legal inline reading (CHECK) or need a discriminator this rule does not have
// (MySQL's KEY/INDEX secondary-index shorthand, whose bare KEY is ALSO a legal
// inline column modifier meaning PRIMARY KEY). They are recorded as known
// limit (12) of internal/core/dbcoverage/dbcoverage.go and asserted here so the
// declared limit is MACHINE-visible (ADR 0034 §2.7) rather than prose.
//
// None of them fabricates a KEY: CHECK reduces identically with and without the
// comma (it is a declared skip either way), and KEY/INDEX loses the index while
// corrupting the preceding column's RawType. Invert the relevant case, do not
// delete it, when the reducer learns the shape.
func TestSQLDDL_MissingComma_ShapesThatStayUnrecovered_AreDeclared(t *testing.T) {
	t.Run("CHECK has a legal inline reading, so it is not cut", func(t *testing.T) {
		s := parseSQL(t, sqlddl.Postgres(), "CREATE TABLE t(\na INT,\nb INT\nCHECK (a > 0)\n);\n")
		tb := tableByName(s, "t")
		if tb == nil {
			t.Fatalf("table t missing")
		}
		// Identical to the comma-separated reading: CHECK declares no key,
		// index or column, so both readings reduce to the same model.
		if len(tb.PrimaryKey) != 0 || len(tb.Indexes) != 0 {
			t.Errorf("PrimaryKey=%v Indexes=%v, want none", tb.PrimaryKey, indexStrings(tb))
		}
		if got := columnNames(*tb); !equalStrings(got, []string{"a", "b"}) {
			t.Errorf("columns = %v, want [a b]", got)
		}
	})

	t.Run("MySQL KEY/INDEX shorthand is NOT recovered: the index is lost and the type corrupted", func(t *testing.T) {
		s := parseSQL(t, sqlddl.MySQL(), "CREATE TABLE t(\na INT,\nb INT\nKEY idx_ab (a, b)\n);\n")
		tb := tableByName(s, "t")
		if tb == nil {
			t.Fatalf("table t missing")
		}
		if len(tb.Indexes) != 0 {
			t.Errorf("Indexes = %v — if the index is now READ, this limit is closed: invert this case "+
				"and update dbcoverage.go limit (12)", indexStrings(tb))
		}
		col := columnByName(tb, "b")
		if col == nil {
			t.Fatalf("column b missing; have %v", columnNames(*tb))
		}
		if col.RawType != "INT\nKEY idx_ab (a, b)" {
			t.Errorf("column b RawType = %q — the characterized (wrong) value is \"INT\\nKEY idx_ab (a, b)\"; "+
				"if it now reads \"INT\" the limit changed and this case must be updated", col.RawType)
		}
	})

	t.Run("a missing comma between two plain columns is undecidable and stays unrecovered", func(t *testing.T) {
		s := parseSQL(t, sqlddl.Postgres(), "CREATE TABLE t(\na INT\nb INT\n);\n")
		tb := tableByName(s, "t")
		if tb == nil {
			t.Fatalf("table t missing")
		}
		// No keyword marks the boundary, so the two definitions read as one
		// column. Characterized, not fixed: any rule that guessed here would
		// be inventing a column name.
		if got := columnNames(*tb); !equalStrings(got, []string{"a"}) {
			t.Errorf("columns = %v — the characterized value is [a]; if it now reads [a b] this limit changed", got)
		}
	})
}
