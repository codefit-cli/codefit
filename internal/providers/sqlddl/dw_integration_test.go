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
// canonical reference warehouse still yields NO DW finding — but for ONE
// remaining reason, not the two it used to have.
//
//	LIMIT 1 (role NAME vocabulary) is GONE. It used to read: "AdventureWorksDW
//	uses PascalCase Kimball naming (FactInternetSales, DimCustomer), while
//	codefit's table-role detection recognizes only the snake_case prefixes
//	fact_/dim_/stg_/mart_." The paradigm role-name vocabulary now recognizes
//	PascalCase (and suffix, and case-insensitive) spellings, so all three of
//	this corpus's table names DO nominate a role. That advance is locked
//	positively by TestDW_AdventureWorksDW_PascalCaseNaming_IsNowRecognized
//	below, against the REAL parsed corpus — not asserted from a fixture.
//
//	LIMIT 2 (T-SQL ALTER reduction, a PRE-EXISTING parser gap discovered while
//	vendoring this fixture — NOT introduced by the DW rules) is UNCHANGED and
//	is now the SOLE reason the corpus yields nothing. Three shapes of
//	ALTER TABLE ... ADD CONSTRAINT are dropped by the reducer, and
//	AdventureWorksDW's real DDL uses all three, so the warehouse's three real
//	primary keys and all eight of its real foreign keys are invisible to every
//	DB and DW rule. With no keys in the model the fact candidate shows a
//	fan-out of 0 and the dimension candidates a fan-in of 0, so the A5
//	corroboration gate demotes all three back to "unclassified".
//	Consequence beyond the DW family: DB-050 ("table without a primary key")
//	AFFIRMS, at confidence 1.0, that all three tables lack a primary key — a
//	false affirmation over DDL that plainly declares them. See
//	TestSQLDDL_TSQLAlterAddConstraint_DeclaredLimits below for the isolated,
//	minimal reproduction of each shape.
//
// The rules' positive fire paths are therefore STILL proven by
// declared-synthetic constructed fixtures in the per-rule unit tests
// (internal/core/dwrules), per the ADR 0028 fixture-gap policy — this file is
// the honest record of how far the real corpus now reaches, and of the one
// gap that still stops it short.

// awdwSchema parses the vendored AdventureWorksDW excerpt under the SQLServer
// dialect, verbatim. It used to accept declared textual mutations so a caller
// could rename the tables into snake_case; nothing needs that any more (see
// TestDW_AdventureWorksDW_StarStillInvisible_DeclaredLimit), so every test in
// this file now reads Microsoft's DDL exactly as vendored.
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

// LIMIT 1 RETIRED, locked positively. This test replaces the LIMIT 1 lock
// (formerly ...PascalCaseNaming_ClassifiesUnclassified), which asserted that
// AdventureWorksDW's names carried no recognized prefix. That is no longer
// true, and — this is the part worth reading — the OLD ASSERTION KEPT PASSING
// ANYWAY once the vocabulary widened, because LIMIT 2 independently produces
// the same "everything unclassified" outcome. Its stated reason had gone
// false while its assertion stayed green: a lock that could no longer tell
// the difference between the thing it guarded and the thing next to it.
//
// So the assertion here is pinned to the one signal that DOES separate them.
// Classification.Unprovable is populated ONLY for a table whose name
// nominated a role and whose structural corroboration could not be proven; a
// table whose name nominates nothing is skipped outright. Membership is
// therefore the public, real-parser proof that the vocabulary now reaches
// Microsoft's own Kimball spelling — and it is exactly what regresses to an
// empty map if the vocabulary is narrowed back.
//
// Verified against main before this slice: the same corpus yielded
// Unprovable = map[] (names unrecognized) and now yields all three tables.
//
// It deliberately does NOT claim the DW family works end-to-end on real DDL.
// It does not: LIMIT 2 still starves corroboration, the roles below are still
// unclassified, and the DW surface below is still empty.
func TestDW_AdventureWorksDW_PascalCaseNaming_IsNowRecognized(t *testing.T) {
	s := awdwSchema(t)
	if len(s.Tables) != 3 {
		t.Fatalf("parsed %d tables, want 3 (DimCustomer, DimDate, FactInternetSales)", len(s.Tables))
	}
	c := paradigm.Detect(s)

	// THE ADVANCE: every real PascalCase name now nominates a role.
	for _, name := range []string{"FactInternetSales", "DimCustomer", "DimDate"} {
		if !c.Unprovable[name] {
			t.Errorf("Unprovable[%s] = false, want true — %s must be recognized by the role-name "+
				"vocabulary and demoted for UNPROVEN STRUCTURE (LIMIT 2), not skipped as an "+
				"unrecognized name. A false here means the PascalCase spelling regressed out of "+
				"the vocabulary", name, name)
		}
	}
	if len(c.Unprovable) != 3 {
		t.Errorf("Unprovable = %v (%d entries), want exactly the 3 warehouse tables", c.Unprovable, len(c.Unprovable))
	}

	// LIMIT 2, unchanged: recognized names still do not promote, because the
	// dropped keys leave the A5 corroboration gate nothing to corroborate with.
	if c.Paradigm != paradigm.ParadigmOLTP {
		t.Errorf("Paradigm = %q, want %q — LIMIT 2 still leaves the star invisible", c.Paradigm, paradigm.ParadigmOLTP)
	}
	for name, role := range c.Roles {
		if role != paradigm.RoleUnclassified {
			t.Errorf("role[%s] = %q, want %q — if this promoted, LIMIT 2 is FIXED: re-point this "+
				"file at real DW-001/002/005 assertions over a correctly modelled star", name, role, paradigm.RoleUnclassified)
		}
	}
	if _, surf := dwrules.Run(s, &c); len(surf) != 0 {
		t.Errorf("DW surface = %d items, want 0 — no DW rule may reach an unclassified table", len(surf))
	}
}

// LIMIT 2 in isolation, on the REAL corpus under its REAL names. This test
// used to run against a declared textual mutation (kimballToSnakeCase) that
// rewrote Microsoft's PascalCase names into snake_case, purely so role
// detection could see them at all. That workaround existed only to route
// around LIMIT 1; with LIMIT 1 retired the rename proves nothing the real
// names do not, so it is gone and the assertions moved onto the vendored DDL
// verbatim — strictly closer to the real parser, per the project's
// prefer-the-real-corpus rule. No assertion was weakened or dropped.
//
// What remains is the isolated structural cause: the parser drops every
// primary key and all eight foreign keys, so FactInternetSales shows a
// fan-out of 0 (below factFanOutMin) and the dimensions a fan-in of 0. The
// star is real in the DDL and invisible in the model.
func TestDW_AdventureWorksDW_StarStillInvisible_DeclaredLimit(t *testing.T) {
	s := awdwSchema(t)

	fact := tableNamed(t, s, "FactInternetSales")
	if len(fact.ForeignKeys) != 0 {
		t.Errorf("FactInternetSales foreign keys = %d, want 0 — the DDL declares 8, and ALL of them are "+
			"lost: the block puts a newline between ADD and CONSTRAINT (which drops the first) and chains "+
			"the rest by comma (which drops those too). LIMIT 2", len(fact.ForeignKeys))
	}
	for _, name := range []string{"FactInternetSales", "DimCustomer", "DimDate"} {
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
