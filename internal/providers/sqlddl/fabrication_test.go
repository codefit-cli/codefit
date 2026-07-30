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
// succeeded. Design §8c's disposition (narrow the fabrication at its source
// in applyAlterAction, converting a CONSTRAINT-prefixed remainder to
// MarkUnproven instead of applyColumn) is implemented once db.Table.
// MarkUnproven exists — see reduce.go's applyAlterAction. Once that fix
// lands, this test's R1-a/R1-b assertions are UPDATED (not loosened) to lock
// the corrected behavior: Complete=false + an Unreduced entry, no phantom
// column/key.
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

	// R1-a/b predict CONFIRMED fabrication (design §8a code read): "PRIMARY" is
	// in sqlserverModifiers() (types.go:105-111), so applyColumn's modifier
	// scan (splitTypeAndMods) swallows "PRIMARY KEY (...)" / "FOREIGN KEY
	// (...) REFERENCES ..." as a column's trailing modifiers, fabricating a
	// phantom column literally named "CONSTRAINT" plus a phantom key. Hard
	// assertions on the prediction: if they fail, that failure IS the branch
	// signal (REFUTED) and this test must be corrected to lock the real
	// output, never loosened to hide the mismatch.
	t.Run("R1-a: ADD  CONSTRAINT (double space) before a PRIMARY KEY — predicts CONFIRMED fabrication", func(t *testing.T) {
		got := parse(t, tableDecl+"ALTER TABLE [dbo].[f] ADD  CONSTRAINT [pk] PRIMARY KEY ([a]);\nGO\n")
		t.Logf("R1-a actual output: PrimaryKey=%v Columns=%v", got.PrimaryKey, columnNames(got))
		if len(got.PrimaryKey) != 1 || got.PrimaryKey[0] != "CONSTRAINT" {
			t.Errorf("PrimaryKey = %v, want [CONSTRAINT] (predicted fabrication per design §8a)", got.PrimaryKey)
		}
		if !hasColumn(got, "CONSTRAINT") {
			t.Errorf("columns = %v, want a phantom column named CONSTRAINT (predicted fabrication per design §8a)", columnNames(got))
		}
	})

	t.Run("R1-b: ADD  CONSTRAINT (double space) before a FOREIGN KEY — predicts CONFIRMED fabrication", func(t *testing.T) {
		got := parse(t, tableDecl+"ALTER TABLE [dbo].[f] ADD  CONSTRAINT [fk] FOREIGN KEY ([a]) REFERENCES [dbo].[d]([a]);\nGO\n")
		t.Logf("R1-b actual output: ForeignKeys=%v Columns=%v", got.ForeignKeys, columnNames(got))
		if len(got.ForeignKeys) != 1 || got.ForeignKeys[0].RefTable != "d" {
			t.Errorf("ForeignKeys = %v, want one phantom FK to d (predicted fabrication per design §8a)", got.ForeignKeys)
		}
	})

	// R1-c predicts a clean drop: "ADD " (reduce.go:442) requires a literal
	// single space, so a tab falls straight to the applyAlterAction default —
	// no phantom column, no phantom key.
	t.Run("R1-c: ADD + tab + CONSTRAINT — predicts a clean drop, no fabrication", func(t *testing.T) {
		got := parse(t, tableDecl+"ALTER TABLE [dbo].[f] ADD\tCONSTRAINT [pk] PRIMARY KEY ([a]);\nGO\n")
		t.Logf("R1-c actual output: PrimaryKey=%v Columns=%v", got.PrimaryKey, columnNames(got))
		if len(got.PrimaryKey) != 0 {
			t.Errorf("PrimaryKey = %v, want empty (predicted clean drop per design §8a)", got.PrimaryKey)
		}
		if hasColumn(got, "CONSTRAINT") {
			t.Errorf("columns = %v, want no phantom CONSTRAINT column (predicted clean drop per design §8a)", columnNames(got))
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
