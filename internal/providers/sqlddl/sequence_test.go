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

// ADR 0044 — a SEQUENCE is not a table, and neither is a VIEW.
//
// pg_dump emits, for every serial/identity column, a CREATE SEQUENCE followed by
// the perfectly legal `ALTER TABLE public.<name>_id_seq OWNER TO <role>` (PG's
// ALTER TABLE accepts every relation kind for the ownership actions). The
// reducer had no branch for CREATE SEQUENCE, so the name was unknown when the
// ALTER TABLE arrived and getTable MATERIALIZED A TABLE from it: zero columns,
// StructureProven()==false, one Unreduced entry, and a routed
// db-table-structure-unproven surface item asking the agent whether a SEQUENCE
// declares a primary key — which a sequence cannot have.
//
// Measured on a real Spring/Hibernate pg_dump: 9 sequences produced 9 phantom
// tables, 9 of the run's 23 surface items (39%), and the per-scan note described
// them as "9 table(s)" whose structure codefit could not prove.

func parsePGFixture(t *testing.T, name string) *db.Schema {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	s, err := sqlddl.New().ParseSchema([]providers.SourceFile{{Path: name, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

// The fixture is verified by CONTENT, not by name: a sequence test written
// against a corpus with no CREATE SEQUENCE in it passes vacuously.
func TestSQLDDL_PGDumpSequenceFixture_ActuallyContainsSequences(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "pg_constructed_pgdump_sequences.sql"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	body := string(content)
	for _, want := range []string{
		"\nCREATE SEQUENCE public.users_id_seq",
		"\nCREATE SEQUENCE IF NOT EXISTS public.orders_id_seq",
		"\nALTER TABLE public.users_id_seq OWNER TO postgres;",
		"\nALTER TABLE public.orders_id_seq OWNER TO postgres;",
		"\nALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;",
		"\nSELECT pg_catalog.setval(",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fixture does not contain %q — the tests below would pass vacuously", want)
		}
	}
}

func TestSQLDDL_PGDumpSequence_MaterializesNoPhantomTable(t *testing.T) {
	s := parsePGFixture(t, "pg_constructed_pgdump_sequences.sql")
	for _, tb := range s.Tables {
		if strings.HasSuffix(tb.Name, "_id_seq") {
			t.Errorf("a SEQUENCE was materialized as a table: %q (cols=%d, proven=%v, note=%q)",
				tb.Name, len(tb.Columns), tb.StructureProven(), tb.Note)
		}
	}
	if got := tableNames(s); len(got) != 2 {
		t.Errorf("tables = %v, want exactly the two real ones (users, orders)", got)
	}
}

// The consequence the phantom actually had: every one of them was unproven, and
// the per-scan inventory therefore described sequences as unreadable TABLES.
func TestSQLDDL_PGDumpSequence_LeavesEveryTableProven(t *testing.T) {
	s := parsePGFixture(t, "pg_constructed_pgdump_sequences.sql")
	for _, tb := range s.Tables {
		if !tb.StructureProven() {
			t.Errorf("table %q is unproven (note=%q, unreduced=%d) — nothing in this fixture is unreadable",
				tb.Name, tb.Note, len(tb.Unreduced))
		}
	}
	if len(s.Unreduced) != 0 {
		t.Errorf("Schema.Unreduced = %v, want empty — a sequence declares no table structure, so nothing was lost", s.Unreduced)
	}
}

// A sequence is a DECLARED, RECOGNIZED skip (ADR 0034 §2.4), not a withheld
// declaration and not an unreduced one: it can never declare a column, key or
// index of any table, so recording it would be noise rather than honesty.
func TestSQLDDL_CreateSequence_IsASilentDeclaredSkip(t *testing.T) {
	s := parsePGFixture(t, "pg_constructed_pgdump_sequences.sql")
	if len(s.Withheld) != 0 {
		t.Errorf("Schema.Withheld = %+v, want empty", s.Withheld)
	}
}

// The VIEW half of the same mechanism, measured on the real Pagila corpus: 8 of
// its 21 phantom tables were views (actor_info, customer_list, film_list, …),
// materialized by the same `ALTER TABLE <name> OWNER TO` pg_dump writes for
// every relation it dumps.
func TestSQLDDL_ViewOwnerTo_MaterializesNoPhantomTable(t *testing.T) {
	sql := "CREATE VIEW public.actor_info AS SELECT 1 AS n;\n" +
		"ALTER TABLE public.actor_info OWNER TO postgres;\n"
	s, err := sqlddl.New().ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 0 {
		t.Errorf("tables = %v, want none — actor_info is a VIEW this parse already read", tableNames(s))
	}
	if len(s.Views) != 1 {
		t.Fatalf("views = %d, want 1", len(s.Views))
	}
}

// The THIRD statement that materializes a relation by reference: CREATE INDEX.
// PostgreSQL indexes materialized views, and pagila does — `CREATE UNIQUE INDEX
// rental_category ON public.rental_by_category` left the last phantom standing
// after the ALTER TABLE guard removed the other twenty.
//
// The index is NOT re-homed anywhere, and that is deliberate: db.View has no
// index field, because the DB dimension's rules are about TABLES. Inventing a
// table to hold it is exactly the fabrication class the completeness contract
// structurally cannot catch (ADR 0034 §2.6), and it is strictly worse than
// declaring that codefit read an index it has no place for.
func TestSQLDDL_IndexOnMaterializedView_MaterializesNoPhantomTable(t *testing.T) {
	sql := "CREATE MATERIALIZED VIEW public.rental_by_category AS SELECT 1 AS category;\n" +
		"ALTER TABLE public.rental_by_category OWNER TO postgres;\n" +
		"CREATE UNIQUE INDEX rental_category ON public.rental_by_category USING btree (category);\n"
	s, err := sqlddl.New().ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 0 {
		t.Errorf("tables = %v, want none — rental_by_category is a MATERIALIZED VIEW this parse already read", tableNames(s))
	}
}

// The same for a CREATE INDEX form the reducer's grammar cannot reduce at all:
// it must not attribute the drop to a fabricated table. It stays on the
// SCHEMA-level inventory, which gates nothing per-table (ADR 0034 §2.8).
func TestSQLDDL_UnrecognizedIndexFormOnView_RecordsAtSchemaLevel(t *testing.T) {
	sql := "CREATE VIEW public.v AS SELECT 1 AS a;\n" +
		"CREATE INDEX idx_v ON ONLY public.v (a);\n"
	s, err := sqlddl.New().ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 0 {
		t.Errorf("tables = %v, want none", tableNames(s))
	}
	if len(s.Unreduced) != 1 {
		t.Errorf("Schema.Unreduced = %+v, want exactly one entry — the statement was not read, and saying so is the point", s.Unreduced)
	}
}

// BOUNDARY / positive control, equal priority: the skip is driven by POSITIVE
// EVIDENCE that the name is a relation codefit already read and that is not a
// table — never by the action being a recognized skip. A name nothing declared
// still materializes with ReasonTableNeverDeclared, exactly as before, even when
// the only statement naming it is an OWNER TO that declares no structure at all.
// This is what distinguishes the chosen design from "OWNER TO never creates a
// table", which would have silently deleted this entry.
func TestSQLDDL_UnknownRelationOwnerTo_StillMaterializesNeverDeclared(t *testing.T) {
	sql := "ALTER TABLE public.ghost_table OWNER TO postgres;\n"
	tb := parsePGTableNamed(t, sql, "ghost_table")
	if tb.StructureProven() {
		t.Error("StructureProven() = true, want false — no CREATE TABLE was ever seen for this name")
	}
	if !strings.Contains(tb.Note, string(db.ReasonTableNeverDeclared)) {
		t.Errorf("Note = %q, want it to contain ReasonTableNeverDeclared", tb.Note)
	}
}

// A sequence declared AFTER the ALTER TABLE that names it cannot retroactively
// un-materialize the table the reducer already built — the reducer is an
// incremental fold over statements IN ORDER, and codefit reports what it read.
// Locked so the ordering assumption is visible rather than accidental.
func TestSQLDDL_AlterBeforeCreateSequence_StillMaterializes(t *testing.T) {
	sql := "ALTER TABLE public.late_seq OWNER TO postgres;\n" +
		"CREATE SEQUENCE public.late_seq START WITH 1;\n"
	tb := parsePGTableNamed(t, sql, "late_seq")
	if tb.StructureProven() {
		t.Error("StructureProven() = true, want false")
	}
}

// The NAME-COLLISION control, and it is the reason isKnownNonTable checks
// b.tables FIRST. PostgreSQL keeps tables, sequences and views in one relation
// namespace PER SCHEMA, not per database, and normalizeName strips the schema
// qualifier — so a view `public.x` and a table `other.x` collapse to the single
// name "x" in this reducer. When that happens the TABLE must win: it is the
// thing the model represents, and skipping its ALTER TABLE would silently drop a
// real primary key.
//
// Without the b.tables check in isKnownNonTable this ADD CONSTRAINT is skipped
// and the table reports no primary key at all — which DB-050 then affirms.
func TestSQLDDL_ViewAndTableCollideOnName_TheTableStillWins(t *testing.T) {
	sql := "CREATE VIEW public.x AS SELECT 1 AS n;\n" +
		"CREATE TABLE other.x (id int);\n" +
		"ALTER TABLE ONLY other.x ADD CONSTRAINT x_pkey PRIMARY KEY (id);\n"
	tb := parsePGTableNamed(t, sql, "x")
	if len(tb.PrimaryKey) != 1 || tb.PrimaryKey[0] != "id" {
		t.Errorf("PrimaryKey = %v, want [id] — the ALTER TABLE names the TABLE, not the view", tb.PrimaryKey)
	}
	if !tb.StructureProven() {
		t.Errorf("StructureProven() = false (note=%q), want true", tb.Note)
	}
}
