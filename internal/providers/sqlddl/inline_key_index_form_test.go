package sqlddl_test

import (
	"fmt"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// wantKind is the expected classification outcome for one matrix cell.
type wantKind int

const (
	wantColumn wantKind = iota
	wantIndex
	wantAbstain
)

// TestInlineKeyIndexForm is the design §3 matrix (M1): isInlineKeyIndexForm's
// column-vs-index discriminator, driven through the REAL parser (never a
// hand-built db.Table), crossed over every keyword the dispatch recognizes,
// every dialect, both call sites that consult it (applyTableItem's CREATE
// TABLE body and applyAlterAdd's ALTER TABLE ADD list — both call the SAME
// predicate on the SAME item text, reduce.go:1168/1729), and each of design
// §3's shapes.
//
// ONLY the no-parens/unmapped-type shape is expected to change behaviour
// (spec "Column vs. index-constraint classification MUST be paren-gated").
// Every other row locks BYTE-IDENTICAL behaviour before and after the D2 fix
// — a second failing row before the fix means the design is wrong, not that
// this test needs adjusting.
func TestInlineKeyIndexForm(t *testing.T) {
	keywords := []string{"KEY", "INDEX", "FULLTEXT", "SPATIAL"}

	type dialectSpec struct {
		name   string
		parser func() *sqlddl.Parser
	}
	dialects := []dialectSpec{
		{"postgresql", func() *sqlddl.Parser { return sqlddl.New() }},
		{"mysql", func() *sqlddl.Parser { return sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())) }},
		{"sqlserver", func() *sqlddl.Parser { return sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())) }},
	}

	type shape struct {
		name string
		item func(kw string) string
		want wantKind
	}
	shapes := []shape{
		{
			// Design §3 row 1 — THE FIX: `<kw> <unmapped-type> [modifiers]`, no
			// parens at all. tsvector is absent from all three TypeMaps (verified
			// against types.go), so this exercises the same shape across every
			// dialect identically.
			name: "no_parens_unmapped_type",
			item: func(kw string) string { return kw + " tsvector NOT NULL" },
			want: wantColumn,
		},
		{
			// Design §3 row 2 — a mapped type stays a column, unaffected by D2.
			name: "no_parens_mapped_type",
			item: func(kw string) string { return kw + " varchar(255) NOT NULL" },
			want: wantColumn,
		},
		{
			// Design §3 row 3a — unnamed parenthesized column list: the index
			// FORM, unaffected (base=="" early return, untouched by D2).
			name: "bare_paren_column_list",
			item: func(kw string) string { return kw + " (a, b)" },
			want: wantIndex,
		},
		{
			// Design §3 row 3b — named parenthesized column list: still the
			// index form (D2's tail!="" branch).
			name: "named_paren_column_list",
			item: func(kw string) string { return kw + " idx (a, b)" },
			want: wantIndex,
		},
		{
			// Design §3 row 3c — a named index with an explicit access method
			// between the name and the column list (MySQL's index_type
			// position). tail!="" keeps this the index form.
			name: "named_using_btree",
			item: func(kw string) string { return kw + " idx USING BTREE (a)" },
			want: wantIndex,
		},
		{
			// Design §3 row 4 — MySQL's legal unspaced form: the name and the
			// column list collapse into ONE token. Without the '('-in-token
			// clause this would misread as a column; with it, still an index.
			name: "unspaced_paren_column_list",
			item: func(kw string) string { return kw + " idx(a, b)" },
			want: wantIndex,
		},
		{
			// Spec "Malformed parens still abstain, never fabricate": parens
			// PRESENT but empty. base=="" (typeBase strips at the very first
			// '(', leaving nothing) so this hits the FIRST early return —
			// unaffected by D2 — and applyTableConstraint's own cols==0 guard
			// aborts to MarkUnproven rather than fabricating a zero-column
			// index (the FABRICATION GUARD, reduce.go:1475-1484).
			name: "malformed_empty_parens",
			item: func(kw string) string { return kw + " ()" },
			want: wantAbstain,
		},
	}

	type callSite struct {
		name string
		// wrap builds a full CREATE TABLE (+ optional ALTER TABLE ADD)
		// statement placing item at either the CREATE TABLE body call site
		// (applyTableItem) or the ALTER TABLE ADD call site (applyAlterAdd) —
		// both dispatch through the identical isInlineKeyIndexForm(item) call.
		wrap func(item string) string
	}
	callSites := []callSite{
		{
			name: "create_table_body",
			wrap: func(item string) string {
				return fmt.Sprintf("CREATE TABLE t (\n    id int NOT NULL,\n    %s\n);", item)
			},
		},
		{
			name: "alter_table_add",
			wrap: func(item string) string {
				return fmt.Sprintf("CREATE TABLE t (\n    id int NOT NULL\n);\nALTER TABLE t ADD %s;", item)
			},
		},
	}

	for _, kw := range keywords {
		for _, d := range dialects {
			for _, cs := range callSites {
				for _, sh := range shapes {
					name := kw + "/" + d.name + "/" + cs.name + "/" + sh.name
					t.Run(name, func(t *testing.T) {
						item := sh.item(kw)
						sql := cs.wrap(item)
						s, err := d.parser().ParseSchema([]providers.SourceFile{{Path: "matrix.sql", Content: []byte(sql)}})
						if err != nil {
							t.Fatalf("ParseSchema(%q): %v", sql, err)
						}
						tb := onlyTable(t, s, "t")
						assertShape(t, tb, kw, sh.want, sql)
					})
				}
			}
		}
	}
}

