package sqlddl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// F1 (coordinator review, index-method-capture): applyTableConstraint builds
// db.Index with no Method for MySQL's inline UNIQUE/KEY/INDEX/FULLTEXT/
// SPATIAL table-constraint forms, even when the source declares one via a
// trailing (or leading) USING clause — the "omission" class (obs #1307): the
// statement parses fine, the value is just never extracted. ALTER TABLE ...
// ADD KEY/UNIQUE ... USING routes through the SAME applyTableConstraint call
// site, so it is fixed by the same change.
//
// Real vendored Sakila (excerpt AND real_objects) contains ZERO USING clauses
// anywhere, so a test written against that corpus would pass vacuously — a
// CONSTRUCTED fixture is required (testdata/mysql/constructed_inline_index_
// using.sql), grep-verified below to actually contain the shapes this test
// claims to exercise, not merely assumed from its filename.
func TestSQLDDL_MySQL_InlineIndexUsing_Fixture_CapturesMethod(t *testing.T) {
	path := filepath.Join("testdata", "mysql", "constructed_inline_index_using.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(content)
	for _, want := range []string{
		"UNIQUE KEY uq_widgets_sku (sku) USING BTREE",
		"KEY idx_widgets_label (label) USING HASH",
		"ADD KEY idx_gadgets_code (code) USING BTREE",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("fixture %s does not contain %q — the shape this test claims to exercise is missing", path, want)
		}
	}

	p := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: path, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}

	widgets := table(t, s, "widgets")
	if !widgets.Complete {
		t.Fatalf("widgets.Complete = false, want true. Note=%q Unreduced=%v", widgets.Note, widgets.Unreduced)
	}
	if len(widgets.Indexes) != 2 {
		t.Fatalf("widgets.Indexes = %v, want exactly 2", widgets.Indexes)
	}
	byCol := map[string]string{}
	for _, ix := range widgets.Indexes {
		if len(ix.Columns) != 1 {
			t.Fatalf("index %+v has unexpected column count", ix)
		}
		byCol[ix.Columns[0]] = ix.Method
	}
	if got := byCol["sku"]; got != "btree" {
		t.Errorf("UNIQUE KEY sku Method = %q, want %q", got, "btree")
	}
	if got := byCol["label"]; got != "hash" {
		t.Errorf("KEY label Method = %q, want %q", got, "hash")
	}

	gadgets := table(t, s, "gadgets")
	if !gadgets.Complete {
		t.Fatalf("gadgets.Complete = false, want true. Note=%q Unreduced=%v", gadgets.Note, gadgets.Unreduced)
	}
	if len(gadgets.Indexes) != 1 {
		t.Fatalf("gadgets.Indexes = %v, want exactly 1 (via ALTER TABLE ADD KEY)", gadgets.Indexes)
	}
	if got := gadgets.Indexes[0].Method; got != "btree" {
		t.Errorf("ADD KEY code Method = %q, want %q", got, "btree")
	}
}

// TestSQLDDL_InlineIndex_NoUsing_MethodEmpty is the positive control: an
// inline/ADD index with NO USING clause stays empty, not defaulted to a
// guessed value.
func TestSQLDDL_InlineIndex_NoUsing_MethodEmpty(t *testing.T) {
	src := "CREATE TABLE t (id int, KEY idx (id));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if len(tb.Indexes) != 1 {
		t.Fatalf("Indexes = %v, want 1", tb.Indexes)
	}
	if got := tb.Indexes[0].Method; got != "" {
		t.Errorf("Method = %q, want empty", got)
	}
}

// TestSQLDDL_PG_InlineUnique_NoUsing_Unaffected regression-locks the
// PostgreSQL inline UNIQUE case (no USING clause in that dialect's table
// constraint grammar) stays Method-empty and Unique=true, unaffected by the
// new capture logic.
func TestSQLDDL_PG_InlineUnique_NoUsing_Unaffected(t *testing.T) {
	src := "CREATE TABLE t (id int, email text, UNIQUE (email));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New().ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if len(tb.Indexes) != 1 {
		t.Fatalf("Indexes = %v, want 1", tb.Indexes)
	}
	if got := tb.Indexes[0].Method; got != "" {
		t.Errorf("Method = %q, want empty", got)
	}
	if !tb.Indexes[0].Unique {
		t.Error("Unique = false, want true")
	}
}
