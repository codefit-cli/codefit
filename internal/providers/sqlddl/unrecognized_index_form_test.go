package sqlddl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// parser-records-unrecognized-drops (ADR 0034 SS2.4): apply()'s own default:
// branch — the top-level statement-kind dispatch, not a sub-statement
// dispatch like applyAlterAction's — currently has an EMPTY body, so a
// CREATE INDEX-shaped statement reCreateIndex's grammar does not cover
// (COLUMNSTORE, an anonymous index, ON ONLY) is silently discarded and its
// table's Complete stays true, violating StructureProven()'s own documented
// contract. These tests lock the fix: the dispatch has no branch for these
// forms, so per ADR 0034 SS2.4 ("Only a GENUINELY unrecognized statement ...
// marks a table unproven") they must mark their table unproven.

// TestSQLDDL_UnrecognizedIndexForms_Fixture_MarkAllThreeTablesUnproven is the
// AUTHORED fixture regression (mirrors n2_fixture_test.go's own discipline):
// before this fixture existed, COLUMNSTORE / anonymous CREATE INDEX / ON
// ONLY appeared ZERO times anywhere in this package's testdata/, so a test
// written against the existing corpus would have passed VACUOUSLY.
func TestSQLDDL_UnrecognizedIndexForms_Fixture_MarkAllThreeTablesUnproven(t *testing.T) {
	path := filepath.Join("testdata", "pg_constructed_unrecognized_index_forms.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	// Positive control on the FIXTURE'S OWN CONTENT (not its name): prove
	// the shapes this test claims to exercise actually exist verbatim in the
	// file under test.
	text := string(content)
	for _, want := range []string{
		"COLUMNSTORE", "CREATE INDEX ON event_log", "ON ONLY metrics_partitioned",
		"CREATE FULLTEXT INDEX", "CREATE SPATIAL INDEX", "CREATE XML INDEX", "CREATE PRIMARY XML INDEX",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("fixture %s does not contain %q — the shape this test claims to exercise is missing", path, want)
		}
	}

	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: path, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	wantTables := []string{"fact_sales", "event_log", "metrics_partitioned", "articles", "venues", "documents", "catalog_items"}
	if len(s.Tables) != len(wantTables) {
		t.Fatalf("parsed %d tables, want %d (%v)", len(s.Tables), len(wantTables), wantTables)
	}

	byName := map[string]db.Table{}
	for _, tb := range s.Tables {
		byName[tb.Name] = tb
	}

	for _, name := range wantTables {
		tb, ok := byName[name]
		if !ok {
			t.Fatalf("table %s not found (tables: %v)", name, s.Tables)
		}
		if tb.Complete {
			t.Errorf("table %s: Complete = true, want false — its CREATE INDEX form is genuinely unrecognized by the dispatch", name)
		}
		if !strings.Contains(tb.Note, string(db.ReasonUnreducedTableStatement)) {
			t.Errorf("table %s: Note = %q, want it to contain ReasonUnreducedTableStatement", name, tb.Note)
		}
		if len(tb.Unreduced) != 1 {
			t.Errorf("table %s: Unreduced = %v, want exactly one entry", name, tb.Unreduced)
		}
	}

	assertUnreducedTextContains(t, byName["fact_sales"], "COLUMNSTORE")
	assertUnreducedTextContains(t, byName["event_log"], "CREATE INDEX ON event_log")
	assertUnreducedTextContains(t, byName["metrics_partitioned"], "ON ONLY metrics_partitioned")
	// REL-001 (4R reliability lens): FULLTEXT/SPATIAL/XML/PRIMARY XML are a
	// standalone-statement gap of the SAME shape COLUMNSTORE closed above —
	// this package already knows FULLTEXT/SPATIAL as inline/ADD index
	// vocabulary (isInlineKeyIndexForm/isAddKeyIndexForm) but had no branch
	// for the standalone CREATE form.
	assertUnreducedTextContains(t, byName["articles"], "CREATE FULLTEXT INDEX")
	assertUnreducedTextContains(t, byName["venues"], "CREATE SPATIAL INDEX")
	assertUnreducedTextContains(t, byName["documents"], "CREATE XML INDEX")
	assertUnreducedTextContains(t, byName["catalog_items"], "CREATE PRIMARY XML INDEX")
}