// TestInlineKeyIndexForm_QuotedIdentifierBypassesDispatch is M2: a QUOTED
// reserved-word identifier — MySQL-backtick-quoted, T-SQL bracket-quoted, or
// ANSI double-quoted — never reaches the bare, unquoted leadingKeyword check
// at all — quoting is
// canonicalized to a leading '"' by split() before reduce.go ever sees it
// (reduce.go:1183-1186's doc comment asserts this; nothing exercised it
// before this test).
func TestInlineKeyIndexForm_QuotedIdentifierBypassesDispatch(t *testing.T) {
	cases := []struct {
		dialect string
		parser  func() *sqlddl.Parser
		quoted  string
	}{
		{"postgresql", func() *sqlddl.Parser { return sqlddl.New() }, `"fulltext"`},
		{"mysql", func() *sqlddl.Parser { return sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())) }, "`fulltext`"},
		{"sqlserver", func() *sqlddl.Parser { return sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())) }, "[fulltext]"},
	}
	for _, c := range cases {
		t.Run(c.dialect, func(t *testing.T) {
			sql := fmt.Sprintf("CREATE TABLE t (\n    id int NOT NULL,\n    %s tsvector NOT NULL\n);", c.quoted)
			s, err := c.parser().ParseSchema([]providers.SourceFile{{Path: "quoted.sql", Content: []byte(sql)}})
			if err != nil {
				t.Fatalf("ParseSchema(%q): %v", sql, err)
			}
			tb := onlyTable(t, s, "t")
			if !tb.Complete {
				t.Fatalf("table t: Complete = false, want true (a quoted reserved word must read as an ordinary column, never the inline-index dispatch); note=%q", tb.Note)
			}
			if !hasColumn(tb, "fulltext") {
				t.Fatalf("table t: no column %q found; columns=%v", "fulltext", columnNames(tb))
			}
			if len(tb.Indexes) != 0 {
				t.Fatalf("table t: got %d indexes, want 0 (a quoted column must never be routed to applyTableConstraint)", len(tb.Indexes))
			}
		})
	}
}

func onlyTable(t *testing.T, s *db.Schema, name string) db.Table {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("no table %q in parsed schema (tables: %v)", name, tableNamesOf(s))
	return db.Table{}
}

func tableNamesOf(s *db.Schema) []string {
	var out []string
	for _, tb := range s.Tables {
		out = append(out, tb.Name)
	}
	return out
}

// hasColumn/columnNames reused from fabrication_test.go (same package).

// assertShape verifies one matrix cell against its expected outcome. kw is
// used lowercased as the expected column name for the wantColumn case
// (a real DDL column named key/index/fulltext/spatial, exactly what the
// dispatch must tell apart from the MySQL shorthand of the same spelling).
func assertShape(t *testing.T, tb db.Table, kw string, want wantKind, sql string) {
	t.Helper()
	// The captured column's name is the VERBATIM leading token as written in
	// the DDL (normalizeName strips quotes/schema qualifiers, never folds
	// case) — every item in this matrix writes kw UPPERCASE, so the expected
	// name is kw itself, not a lowercased guess.
	wantName := kw
	switch want {
	case wantColumn:
		if !tb.Complete {
			t.Fatalf("Complete = false, want true (column must be captured, not dropped); note=%q\nsql:\n%s", tb.Note, sql)
		}
		if !hasColumn(tb, wantName) {
			t.Fatalf("no column %q found; columns=%v\nsql:\n%s", wantName, columnNames(tb), sql)
		}
		if len(tb.Indexes) != 0 {
			t.Fatalf("got %d indexes, want 0 (must not fabricate an index)\nsql:\n%s", len(tb.Indexes), sql)
		}
	case wantIndex:
		if hasColumn(tb, wantName) {
			t.Fatalf("column %q was captured, want an INDEX instead (columns=%v)\nsql:\n%s", wantName, columnNames(tb), sql)
		}
		if len(tb.Indexes) != 1 {
			t.Fatalf("got %d indexes, want exactly 1\nsql:\n%s", len(tb.Indexes), sql)
		}
	case wantAbstain:
		if tb.Complete {
			t.Fatalf("Complete = true, want false (malformed parens must abstain)\nsql:\n%s", sql)
		}
		if hasColumn(tb, wantName) {
			t.Fatalf("column %q was fabricated from a malformed index form\nsql:\n%s", wantName, sql)
		}
		if len(tb.Indexes) != 0 {
			t.Fatalf("got %d indexes, want 0 (must abstain, never fabricate a zero-column index)\nsql:\n%s", len(tb.Indexes), sql)
		}
	}
}
