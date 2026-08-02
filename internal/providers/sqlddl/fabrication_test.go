package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// R1 — settling the fabrication hypothesis (design §8, spec "R1 — Fabrication
// Hypothesis"). Before any design decision may depend on it, this test
// exercises non-single-space ADD/CONSTRAINT input under the SQLServer dialect
// and asserts the reducer's ACTUAL, TODAY output — not a prediction.
//
// BRANCH OUTCOME: CONFIRMED (run 2026-07-30, go test -run
// TestSQLDDL_R1_FabricationHypothesis -v -count=1, exit 0). Verbatim actual
// output at first execution:
//
//	R1-a (ADD  CONSTRAINT [pk] PRIMARY KEY ([a])):
//	  PrimaryKey=[CONSTRAINT] Columns=[a b CONSTRAINT]
//	R1-b (ADD  CONSTRAINT [fk] FOREIGN KEY ([a]) REFERENCES [dbo].[d]([a])):
//	  ForeignKeys=[{{r1.sql 3} [CONSTRAINT] d [a]}] Columns=[a b CONSTRAINT]
//	R1-c (ADD + tab + CONSTRAINT):
//	  PrimaryKey=[] Columns=[a b]  (clean drop, as predicted)
//
// Per spec Scenario A (CONFIRMED): fabrication gets its OWN disposition — the
// completeness contract alone does not cover a reducer that believes it
// succeeded. Design §8c's first disposition narrowed the fabrication at its
// source, converting a CONSTRAINT-prefixed remainder to MarkUnproven instead
// of applyColumn — an honest abstention, but still a DROP of a constraint the
// DDL plainly declares.
//
// tsql-alter-add-constraint SUPERSEDES that disposition with the real fix:
// applyAlterAdd dispatches on the item's LEADING KEYWORD, so the whitespace
// between ADD and CONSTRAINT is irrelevant and every case below now REDUCES
// correctly. What these tests still lock — and the reason they were kept
// rather than deleted — is that the phantom column literally named
// "CONSTRAINT" never comes back: it is the observable signature of the
// original fabrication, and it is asserted absent in every case.
//
// The verbatim CONFIRMED output above is preserved as the historical record
// of the branch decision; it is no longer this reducer's behavior.
func TestSQLDDL_R1_FabricationHypothesis(t *testing.T) {
	const tableDecl = "CREATE TABLE [dbo].[f]([a] [int] NOT NULL,[b] [int] NOT NULL);\nGO\n"
	parse := func(t *testing.T, src string) db.Table {
		t.Helper()
		p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
		s, err := p.ParseSchema([]providers.SourceFile{{Path: "r1.sql", Content: []byte(src)}})
		if err != nil {
			t.Fatalf("ParseSchema: %v", err)
		}
		for _, tb := range s.Tables {
			if tb.Name == "f" {
				return tb
			}
		}
		t.Fatalf("table f not found")
		return db.Table{}
	}

	t.Run("control: single-space ADD CONSTRAINT parses correctly", func(t *testing.T) {
		got := parse(t, tableDecl+"ALTER TABLE [dbo].[f] ADD CONSTRAINT [pk] PRIMARY KEY ([a]);\nGO\n")
		if len(got.PrimaryKey) != 1 || got.PrimaryKey[0] != "a" {
			t.Errorf("PrimaryKey = %v, want [a] (this is the sound baseline the R1-a/b cases are compared against)", got.PrimaryKey)
		}
	})

	// R1-a/b originally CONFIRMED fabrication (design §8a code read):
	// "PRIMARY" is in sqlserverModifiers() (types.go:105-111), so
	// applyColumn's modifier scan (splitTypeAndMods) swallowed "PRIMARY KEY
	// (...)" / "FOREIGN KEY (...) REFERENCES ..." as a column's trailing
	// modifiers, fabricating a phantom column literally named "CONSTRAINT"
	// plus a phantom key (see this file's history / commit "test(sqlddl):
	// settle R1 fabrication hypothesis — CONFIRMED" for the original verbatim
	// output). Under tsql-alter-add-constraint the keyword-driven dispatch
	// reduces these correctly, so the assertions are UPDATED (not loosened):
	// the REAL key is read, the phantom column is still absent, and the table
	// stays proven because nothing was dropped.
	t.Run("R1-a: ADD  CONSTRAINT (double space) before a PRIMARY KEY — reduced, not fabricated", func(t *testing.T) {
		got := parse(t, tableDecl+"ALTER TABLE [dbo].[f] ADD  CONSTRAINT [pk] PRIMARY KEY ([a]);\nGO\n")
		if len(got.PrimaryKey) != 1 || got.PrimaryKey[0] != "a" {
			t.Errorf("PrimaryKey = %v, want [a] — the DDL declares it", got.PrimaryKey)
		}
		if hasColumn(got, "CONSTRAINT") {
			t.Errorf("columns = %v, want no phantom CONSTRAINT column", columnNames(got))
		}
		if !got.Complete {
			t.Errorf("Complete = false, want true — nothing was dropped; Unreduced = %+v", got.Unreduced)
		}
	})

	t.Run("R1-b: ADD  CONSTRAINT (double space) before a FOREIGN KEY — reduced, not fabricated", func(t *testing.T) {
		got := parse(t, tableDecl+"ALTER TABLE [dbo].[f] ADD  CONSTRAINT [fk] FOREIGN KEY ([a]) REFERENCES [dbo].[d]([a]);\nGO\n")
		if len(got.ForeignKeys) != 1 || got.ForeignKeys[0].RefTable != "d" {
			t.Errorf("ForeignKeys = %+v, want the one declared, referencing d", got.ForeignKeys)
		}
		if hasColumn(got, "CONSTRAINT") {
			t.Errorf("columns = %v, want no phantom CONSTRAINT column", columnNames(got))
		}
		if !got.Complete {
			t.Errorf("Complete = false, want true — nothing was dropped; Unreduced = %+v", got.Unreduced)
		}
	})

	// R1-c used to predict a clean DROP, because the old dispatch's "ADD "
	// prefix required a literal single space and a tab fell straight to the
	// applyAlterAction default. Keyword dispatch makes the separator
	// irrelevant, so a tab now reads exactly like a space.
	t.Run("R1-c: ADD + tab + CONSTRAINT — the separator no longer decides anything", func(t *testing.T) {
		got := parse(t, tableDecl+"ALTER TABLE [dbo].[f] ADD\tCONSTRAINT [pk] PRIMARY KEY ([a]);\nGO\n")
		if len(got.PrimaryKey) != 1 || got.PrimaryKey[0] != "a" {
			t.Errorf("PrimaryKey = %v, want [a]", got.PrimaryKey)
		}
		if hasColumn(got, "CONSTRAINT") {
			t.Errorf("columns = %v, want no phantom CONSTRAINT column", columnNames(got))
		}
	})
}

