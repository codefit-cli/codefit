package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// T-SQL's ALTER TABLE ... ADD CONSTRAINT family — the shapes Microsoft's own
// generated scripts (SSMS, the AdventureWorks/AdventureWorksDW install
// scripts) use to declare primary and foreign keys, and which this reducer
// used to drop wholesale (the gap this package's own docs called "LIMIT 2").
//
// Every case here drives the REAL tokenizer + reducer over real statement
// text; nothing is a hand-built db.Table. The negative cases are as load
// bearing as the positive ones: a shape this reducer cannot read faithfully
// must fall to the honest-abstention floor (ADR 0034 — MarkUnproven,
// Complete=false, the raw statement kept), never to an invented column or key.
func tsqlAlterTable(t *testing.T, src string) db.Table {
	t.Helper()
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "alter.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return tableNamed(t, s, "f")
}

const tsqlAlterBaseTable = "CREATE TABLE [dbo].[f]([a] [int] NOT NULL,[b] [int] NOT NULL);\nGO\n"

// (1) WITH CHECK / WITH NOCHECK before ADD — the prefix AdventureWorksDW uses
// for every one of its primary keys, and SSMS emits for every foreign key.
func TestSQLDDL_TSQL_WithCheckAddConstraint_PrimaryKey(t *testing.T) {
	for _, prefix := range []string{"WITH CHECK", "WITH NOCHECK", "WITH  CHECK"} {
		t.Run(prefix, func(t *testing.T) {
			got := tsqlAlterTable(t, tsqlAlterBaseTable+
				"ALTER TABLE [dbo].[f] "+prefix+" ADD CONSTRAINT [pk] PRIMARY KEY CLUSTERED ([a]) ON [PRIMARY];\nGO\n")
			if len(got.PrimaryKey) != 1 || got.PrimaryKey[0] != "a" {
				t.Errorf("PrimaryKey = %v, want [a]", got.PrimaryKey)
			}
			if !got.StructureProven() {
				t.Errorf("StructureProven() = false, want true; Unreduced = %+v", got.Unreduced)
			}
		})
	}
}

// (2) Arbitrary whitespace between ADD and CONSTRAINT — AdventureWorksDW's
// foreign-key block puts a NEWLINE there; SSMS emits two spaces.
func TestSQLDDL_TSQL_AddConstraint_ArbitraryWhitespace(t *testing.T) {
	for name, sep := range map[string]string{
		"single space": " ",
		"double space": "  ",
		"tab":          "\t",
		"newline":      "\n    ",
	} {
		t.Run(name, func(t *testing.T) {
			got := tsqlAlterTable(t, tsqlAlterBaseTable+
				"ALTER TABLE [dbo].[f] ADD"+sep+"CONSTRAINT [fk1] FOREIGN KEY ([a]) REFERENCES [dbo].[d1] ([a]);\nGO\n")
			if len(got.ForeignKeys) != 1 {
				t.Fatalf("ForeignKeys = %+v, want exactly 1", got.ForeignKeys)
			}
			fk := got.ForeignKeys[0]
			if fk.RefTable != "d1" || len(fk.Columns) != 1 || fk.Columns[0] != "a" {
				t.Errorf("ForeignKeys[0] = %+v, want columns [a] referencing d1", fk)
			}
			if !got.StructureProven() {
				t.Errorf("StructureProven() = false, want true; Unreduced = %+v", got.Unreduced)
			}
		})
	}
}

