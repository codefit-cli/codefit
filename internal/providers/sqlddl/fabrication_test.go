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
