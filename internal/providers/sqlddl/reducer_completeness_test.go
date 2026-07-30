package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// D2 (design §2) — what the reducer records as an unproven drop, and what it
// deliberately does NOT (a declared, recognized skip is not incompleteness —
// recording those would mute DB-050 across ordinary PostgreSQL, N2).

func parsePGTable(t *testing.T, sql string) db.Table {
	t.Helper()
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	for _, tb := range s.Tables {
		if tb.Name == "t" {
			return tb
		}
	}
	t.Fatalf("table t not found in parsed schema (tables: %v)", s.Tables)
	return db.Table{}
}

// Site: applyCreateTable malformed body (reduce.go, ~L241-244) — an
// unbalanced CREATE TABLE(...) still registers the table (as before), but now
// marks it unproven instead of silently leaving Complete at its construction-
// site default.
func TestSQLDDL_MalformedCreateTableBody_MarksUnproven(t *testing.T) {
	p := sqlddl.New()
	// Deliberately unbalanced: the opening '(' never closes.
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(
		"CREATE TABLE t (id int, name varchar(50)\n",
	)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 1 {
		t.Fatalf("parsed %d tables, want 1 (still registered)", len(s.Tables))
	}
	tb := s.Tables[0]
	if tb.Complete {
		t.Error("Complete = true, want false — a malformed CREATE TABLE body cannot be proven complete")
	}
	if !strings.Contains(tb.Note, db.ReasonMalformedTableBody) {
		t.Errorf("Note = %q, want it to contain ReasonMalformedTableBody", tb.Note)
	}
}

// Site: applyTableConstraint — the recognized-skip branch (CHECK/EXCLUDE/
// PARTITION, ADR 0018's declared subset) MUST stay silent. Recording these
// would mute DB-050 on virtually every real PostgreSQL schema (N2).
func TestSQLDDL_TableConstraint_RecognizedSkip_StaysComplete(t *testing.T) {
	for _, sql := range []string{
		"CREATE TABLE t (id int, CHECK (id > 0));\n",
		"CREATE TABLE t (id int, EXCLUDE USING gist (id WITH =));\n",
	} {
		tb := parsePGTable(t, sql)
		if !tb.Complete {
			t.Errorf("sql=%q: Complete = false, want true — CHECK/EXCLUDE is a declared, recognized skip, not incompleteness (N2)", sql)
		}
		if tb.Note != "" {
			t.Errorf("sql=%q: Note = %q, want empty", sql, tb.Note)
		}
	}
}

// Site: applyAlterAction — the recognized-skip branch (ALTER COLUMN / RENAME
// / OWNER / ENABLE / DISABLE / CLUSTER / SET / RESET / VALIDATE / NO, the
// exact vocabulary named in reduce.go's own default-case comment) MUST stay
// silent. This is N2's fixture-authored positive control at the unit level:
// the reducer must NOT mute itself on ordinary PostgreSQL housekeeping DDL.
func TestSQLDDL_AlterAction_RecognizedSkip_StaysComplete(t *testing.T) {
	base := "CREATE TABLE t (id int);\n"
	for _, alter := range []string{
		"ALTER TABLE t OWNER TO postgres;\n",
		"ALTER TABLE t RENAME TO t2;\n",
		"ALTER TABLE t ENABLE ROW LEVEL SECURITY;\n",
		"ALTER TABLE t DISABLE TRIGGER ALL;\n",
		"ALTER TABLE t ALTER COLUMN id SET NOT NULL;\n",
		"ALTER TABLE t CLUSTER ON t_pkey;\n",
	} {
		tb := parsePGTable(t, base+alter)
		if !tb.Complete {
			t.Errorf("alter=%q: Complete = false, want true — a recognized ALTER TABLE housekeeping form is not incompleteness (N2)", alter)
		}
		if tb.Note != "" {
			t.Errorf("alter=%q: Note = %q, want empty", alter, tb.Note)
		}
	}
}

// Site: applyAlterAction default — a genuinely UNRECOGNIZED alter action MUST
// mark the table unproven, recording the verbatim statement.
func TestSQLDDL_AlterAction_UnrecognizedForm_MarksUnproven(t *testing.T) {
	base := "CREATE TABLE t (id int);\n"
	// INHERIT is PostgreSQL-legal ALTER TABLE syntax this reducer does not
	// recognize at all (not in the declared-skip vocabulary, not ADD/DROP).
	tb := parsePGTable(t, base+"ALTER TABLE t INHERIT parent_t;\n")
	if tb.Complete {
		t.Error("Complete = true, want false — an unrecognized ALTER TABLE action must mark the table unproven")
	}
	if !strings.Contains(tb.Note, db.ReasonUnreducedTableStatement) {
		t.Errorf("Note = %q, want it to contain ReasonUnreducedTableStatement", tb.Note)
	}
	if len(tb.Unreduced) != 1 || !strings.Contains(tb.Unreduced[0].Text, "INHERIT") {
		t.Errorf("Unreduced = %v, want one entry carrying the verbatim INHERIT action", tb.Unreduced)
	}
}

// Site: apply's own default (out-of-declared-subset statements — INSERT,
// GRANT, COMMENT, CREATE TYPE, ...) must NOT record anything: these are never
// classified as table-affecting at all, so there is no table to demote.
func TestSQLDDL_OutOfSubsetStatement_RecordsNothing(t *testing.T) {
	tb := parsePGTable(t, "CREATE TABLE t (id int);\nGRANT SELECT ON t TO reporting;\n")
	if !tb.Complete {
		t.Error("Complete = false, want true — GRANT is out of the declared subset and must never demote a table")
	}
	if tb.Note != "" {
		t.Errorf("Note = %q, want empty", tb.Note)
	}
}

// Site: applyAlterTable — a regex miss on the table name itself (the
// statement announced itself as "alter table" but failed to parse) cannot be
// attributed to any table, so it is recorded on Schema.Unreduced, never
// gating any per-table Complete.
func TestSQLDDL_AlterTable_UnattributableRegexMiss_RecordsOnSchema(t *testing.T) {
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(
		"CREATE TABLE t (id int);\nALTER TABLE\n",
	)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Unreduced) != 1 {
		t.Fatalf("Schema.Unreduced = %d entries, want 1", len(s.Unreduced))
	}
	if !s.Tables[0].Complete {
		t.Error("Complete = false on table t, want true — an unattributable statement gates NOTHING per-table")
	}
}

// N2 positive control (design §3c) — the vocabulary this test locks (OWNER
// TO, RENAME, ENABLE, ALTER COLUMN, CHECK) is real PostgreSQL and MUST leave
// Complete=true, exactly as TestSQLDDL_AlterAction_RecognizedSkip_StaysComplete
// and TestSQLDDL_TableConstraint_RecognizedSkip_StaysComplete lock above; this
// test additionally proves DB-050 still affirms a genuinely missing PK on
// that same table (the "honesty must not cost capability" requirement).
func TestSQLDDL_N2_RecognizedSkips_DoNotMuteDB050(t *testing.T) {
	tb := parsePGTable(t, "CREATE TABLE t (id int NOT NULL);\n"+
		"ALTER TABLE t OWNER TO postgres;\n"+
		"ALTER TABLE t ALTER COLUMN id SET NOT NULL;\n")
	if !tb.Complete {
		t.Fatalf("Complete = false, want true (N2 positive control)")
	}
	if len(tb.PrimaryKey) != 0 {
		t.Fatalf("PrimaryKey = %v, want empty (this table genuinely has none)", tb.PrimaryKey)
	}
}