// (3) A comma-chained constraint list: ONE ADD, N constraints. Every one of
// AdventureWorksDW's eight foreign keys after the first arrives this way.
func TestSQLDDL_TSQL_AddConstraint_CommaChainedList(t *testing.T) {
	got := tsqlAlterTable(t, tsqlAlterBaseTable+
		"ALTER TABLE [dbo].[f] ADD\n"+
		"    CONSTRAINT [fk1] FOREIGN KEY ([a]) REFERENCES [dbo].[d1] ([a]),\n"+
		"    CONSTRAINT [fk2] FOREIGN KEY ([b]) REFERENCES [dbo].[d2] ([b]),\n"+
		"    CONSTRAINT [u1] UNIQUE ([b]);\nGO\n")
	if len(got.ForeignKeys) != 2 {
		t.Errorf("ForeignKeys = %+v, want 2", got.ForeignKeys)
	}
	if len(got.Indexes) != 1 || !got.Indexes[0].Unique {
		t.Errorf("Indexes = %+v, want one UNIQUE index", got.Indexes)
	}
	if !got.StructureProven() {
		t.Errorf("StructureProven() = false, want true; Unreduced = %+v", got.Unreduced)
	}
}

// (4) The SSMS tails: WITH (...) index options and ON [PRIMARY] filegroup,
// both AFTER the column list. Reading the constraint must not be derailed by
// them, and the option list's own commas must not be mistaken for the
// constraint-list separator.
func TestSQLDDL_TSQL_AddConstraint_WithOptionsAndOnFilegroupTails(t *testing.T) {
	got := tsqlAlterTable(t, tsqlAlterBaseTable+
		"ALTER TABLE [dbo].[f] ADD  CONSTRAINT [pk] PRIMARY KEY CLUSTERED \n"+
		"(\n\t[a] ASC,\n\t[b] ASC\n)WITH (PAD_INDEX = OFF, STATISTICS_NORECOMPUTE = OFF, IGNORE_DUP_KEY = OFF) ON [PRIMARY];\nGO\n")
	if len(got.PrimaryKey) != 2 || got.PrimaryKey[0] != "a" || got.PrimaryKey[1] != "b" {
		t.Errorf("PrimaryKey = %v, want [a b]", got.PrimaryKey)
	}
	if !got.StructureProven() {
		t.Errorf("StructureProven() = false, want true; Unreduced = %+v", got.Unreduced)
	}
}

// (5) T-SQL's standalone CHECK/NOCHECK CONSTRAINT statement — SSMS emits one
// after every foreign key it generates. It only enables or disables constraint
// CHECKING; it can never declare a key, an index or a column, so it is a
// DECLARED, recognized skip (the same disposition ENABLE/DISABLE/VALIDATE
// already have) and must not demote the table to unproven.
func TestSQLDDL_TSQL_CheckConstraintStatement_IsRecognizedSkip(t *testing.T) {
	for _, verb := range []string{"CHECK", "NOCHECK"} {
		t.Run(verb, func(t *testing.T) {
			got := tsqlAlterTable(t, tsqlAlterBaseTable+
				"ALTER TABLE [dbo].[f] ADD CONSTRAINT [fk1] FOREIGN KEY ([a]) REFERENCES [dbo].[d1] ([a]);\nGO\n"+
				"ALTER TABLE [dbo].[f] "+verb+" CONSTRAINT [fk1];\nGO\n")
			if !got.StructureProven() {
				t.Errorf("StructureProven() = false, want true — %s CONSTRAINT declares nothing; Unreduced = %+v", verb, got.Unreduced)
			}
			if len(got.ForeignKeys) != 1 {
				t.Errorf("ForeignKeys = %+v, want the 1 declared by the previous statement", got.ForeignKeys)
			}
			if len(got.Columns) != 2 {
				t.Errorf("Columns = %d, want 2 — no phantom column may be invented", len(got.Columns))
			}
		})
	}
}

