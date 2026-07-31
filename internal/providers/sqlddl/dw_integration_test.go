package sqlddl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
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
// WHAT THIS FILE ACTUALLY PROVES, stated plainly rather than implied: the
// canonical reference warehouse currently yields NO DW finding, for TWO
// independent reasons, and both are locked here so neither can regress into
// silence. The rules' positive fire paths are therefore proven by
// declared-synthetic constructed fixtures in the per-rule unit tests
// (internal/core/dwrules), per the ADR 0028 fixture-gap policy — this file is
// the honest record of why the real corpus cannot carry them yet.
//
//	LIMIT 1 (role detection). AdventureWorksDW uses PascalCase Kimball naming
//	(FactInternetSales, DimCustomer), while codefit's S1 table-role detection
//	recognizes only the snake_case prefixes fact_/dim_/stg_/mart_ (locked
//	decision A5, ADR 0033). Every table classifies "unclassified", so no DW
//	rule reaches any of them.
//
//	LIMIT 2 (T-SQL ALTER reduction, a PRE-EXISTING parser gap discovered while
//	vendoring this fixture — NOT introduced by the DW rules). Three shapes of
//	ALTER TABLE ... ADD CONSTRAINT are dropped by the reducer, and
//	AdventureWorksDW's real DDL uses all three, so the warehouse's three real
//	primary keys and all eight of its real foreign keys are invisible to every
//	DB and DW rule.
//	Consequence beyond the DW family: DB-050 ("table without a primary key")
//	AFFIRMS, at confidence 1.0, that all three tables lack a primary key — a
//	false affirmation over DDL that plainly declares them. See
//	TestSQLDDL_TSQLAlterAddConstraint_DeclaredLimits below for the isolated,
//	minimal reproduction of each shape.

