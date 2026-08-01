package sqlddl_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// The DW-0xx star-schema/SCD family measured against REAL parsed warehouse DDL
// — Microsoft's AdventureWorksDW (testdata/tsql/adventureworksdw_real_objects.sql,
// vendored MIT; see that file's header for provenance and the license text,
// which was verified by fetching upstream, not assumed from the sibling OLTP
// excerpt).
//
// WHAT THIS FILE ACTUALLY PROVES, stated plainly rather than implied: BOTH of
// the limits that used to keep this corpus silent are now CLOSED, and the
// canonical reference warehouse is reached by the DW family AS VENDORED, under
// Microsoft's own names, with no mutation of any kind.
//
//	LIMIT 1 (role NAME vocabulary) is GONE. It used to read: "AdventureWorksDW
//	uses PascalCase Kimball naming (FactInternetSales, DimCustomer), while
//	codefit's table-role detection recognizes only the snake_case prefixes
//	fact_/dim_/stg_/mart_." The paradigm role-name vocabulary now recognizes
//	PascalCase (and suffix, and case-insensitive) spellings, so all three of
//	this corpus's table names nominate a role.
//
//	LIMIT 2 (T-SQL ALTER reduction, a PRE-EXISTING parser gap discovered while
//	vendoring this fixture — NOT introduced by the DW rules) is GONE TOO. The
//	reducer used to drop three shapes of ALTER TABLE ... ADD CONSTRAINT —
//	T-SQL's WITH CHECK prefix, a newline between ADD and CONSTRAINT, and a
//	comma-chained constraint list — and AdventureWorksDW's real DDL uses all
//	three, so the warehouse's three real primary keys and all eight of its real
//	foreign keys were invisible to every DB and DW rule. They are now reduced;
//	each shape is locked POSITIVELY, in isolation, in
//	tsql_alter_add_constraint_test.go.
//
// With both closed, the corpus no longer needs the declared snake_case rename
// (kimballToSnakeCase) that earlier revisions of this file applied purely to
// route around LIMIT 1: the star is visible under the real names, so every
// assertion below reads Microsoft's DDL exactly as vendored — strictly closer
// to the real parser, per the project's prefer-the-real-corpus rule.
//
// What this file still does NOT claim: it is not the DW rules' fire-path
// dogfood. The positive and trap paths of the per-rule family stay proven by
// declared-synthetic constructed fixtures in internal/core/dwrules, per the
// ADR 0028 fixture-gap policy. This file is the honest record of how far the
// real corpus reaches end to end.