// assertUnreducedTextContains guards the Unreduced[0] access so a table that
// FAILED to record anything (the pre-fix, RED state) reports a clean
// assertion failure instead of an index-out-of-range panic.
func assertUnreducedTextContains(t *testing.T, tb db.Table, want string) {
	t.Helper()
	if len(tb.Unreduced) == 0 {
		t.Errorf("table %s: Unreduced is empty, want an entry containing %q", tb.Name, want)
		return
	}
	if !strings.Contains(tb.Unreduced[0].Text, want) {
		t.Errorf("table %s: Unreduced[0].Text = %q, want it to contain %q", tb.Name, tb.Unreduced[0].Text, want)
	}
}

// TestSQLDDL_UnrecognizedIndexForm_NoAttributableTable_RecordsOnSchema
// covers the "when unsure, prefer Schema.Unreduced" boundary: an
// index-shaped statement with no parseable ON <table> clause at all cannot
// be safely attributed to any table (a wrong attribution is worse than
// none), so it is recorded on Schema.Unreduced instead — gating nothing
// per-table.
func TestSQLDDL_UnrecognizedIndexForm_NoAttributableTable_RecordsOnSchema(t *testing.T) {
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(
		"CREATE TABLE t (id int);\nCREATE INDEX idx USING gist (id);\n",
	)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Unreduced) != 1 {
		t.Fatalf("Schema.Unreduced = %d entries, want 1", len(s.Unreduced))
	}
	if !strings.Contains(s.Unreduced[0].Text, "USING gist") {
		t.Errorf("Schema.Unreduced[0].Text = %q, want it to carry the verbatim statement", s.Unreduced[0].Text)
	}
	if !s.Tables[0].Complete {
		t.Error("Complete = false on table t, want true — an unattributable statement gates NOTHING per-table")
	}
}

// TestSQLDDL_UnrecognizedIndexForm_TableNeverDeclared_MarksReasonTableNeverDeclared
// covers REL-002 (4R reliability lens): when the attributed table did NOT
// previously exist (no CREATE TABLE ever declared it — this genuinely
// unrecognized CREATE INDEX form is the FIRST time the name is seen), the
// two sibling call sites for the identical situation
// (applyAlterTable/applyCreateIndex, both via getTable's created bool) mark
// db.ReasonTableNeverDeclared, not db.ReasonUnreducedTableStatement — a
// materially different claim reaching the agent through
// routeUnprovenTable's "reason: "+t.Note (dbrules/rules.go): "no CREATE
// TABLE was ever seen for this table" versus "a statement affecting this
// table could not be reduced". Using the wrong reason here would tell the
// agent to go re-read a table's DDL that was never in the scanned schema at
// all.
func TestSQLDDL_UnrecognizedIndexForm_TableNeverDeclared_MarksReasonTableNeverDeclared(t *testing.T) {
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(
		// No CREATE TABLE for phantom_fact anywhere in this file.
		"CREATE CLUSTERED COLUMNSTORE INDEX cci ON phantom_fact;\n",
	)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 1 || s.Tables[0].Name != "phantom_fact" {
		t.Fatalf("parsed tables = %v, want exactly one table named phantom_fact (materialized by the drop site)", s.Tables)
	}
	tb := s.Tables[0]
	if tb.Complete {
		t.Error("Complete = true, want false")
	}
	if !strings.Contains(tb.Note, string(db.ReasonTableNeverDeclared)) {
		t.Errorf("Note = %q, want it to contain ReasonTableNeverDeclared — no CREATE TABLE for phantom_fact was ever seen", tb.Note)
	}
	if strings.Contains(tb.Note, string(db.ReasonUnreducedTableStatement)) {
		t.Errorf("Note = %q, want it NOT to also contain ReasonUnreducedTableStatement — the table's non-existence, not a mid-statement drop, is the real cause here", tb.Note)
	}
}
