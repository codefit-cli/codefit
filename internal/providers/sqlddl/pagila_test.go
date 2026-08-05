package sqlddl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Real Pagila objects: the dollar-quoted function bodies ($$ and $_$) must not
// break statement splitting, and views/functions/triggers must populate the model
// (Name/Pos, and Table for triggers). Bodies/columns are NOT captured (declared
// limit for the future DB-020..041 rules).
func TestPagila_ViewsProcsTriggersPopulated(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "pagila_excerpt.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := sqlddl.New().ParseSchema([]providers.SourceFile{{Path: "pagila.sql", Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Views) != 1 {
		t.Errorf("views = %d, want 1 (actor_info)", len(s.Views))
	}
	if len(s.Procedures) != 2 {
		t.Errorf("procedures = %d, want 2 (_group_concat, last_updated) — dollar-quoted bodies must not split", len(s.Procedures))
	}
	if len(s.Triggers) != 2 {
		t.Fatalf("triggers = %d, want 2", len(s.Triggers))
	}
	// actor, plus the real index-bearing tables appended for the DB-011a/
	// DB-011b/Unit E fixture extension (architecture/pagila-fixture-real-indexes):
	// customer, address, rental, and the real payment_p2022_01 partition-child
	// table (the DB-011a positive-fire proof). film is RESTORED here
	// (sql-ddl-phantom-index, formerly omitted for discovery/sqlddl-postgres-
	// parser-gaps obs #1062, HALLAZGO 2 — see the inline note in
	// pagila_excerpt.sql for the fix and TestPagila_Film_FulltextColumnCaptured
	// below for its dedicated assertion). All parsed cleanly alongside the
	// dollar-quoted objects (splitting held).
	wantTables := []string{"actor", "customer", "film", "address", "rental", "payment_p2022_01"}
	gotTables := tableNames(s)
	if len(gotTables) != len(wantTables) {
		t.Fatalf("tables = %v, want %v", gotTables, wantTables)
	}
	for i, want := range wantTables {
		if gotTables[i] != want {
			t.Errorf("tables[%d] = %q, want %q (full: %v)", i, gotTables[i], want, gotTables)
		}
	}
	// Trigger.Table is parsed from the ON clause (schema qualifier stripped).
	var lastUpdated bool
	for _, tr := range s.Triggers {
		if tr.Name == "last_updated" {
			lastUpdated = true
			if tr.Table != "actor" {
				t.Errorf("last_updated trigger.Table = %q, want actor", tr.Table)
			}
		}
	}
	if !lastUpdated {
		t.Error("expected the last_updated trigger to be recognized")
	}
	// View names are populated (bodies are not captured — no field for them).
	if s.Views[0].Name != "actor_info" {
		t.Errorf("view name = %q, want actor_info", s.Views[0].Name)
	}
}

// TestPagila_Film_FulltextColumnCaptured is the load-bearing regression
// fixture for sql-ddl-phantom-index (spec "public.film MUST be vendored
// verbatim in the Pagila fixture"): on REAL, unmodified upstream Pagila DDL —
// not a constructed shape — the fulltext column must be captured, not
// dropped, and the table must prove structurally complete. Before the D2
// fix this failed: 13 of 14 columns (no fulltext) and StructureProven()
// false, because the fixture used to omit film entirely to dodge exactly
// this defect (see pagila_excerpt.sql's restoration note).
func TestPagila_Film_FulltextColumnCaptured(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "pagila_excerpt.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := sqlddl.New().ParseSchema([]providers.SourceFile{{Path: "pagila.sql", Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	var got *db.Table
	for i := range s.Tables {
		if s.Tables[i].Name == "film" {
			got = &s.Tables[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("no table named %q in parsed schema (tables: %v)", "film", tableNames(s))
	}
	wantCols := []string{
		"film_id", "title", "description", "release_year", "language_id",
		"original_language_id", "rental_duration", "rental_rate", "length",
		"replacement_cost", "rating", "last_update", "special_features", "fulltext",
	}
	if len(got.Columns) != len(wantCols) {
		t.Fatalf("film columns = %d %v, want %d %v", len(got.Columns), colNames(*got), len(wantCols), wantCols)
	}
	for i, want := range wantCols {
		if got.Columns[i].Name != want {
			t.Errorf("film column[%d] = %q, want %q (full: %v)", i, got.Columns[i].Name, want, colNames(*got))
		}
	}
	if !hasColumn(*got, "fulltext") {
		t.Errorf("film has no fulltext column; columns = %v", colNames(*got))
	}
	if !got.StructureProven() {
		t.Errorf("film.StructureProven() = false, want true; note=%q unreduced=%+v", got.Note, got.Unreduced)
	}
}