// awdwSchema parses the vendored AdventureWorksDW excerpt under the SQLServer
// dialect, verbatim. It used to accept declared textual mutations so a caller
// could rename the tables into snake_case; nothing needs that any more (see
// TestDW_AdventureWorksDW_StarIsVisible_AsVendored), so every test in this
// file reads Microsoft's DDL exactly as vendored.
func awdwSchema(t *testing.T) *db.Schema {
	t.Helper()
	path := filepath.Join("testdata", "tsql", "adventureworksdw_real_objects.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: path, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

// BOTH LIMITS CLOSED, measured end to end on the vendored corpus under its
// REAL names. This test replaces two predecessors that each locked one half of
// the old silence:
//
//   - ...PascalCaseNaming_IsNowRecognized asserted that the widened vocabulary
//     RECOGNIZED the names, and could only prove it indirectly, through
//     Classification.Unprovable membership, because the missing keys still
//     demoted every table. Unprovable is now EMPTY here — not a regression: a
//     table lands there only when its name nominated a role and structure
//     could not corroborate it, and structure now corroborates. The promoted
//     roles below are the same lock, one level stronger and direct: narrow the
//     vocabulary back and they collapse to unclassified.
//   - ...StarStillInvisible_DeclaredLimit asserted zero keys and a non-olap
//     paradigm, and told its successor in writing to "re-point this fixture at
//     the real assertions over a real, correctly modelled star". This is it.
//
// Every value below is derived from Microsoft's DDL, not adjusted to fit:
// three tables, a two-column fact key, one surrogate key per dimension, eight
// foreign keys out of the fact — each named, not merely counted.
func TestDW_AdventureWorksDW_StarIsVisible_AsVendored(t *testing.T) {
	s := awdwSchema(t)
	if len(s.Tables) != 3 {
		t.Fatalf("parsed %d tables, want 3 (DimCustomer, DimDate, FactInternetSales)", len(s.Tables))
	}

	// LIMIT 2's structural proof: the keys are in the model — asserted by
	// IDENTITY, never by count or arity. Mutation testing (see this commit's
	// message for both runs) proved the count/arity form these two assertions
	// used to have was blind in the FABRICATION direction, which is the one
	// that matters here:
	//
	//   - Removing applyAlterAdd's CONSTRAINT case makes the reducer fall
	//     through to its column path and invent a primary key literally named
	//     "CONSTRAINT" on all three tables. An ARITY check ACCEPTED that
	//     fabrication for both single-column dimensions — len([CONSTRAINT])
	//     is 1, which is exactly what it wanted — and only the fact table's
	//     2-vs-1 mismatch reported anything at all.
	//   - Reading the REFERENCES column list as the foreign key's own LOCAL
	//     column list (so OrderDateKey/DueDateKey/ShipDateKey all collapse to
	//     DateKey) left the COUNT at eight, and passed silently.
	//
	// Every value below is transcribed from Microsoft's DDL — the ALTER TABLE
	// block at the end of the fixture — in the reducer's own normalized
	// spelling: schema qualifiers stripped ([dbo].[DimCurrency] -> DimCurrency,
	// normalizeName) and the foreign keys in the order the DDL declares them.
	fact := tableNamed(t, s, "FactInternetSales")
	wantFK := []string{
		"CurrencyKey -> DimCurrency(CurrencyKey)",
		"CustomerKey -> DimCustomer(CustomerKey)",
		"OrderDateKey -> DimDate(DateKey)",
		"DueDateKey -> DimDate(DateKey)",
		"ShipDateKey -> DimDate(DateKey)",
		"ProductKey -> DimProduct(ProductKey)",
		"PromotionKey -> DimPromotion(PromotionKey)",
		"SalesTerritoryKey -> DimSalesTerritory(SalesTerritoryKey)",
	}
	if got := renderForeignKeys(fact); !slices.Equal(got, wantFK) {
		t.Errorf("FactInternetSales foreign keys =\n\t%s\nwant the eight the DDL declares, across a "+
			"newline-broken ADD and a comma-chained list =\n\t%s",
			strings.Join(got, "\n\t"), strings.Join(wantFK, "\n\t"))
	}
	wantPK := map[string][]string{
		"FactInternetSales": {"SalesOrderNumber", "SalesOrderLineNumber"},
		"DimCustomer":       {"CustomerKey"},
		"DimDate":           {"DateKey"},
	}
	for name, want := range wantPK {
		if pk := tableNamed(t, s, name).PrimaryKey; !slices.Equal(pk, want) {
			t.Errorf("%s primary key = %v, want %v — declared by WITH CHECK ADD CONSTRAINT", name, pk, want)
		}
	}
	for _, tb := range s.Tables {
		if !tb.StructureProven() {
			t.Errorf("%s StructureProven() = false, want true — every statement in this fixture is reduced now", tb.Name)
		}
	}

	// LIMIT 1's proof, direct: the real PascalCase names promote to real roles.
	c := paradigm.Detect(s)
	if c.Paradigm != paradigm.ParadigmOLAP {
		t.Errorf("Paradigm = %q, want olap — the parsed model now carries the keys that corroborate the star", c.Paradigm)
	}
	wantRoles := map[string]paradigm.Role{
		"FactInternetSales": paradigm.RoleFact,
		"DimCustomer":       paradigm.RoleDimension,
		"DimDate":           paradigm.RoleDimension,
	}
	for name, want := range wantRoles {
		if got := c.Roles[name]; got != want {
			t.Errorf("role[%s] = %q, want %q — a failure here means either the PascalCase spelling "+
				"regressed out of the role vocabulary (LIMIT 1) or the keys stopped being reduced "+
				"(LIMIT 2)", name, got, want)
		}
	}
	if len(c.Unprovable) != 0 {
		t.Errorf("Unprovable = %v, want empty — a recognized name lands there only when structure "+
			"cannot corroborate it, and this corpus's structure now does", c.Unprovable)
	}

	// The consequence: the DW family reaches the corpus at last.
	if _, surf := dwrules.Run(s, &c); len(surf) == 0 {
		t.Error("DW surface = 0 items — the DW family must now reach a real, correctly modelled star")
	}
}

// db-model-completeness-contract (2026-07-30): this test used to lock a KNOWN
// BUG — DB-050 deterministically AFFIRMED (confidence 1.0) that these three
// tables have no primary key, over real Microsoft-authored DDL that plainly
// declares one for each. That was the motivating false affirmation for the
// whole change (proposal SS1). The completeness contract converted that
// affirmation into a routed db-table-structure-unproven surface item;
// tsql-alter-add-constraint now removes the drop that caused it in the first
// place, so there is nothing left to route: every table is proven AND every
// declared primary key is in the model.
//
// Both halves are asserted. Dropping the "no DB-050 finding" half would let a
// future regression that re-loses the keys pass as long as it also re-lost the
// routing.
func TestDB050_AdventureWorksDW_KeysAreRead_NothingAffirmedOrRouted(t *testing.T) {
	s := awdwSchema(t)
	fs, surf := dbrules.Run(s)

	for _, f := range fs {
		if f.ID == "DB-050" {
			t.Errorf("DB-050 affirmed %q — every table in this fixture declares a primary key", f.Description)
		}
	}
	for _, it := range surf {
		if it.Category == string(surface.CategoryDBTableStructureUnproven) {
			t.Errorf("db-table-structure-unproven item at line %d — every statement in this fixture is now reduced: %+v", it.Line, it.StructuralSignals)
		}
	}
	for _, tb := range s.Tables {
		if len(tb.PrimaryKey) == 0 {
			t.Errorf("%s primary key = empty, want the one its ALTER TABLE declares", tb.Name)
		}
	}
}

// renderForeignKeys renders a table's foreign keys as "cols -> RefTable(refCols)"
// strings, in model order, so a comparison reads every field the reducer
// populates — local columns, referenced table, referenced columns — instead of
// just how many rows it produced. Any of the three going wrong is a
// fabrication the count alone cannot see.
func renderForeignKeys(t db.Table) []string {
	out := make([]string, 0, len(t.ForeignKeys))
	for _, fk := range t.ForeignKeys {
		out = append(out, fmt.Sprintf("%s -> %s(%s)",
			strings.Join(fk.Columns, ","), fk.RefTable, strings.Join(fk.RefColumns, ",")))
	}
	return out
}

// tableNamed returns the table with the given name, failing the test when absent.
func tableNamed(t *testing.T, s *db.Schema, name string) db.Table {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found in parsed schema", name)
	return db.Table{}
}
