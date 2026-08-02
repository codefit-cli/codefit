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

// ADR 0045 — a body item's line anchor must be the line the item is WRITTEN on.
//
// The comma-separated parts of a CREATE TABLE body (and of an ALTER TABLE action
// list) carry the byte offset of the COMMA BOUNDARY, which sits BEFORE the
// newline that precedes the item's own text. Counting newlines up to that offset
// therefore anchored every item one line EARLY. Measured on a real pg_dump:
// DB-053 reported `column: password` at line 33, whose content is
// `lastname character varying(255),` — a different column entirely.
//
// This is not cosmetic. The baseline fingerprint is stamped from the CONTENT of
// the line at the anchor (sensors/db.stampFingerprints → findings.Fingerprint),
// so a finding's committed identity was bound to an unrelated column's text.

// assertAnchoredOn fails unless the source line at pos is the one containing
// want. It reads the SOURCE, so an off-by-one anchor cannot satisfy it.
func assertAnchoredOn(t *testing.T, src string, pos db.Pos, want, what string) {
	t.Helper()
	lines := strings.Split(src, "\n")
	if pos.Line < 1 || pos.Line > len(lines) {
		t.Fatalf("%s: line %d is out of range 1..%d", what, pos.Line, len(lines))
	}
	got := strings.TrimSpace(lines[pos.Line-1])
	if !strings.Contains(got, want) {
		t.Errorf("%s anchored on line %d, whose content is %q — want the line containing %q",
			what, pos.Line, got, want)
	}
}

const anchorTableSQL = `CREATE TABLE public.users (
    id bigint NOT NULL,
    firstname character varying(255),
    lastname character varying(255),
    password character varying(255),
    CONSTRAINT users_pkey PRIMARY KEY (id)
);
`

func TestSQLDDL_CreateTableColumns_AnchorOnTheirOwnSourceLine(t *testing.T) {
	tb := parsePGTableNamed(t, anchorTableSQL, "users")
	if len(tb.Columns) != 4 {
		t.Fatalf("columns = %d, want 4", len(tb.Columns))
	}
	for _, c := range tb.Columns {
		assertAnchoredOn(t, anchorTableSQL, c.Pos, c.Name, "column "+c.Name)
	}
}

func TestSQLDDL_CreateTableConstraint_AnchorsOnItsOwnSourceLine(t *testing.T) {
	tb := parsePGTableNamed(t, anchorTableSQL, "users")
	if len(tb.PrimaryKey) != 1 {
		t.Fatalf("primary key = %v, want one column", tb.PrimaryKey)
	}
	// The PK itself carries no Pos; the index/FK forms do. Use a table whose
	// last body item is a UNIQUE constraint so the anchor is observable.
	src := `CREATE TABLE public.t (
    a int,
    b int,
    UNIQUE (b)
);
`
	tb2 := parsePGTableNamed(t, src, "t")
	if len(tb2.Indexes) != 1 {
		t.Fatalf("indexes = %d, want 1", len(tb2.Indexes))
	}
	assertAnchoredOn(t, src, tb2.Indexes[0].Pos, "UNIQUE (b)", "unique constraint")
}

// The ALTER TABLE action list shares the same offset convention and the same
// defect. It goes unnoticed on pg_dump output because pg_dump writes ONE action
// per statement, and reAlterTable's own `\s+(.*)$` already consumes the newline
// before the first action — so only the SECOND and later actions of a
// multi-action statement are misanchored.
func TestSQLDDL_AlterTableMultiAction_EachActionAnchorsOnItsOwnSourceLine(t *testing.T) {
	src := `CREATE TABLE public.t (
    a int,
    b int
);
ALTER TABLE ONLY public.t
    ADD CONSTRAINT t_pkey PRIMARY KEY (a),
    ADD CONSTRAINT t_b_key UNIQUE (b);
`
	s, err := sqlddl.New().ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	var tb db.Table
	for _, x := range s.Tables {
		if x.Name == "t" {
			tb = x
		}
	}
	if len(tb.Indexes) != 1 {
		t.Fatalf("indexes = %d, want 1 (the UNIQUE added second)", len(tb.Indexes))
	}
	assertAnchoredOn(t, src, tb.Indexes[0].Pos, "t_b_key", "second ALTER action")
}

// The same invariant over EVERY .sql corpus vendored in this repository,
// through the REAL parser — the control that says the regenerated goldens hold
// the RIGHT line numbers and not merely DIFFERENT ones. A hand-built db.Table
// can carry a Pos the reducer never produces; this cannot.
//
// It asserts the weakest thing that is still decisive: the source line a column
// is anchored on must CONTAIN that column's name. That is false for an
// off-by-one on every multi-line body in the tree, and it needs no golden of its
// own to stay honest.
func TestSQLDDL_EveryVendoredCorpus_ColumnsAnchorOnTheirOwnSourceLine(t *testing.T) {
	corpora := vendoredCorpora(t)
	if len(corpora) < 10 {
		t.Fatalf("only %d corpora found under testdata/ — the walk is broken, not the tree", len(corpora))
	}
	checked := 0
	for _, rel := range corpora {
		content, err := os.ReadFile(filepath.Join("testdata", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		p := sqlddl.New(sqlddl.WithDialect(dialectFor(rel)))
		s, err := p.ParseSchema([]providers.SourceFile{{Path: rel, Content: content}})
		if err != nil {
			t.Fatalf("ParseSchema %s: %v", rel, err)
		}
		lines := strings.Split(string(content), "\n")
		for _, tb := range s.Tables {
			for _, c := range tb.Columns {
				if c.Pos.Line < 1 || c.Pos.Line > len(lines) {
					t.Errorf("%s: column %s.%s anchored on line %d, out of range 1..%d",
						rel, tb.Name, c.Name, c.Pos.Line, len(lines))
					continue
				}
				checked++
				if !strings.Contains(lines[c.Pos.Line-1], c.Name) {
					t.Errorf("%s: column %s.%s anchored on line %d, whose content is %q",
						rel, tb.Name, c.Name, c.Pos.Line, strings.TrimSpace(lines[c.Pos.Line-1]))
				}
			}
		}
	}
	if checked < 100 {
		t.Errorf("only %d columns were checked — the corpus tree cannot be that small, this assertion is passing by vacuity", checked)
	}
	t.Logf("checked %d column anchors across %d corpora", checked, len(corpora))
}

// Positive control, equal priority: the FIRST body item of a single-line
// CREATE TABLE has no preceding newline to skip, and must keep anchoring on the
// statement's own line — the fix must advance past whitespace, never add one.
func TestSQLDDL_SingleLineCreateTable_ColumnsAnchorOnTheStatementLine(t *testing.T) {
	src := "-- leading comment\nCREATE TABLE t (a int, b int);\n"
	tb := parsePGTableNamed(t, src, "t")
	for _, c := range tb.Columns {
		if c.Pos.Line != 2 {
			t.Errorf("column %s anchored on line %d, want 2 (everything is on one line)", c.Name, c.Pos.Line)
		}
	}
}