// (6) THE FABRICATION GUARD. A constraint whose column list this reducer
// cannot actually read must fall to the honest-abstention floor — never a
// silently empty key and never an invented one. This is the regression this
// package already caught once (a widened regex truncating at the first ')' and
// fabricating a phantom column); the shapes below are its ALTER-side twins.
func TestSQLDDL_TSQL_AddConstraint_UnreadableColumnList_AbstainsNeverFabricates(t *testing.T) {
	cases := map[string]string{
		"PRIMARY KEY with no column list at all": "ALTER TABLE [dbo].[f] ADD CONSTRAINT [pk] PRIMARY KEY CLUSTERED;\nGO\n",
		"PRIMARY KEY with an unbalanced list":    "ALTER TABLE [dbo].[f] ADD CONSTRAINT [pk] PRIMARY KEY CLUSTERED ([a];\nGO\n",
		"FOREIGN KEY with no column list":        "ALTER TABLE [dbo].[f] ADD CONSTRAINT [fk] FOREIGN KEY REFERENCES [dbo].[d1];\nGO\n",
		"UNIQUE with no column list":             "ALTER TABLE [dbo].[f] ADD CONSTRAINT [u] UNIQUE;\nGO\n",
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			got := tsqlAlterTable(t, tsqlAlterBaseTable+alter)
			if len(got.PrimaryKey) != 0 {
				t.Errorf("PrimaryKey = %v, want empty — nothing readable was declared", got.PrimaryKey)
			}
			if len(got.ForeignKeys) != 0 {
				t.Errorf("ForeignKeys = %+v, want empty — nothing readable was declared", got.ForeignKeys)
			}
			if len(got.Indexes) != 0 {
				t.Errorf("Indexes = %+v, want empty — nothing readable was declared", got.Indexes)
			}
			if len(got.Columns) != 2 {
				t.Errorf("Columns = %v, want the 2 real ones — no phantom column may be invented", columnNames(got))
			}
			if got.StructureProven() {
				t.Error("StructureProven() = true, want false — an unreadable constraint must be RECORDED, not silently believed complete")
			}
			if len(got.Unreduced) != 1 {
				t.Errorf("Unreduced = %d entries, want 1 (the raw statement the agent needs to judge for itself)", len(got.Unreduced))
			}
		})
	}
}

// (7) End to end over the vendored corpus: Microsoft's AdventureWorksDW is a
// genuine Kimball warehouse, and its three real primary keys and eight real
// foreign keys must now be IN the neutral model, with every table proven.
func TestSQLDDL_AdventureWorksDW_KeysAndForeignKeysAreReduced(t *testing.T) {
	s := awdwSchema(t)
	if len(s.Tables) != 3 {
		t.Fatalf("parsed %d tables, want 3", len(s.Tables))
	}

	wantPK := map[string][]string{
		"DimCustomer":       {"CustomerKey"},
		"DimDate":           {"DateKey"},
		"FactInternetSales": {"SalesOrderNumber", "SalesOrderLineNumber"},
	}
	for name, want := range wantPK {
		got := tableNamed(t, s, name).PrimaryKey
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s primary key = %v, want %v", name, got, want)
		}
	}

	fact := tableNamed(t, s, "FactInternetSales")
	if len(fact.ForeignKeys) != 8 {
		t.Errorf("FactInternetSales foreign keys = %d, want 8 (the DDL declares eight)", len(fact.ForeignKeys))
	}
	wantRefs := []string{"DimCurrency", "DimCustomer", "DimDate", "DimDate", "DimDate", "DimProduct", "DimPromotion", "DimSalesTerritory"}
	var gotRefs []string
	for _, fk := range fact.ForeignKeys {
		gotRefs = append(gotRefs, fk.RefTable)
	}
	if strings.Join(gotRefs, ",") != strings.Join(wantRefs, ",") {
		t.Errorf("FactInternetSales FK targets = %v, want %v", gotRefs, wantRefs)
	}

	for _, tb := range s.Tables {
		if !tb.StructureProven() {
			t.Errorf("%s: StructureProven() = false, want true; Unreduced = %+v", tb.Name, tb.Unreduced)
		}
	}
	if len(s.Unreduced) != 0 {
		t.Errorf("schema-level Unreduced = %+v, want none", s.Unreduced)
	}
}