// awdwSchema parses the vendored AdventureWorksDW excerpt under the SQLServer
// dialect, optionally applying declared textual mutations first.
func awdwSchema(t *testing.T, replacements ...string) *db.Schema {
	t.Helper()
	path := filepath.Join("testdata", "tsql", "adventureworksdw_real_objects.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(content)
	if len(replacements) > 0 {
		text = strings.NewReplacer(replacements...).Replace(text)
	}
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: path, Content: []byte(text)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

// kimballToSnakeCase is a DECLARED MUTATION of the vendored file:
// AdventureWorksDW's PascalCase Kimball names rewritten to the snake_case
// prefixes codefit's S1 role detection recognizes. Nothing else changes — the
// columns and the DDL stay Microsoft's real ones. This is the same "mutate a
// copy of real DDL rather than invent a fixture" discipline the DB-011b
// coverage prose already documents.
var kimballToSnakeCase = []string{
	"[FactInternetSales]", "[fact_internet_sales]",
	"[DimCustomer]", "[dim_customer]",
	"[DimDate]", "[dim_date]",
	"[DimCurrency]", "[dim_currency]",
	"[DimProduct]", "[dim_product]",
	"[DimPromotion]", "[dim_promotion]",
	"[DimSalesTerritory]", "[dim_sales_territory]",
}

// LIMIT 1, locked: PascalCase Kimball naming carries no recognized prefix, so
// the canonical reference warehouse classifies as pure OLTP and no DW rule
// reaches it. Whoever broadens the prefix vocabulary will see this go red —
// which is exactly the point of locking it.
func TestDW_AdventureWorksDW_PascalCaseNaming_ClassifiesUnclassified(t *testing.T) {
	s := awdwSchema(t)
	if len(s.Tables) != 3 {
		t.Fatalf("parsed %d tables, want 3 (DimCustomer, DimDate, FactInternetSales)", len(s.Tables))
	}
	c := paradigm.Detect(s)
	if c.Paradigm != paradigm.ParadigmOLTP {
		t.Errorf("Paradigm = %q, want %q (no table name carries a recognized prefix)", c.Paradigm, paradigm.ParadigmOLTP)
	}
	for name, role := range c.Roles {
		if role != paradigm.RoleUnclassified {
			t.Errorf("role[%s] = %q, want %q", name, role, paradigm.RoleUnclassified)
		}
	}
	if _, surf := dwrules.Run(s, &c); len(surf) != 0 {
		t.Errorf("DW surface = %d items, want 0 — no DW rule may reach an unclassified table", len(surf))
	}
}

// LIMIT 2 on the real corpus: even after renaming the tables so role detection
// CAN see them, AdventureWorksDW still classifies OLTP — because the parser
// dropped every primary key and all eight foreign keys, so fact_internet_sales
// shows a fan-out of 0 (below the corroboration threshold) and the dimensions
// show a fan-in of 0. The star is real in the DDL and invisible in the model.
func TestDW_AdventureWorksDW_SnakeCaseRenamed_StarStillInvisible_DeclaredLimit(t *testing.T) {
	s := awdwSchema(t, kimballToSnakeCase...)

	fact := tableNamed(t, s, "fact_internet_sales")
	if len(fact.ForeignKeys) != 0 {
		t.Errorf("fact_internet_sales foreign keys = %d, want 0 — the DDL declares 8, and ALL of them are "+
			"lost: the block puts a newline between ADD and CONSTRAINT (which drops the first) and chains "+
			"the rest by comma (which drops those too). LIMIT 2", len(fact.ForeignKeys))
	}
	for _, name := range []string{"fact_internet_sales", "dim_customer", "dim_date"} {
		if pk := tableNamed(t, s, name).PrimaryKey; len(pk) != 0 {
			t.Errorf("%s primary key = %v, want empty — WITH CHECK ADD CONSTRAINT is dropped (LIMIT 2)", name, pk)
		}
	}

	c := paradigm.Detect(s)
	if c.Paradigm != paradigm.ParadigmOLAP {
		t.Logf("KNOWN LIMIT: Paradigm = %q (not olap) because the parsed model has no keys to corroborate with", c.Paradigm)
	} else {
		t.Error("Paradigm is olap — LIMIT 2 appears FIXED; re-point this fixture at the real assertions " +
			"(DW-001/002/005 negatives on a real, correctly modelled star) and delete this limit lock")
	}
}

// db-model-completeness-contract (2026-07-30): this test used to lock a KNOWN
// BUG — DB-050 deterministically AFFIRMED (confidence 1.0) that these three
// tables have no primary key, over real Microsoft-authored DDL that plainly
// declares one for each. That was the motivating false affirmation for the
// whole change (proposal SS1). It is REWRITTEN, not deleted: the three ALTER
// TABLE ... ADD CONSTRAINT shapes are STILL dropped (LIMIT 2 above is
// unchanged, deliberately deferred parser-shape debt) — what changed is that
// the drop is now RECORDED (D2), so DB-050 ROUTES to a dedicated surface item
// instead of affirming (D4/D5, design SS4/SS5). This is the "AdventureWorksDW
// yields zero false affirmations" success criterion from the proposal, made
// executable.
func TestDB050_AdventureWorksDW_NoFalseAffirmation_RoutesToSurfaceInstead(t *testing.T) {
	s := awdwSchema(t)
	fs, surf := dbrules.Run(s)

	for _, f := range fs {
		if f.ID == "DB-050" {
			t.Errorf("DB-050 affirmed %q — must route to surface instead of affirming over unproven structure", f.Description)
		}
	}

	var routed []findings.SurfaceItem
	for _, it := range surf {
		if it.Category == string(surface.CategoryDBTableStructureUnproven) {
			routed = append(routed, it)
		}
	}
	if len(routed) != 3 {
		t.Fatalf("db-table-structure-unproven surface items = %d, want 3 (DimCustomer, DimDate, FactInternetSales). "+
			"If this changed, LIMIT 2's ALTER TABLE ADD CONSTRAINT shapes may have been fixed or regressed — "+
			"re-derive this count from the fixture rather than adjusting it blindly. Got: %+v", len(routed), routed)
	}
	for _, it := range routed {
		if it.StructuralFacts["table_structure_proven_complete"] {
			t.Errorf("item for line %d: table_structure_proven_complete = true, want false", it.Line)
		}
		hasUnreducedStatement := false
		for _, sig := range it.StructuralSignals {
			if strings.HasPrefix(sig, "unreduced_statement: ") {
				hasUnreducedStatement = true
			}
		}
		if !hasUnreducedStatement {
			t.Errorf("item for line %d carries no unreduced_statement signal — the agent needs the raw DDL to judge for itself", it.Line)
		}
	}
}

// The three ALTER TABLE ... ADD CONSTRAINT shapes the T-SQL reducer drops,
// isolated to minimal DDL so the gap is reproducible without the 200-line
// fixture. Each subtest asserts TODAY's behavior and names what it should be.
func TestSQLDDL_TSQLAlterAddConstraint_DeclaredLimits(t *testing.T) {
	const table = "CREATE TABLE [dbo].[f]([a] [int] NOT NULL,[b] [int] NOT NULL);\nGO\n"
	parse := func(t *testing.T, src string) db.Table {
		t.Helper()
		p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
		s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(src)}})
		if err != nil {
			t.Fatalf("ParseSchema: %v", err)
		}
		return tableNamed(t, s, "f")
	}

	// (a) The supported baseline — proves the gaps below are about SHAPE, not
	// about ALTER TABLE or bracketed identifiers being unsupported in general.
	t.Run("supported: single-space ADD CONSTRAINT, one constraint", func(t *testing.T) {
		got := parse(t, table+"ALTER TABLE [dbo].[f] ADD CONSTRAINT [fk1] FOREIGN KEY ([a]) REFERENCES [dbo].[d1] ([a]);\nGO\n")
		if len(got.ForeignKeys) != 1 {
			t.Errorf("foreign keys = %d, want 1", len(got.ForeignKeys))
		}
	})

	// (b) T-SQL's WITH CHECK / WITH NOCHECK prefix — the shape AdventureWorksDW
	// uses for every primary key.
	t.Run("limit: WITH CHECK ADD CONSTRAINT is skipped", func(t *testing.T) {
		got := parse(t, table+"ALTER TABLE [dbo].[f] WITH CHECK ADD CONSTRAINT [pk] PRIMARY KEY CLUSTERED ([a]) ON [PRIMARY];\nGO\n")
		if len(got.PrimaryKey) != 0 {
			t.Errorf("primary key = %v, want empty (known limit). If this now parses, the gap is fixed — "+
				"retire this subtest and the matching NotCovered entry", got.PrimaryKey)
		}
	})

	// (c) A newline (rather than a single space) between ADD and CONSTRAINT —
	// the shape AdventureWorksDW uses for its foreign-key block.
	t.Run("limit: newline between ADD and CONSTRAINT is skipped", func(t *testing.T) {
		got := parse(t, table+"ALTER TABLE [dbo].[f] ADD\n    CONSTRAINT [fk1] FOREIGN KEY ([a]) REFERENCES [dbo].[d1] ([a]);\nGO\n")
		if len(got.ForeignKeys) != 0 {
			t.Errorf("foreign keys = %v, want none (known limit)", got.ForeignKeys)
		}
	})

	// (d) A comma-chained constraint list — only the first survives, because
	// the later parts start at CONSTRAINT with no repeated ADD.
	t.Run("limit: comma-chained constraints keep only the first", func(t *testing.T) {
		got := parse(t, table+"ALTER TABLE [dbo].[f] ADD CONSTRAINT [fk1] FOREIGN KEY ([a]) REFERENCES [dbo].[d1] ([a]), "+
			"CONSTRAINT [fk2] FOREIGN KEY ([b]) REFERENCES [dbo].[d2] ([b]);\nGO\n")
		if len(got.ForeignKeys) != 1 {
			t.Errorf("foreign keys = %d, want 1 (known limit — the DDL declares 2)", len(got.ForeignKeys))
		}
	})
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