func hasColumn(t db.Table, name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func columnNames(t db.Table) []string {
	out := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		out[i] = c.Name
	}
	return out
}

// TestSQLDDL_MissingCommaBeforeTableConstraint_FabricatesTheWrongKey
// characterizes a SECOND fabrication of the ADR 0034 §2.6 class — a reducer
// that believes it succeeded — found while measuring run-on separation (ADR
// 0041) and DELIBERATELY not fixed there.
//
// A CREATE TABLE body item that is missing its separating comma before a
// table-level PRIMARY KEY is not split by splitTopLevelParts (which splits on
// top-level commas), so the whole run is one item, it starts with a column
// name, and the trailing PRIMARY KEY reads as that column's INLINE key. The
// composite key the DDL declares is replaced by a single-column one, and the
// table still reports Complete=true — the completeness contract cannot see it,
// because nothing was dropped.
//
// It is PRE-EXISTING and has nothing to do with run-on separation: the "with a
// terminator" case below is an ordinary ';'-delimited statement and fabricates
// identically. Run-on separation only made it REACHABLE on real DDL —
// dw-kenap's Fact_Reservation is written this way and reports pk=[Profit]
// where its DDL declares a six-column key.
//
// This is a CHARACTERIZATION test: it asserts today's wrong output on purpose,
// so the limit declared in dbcoverage.go is machine-visible (ADR 0034 §2.7). It
// must be INVERTED, not deleted, when the reducer learns this shape.
func TestSQLDDL_MissingCommaBeforeTableConstraint_FabricatesTheWrongKey(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "with a terminator (no run-on involved)",
			src: "CREATE TABLE fact_reservation(\n" +
				"car_sid INT,\n" +
				"profit INT\n" +
				"PRIMARY KEY(car_sid, profit)\n" +
				");\n",
		},
		{
			name: "recovered from a run-on",
			src: "CREATE TABLE dim_car(car_sid INT NOT NULL PRIMARY KEY)\n" +
				"CREATE TABLE fact_reservation(\n" +
				"car_sid INT,\n" +
				"profit INT\n" +
				"PRIMARY KEY(car_sid, profit)\n" +
				")\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := parseSQL(t, sqlddl.SQLServer(), c.src)
			tb := tableByName(s, "fact_reservation")
			if tb == nil {
				t.Fatalf("table fact_reservation missing; have %v", sortedTableNames(s))
			}
			// The DDL declares PRIMARY KEY(car_sid, profit). The reducer
			// reports the LAST column alone. Asserted as-is: this is the
			// defect, characterized.
			if !equalStrings(tb.PrimaryKey, []string{"profit"}) {
				t.Errorf("PrimaryKey = %v, want [profit] — this test characterizes a KNOWN "+
					"defect (the DDL declares [car_sid profit]); if it now reads [car_sid profit], "+
					"the defect is FIXED: invert this test and close the coverage limit", tb.PrimaryKey)
			}
			// And the reason the contract cannot catch it: nothing was dropped.
			if !tb.StructureProven() {
				t.Errorf("StructureProven = false (note %q), want true — the point of this "+
					"characterization is that a FABRICATION reports itself complete", tb.Note)
			}
		})
	}
}

// TestSQLDDL_CommaBeforeTableConstraint_IsTheControl proves the case above is
// about the MISSING COMMA and nothing else: the same body with the comma
// present reduces the composite key correctly. Without this control the test
// above could be locking a general inability to read a composite primary key.
func TestSQLDDL_CommaBeforeTableConstraint_IsTheControl(t *testing.T) {
	src := "CREATE TABLE fact_reservation(\n" +
		"car_sid INT,\n" +
		"profit INT,\n" +
		"PRIMARY KEY(car_sid, profit)\n" +
		");\n"
	s := parseSQL(t, sqlddl.SQLServer(), src)
	tb := tableByName(s, "fact_reservation")
	if tb == nil {
		t.Fatalf("table fact_reservation missing; have %v", sortedTableNames(s))
	}
	if !equalStrings(tb.PrimaryKey, []string{"car_sid", "profit"}) {
		t.Errorf("PrimaryKey = %v, want [car_sid profit]", tb.PrimaryKey)
	}
}
