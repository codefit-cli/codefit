package db_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// THE DEFECT THIS FILE CLOSES.
//
// A PostgreSQL dump produced by pg_dump under PowerShell is UTF-16LE with a
// byte-order mark. Run verbatim through the real Sensor.Audit, a schema
// declaring nine tables, nine primary keys and eleven foreign keys measured:
//
//	Measured=true  Score=100  Note=""  0 tables  0 findings  0 surface items
//
// No abstention floor fired, because nothing was DROPPED — the tokenizer never
// recognized a statement in the first place. The output is byte-for-byte the
// shape of a clean scan, and an agent cannot tell the two apart. This is the
// state db.go's own doc comment names as the thing codefit exists to catch.
//
// Every test below runs the REAL sensor over REAL files on disk. None builds a
// db.Schema by hand: a hand-built schema cannot exhibit this defect at all,
// because the defect lives entirely in the bytes-to-text step above the parser.

// schemaProject writes the named testdata fixtures into a temp project root
// under their own base names and returns an AuditContext with all of them
// configured as schema_paths, in the given order.
func schemaProject(t *testing.T, fixtures ...string) auditctx.AuditContext {
	t.Helper()
	root := t.TempDir()
	var paths []string
	for _, f := range fixtures {
		data, err := os.ReadFile(filepath.Join("testdata", f))
		if err != nil {
			t.Fatalf("reading fixture %s: %v", f, err)
		}
		if err := os.WriteFile(filepath.Join(root, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, f)
	}
	return writeCfg(t, root, paths)
}

// inlineProject writes literal file contents (name → bytes) into a temp project
// root and configures them as schema_paths in the given order.
func inlineProject(t *testing.T, order []string, files map[string][]byte) auditctx.AuditContext {
	t.Helper()
	root := t.TempDir()
	for _, name := range order {
		if err := os.WriteFile(filepath.Join(root, name), files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return writeCfg(t, root, order)
}

func writeCfg(t *testing.T, root string, paths []string) auditctx.AuditContext {
	t.Helper()
	yaml := "project:\n  name: enc\n  language: typescript\ndatabase:\n  schema_paths:\n"
	for _, p := range paths {
		yaml += "    - " + p + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("loading test .codefit.yaml: %v", err)
	}
	return auditctx.AuditContext{ProjectRoot: root, Config: cfg}
}

func auditEncodingProject(t *testing.T, ctx auditctx.AuditContext) sdb.Result {
	t.Helper()
	res, err := sdb.New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	return res
}

// shape is the parse outcome two encodings of the same DDL must agree on.
type shape struct {
	tables, proven, columns, pks, fks int
	names                             []string
}

func shapeOf(t *testing.T, r sdb.Result) shape {
	t.Helper()
	if r.Schema == nil {
		return shape{}
	}
	var s shape
	for _, tb := range r.Schema.Tables {
		s.tables++
		if tb.StructureProven() {
			s.proven++
		}
		s.columns += len(tb.Columns)
		if len(tb.PrimaryKey) > 0 {
			s.pks++
		}
		s.fks += len(tb.ForeignKeys)
		s.names = append(s.names, tb.Name)
	}
	return s
}

// The twin lock: the SAME DDL in UTF-16LE-with-BOM and in UTF-8 must produce
// the SAME schema. Asserting equality against the UTF-8 twin — rather than
// against literal numbers — is what keeps this test from going stale if the
// fixture is ever extended, and what makes it a statement about the ENCODING
// rather than about this particular DDL.
func TestSensorDB_Twin_UTF16LEWithBOM_ParsesIdenticallyToUTF8(t *testing.T) {
	want := shapeOf(t, auditEncodingProject(t, schemaProject(t, "twin_utf8.sql")))
	// Guard against a vacuous pass: the reference half must actually declare
	// something, or "equal" below would prove nothing at all.
	if want.tables != 2 || want.pks != 2 || want.fks != 1 {
		t.Fatalf("UTF-8 twin fixture is not the schema this test assumes: %+v", want)
	}

	got := shapeOf(t, auditEncodingProject(t, schemaProject(t, "twin_utf16le_bom.sql")))
	if got.tables != want.tables || got.proven != want.proven || got.columns != want.columns ||
		got.pks != want.pks || got.fks != want.fks || strings.Join(got.names, ",") != strings.Join(want.names, ",") {
		t.Fatalf("UTF-16LE+BOM parse = %+v, want the UTF-8 twin's %+v", got, want)
	}
}

// A schema codefit CAN read produces no unread trace at all. Without this
// negative control every assertion below could be satisfied by a note that is
// always printed, which would be noise rather than a signal.
func TestSensorDB_ReadableSchema_ProducesNoUnreadTrace(t *testing.T) {
	res := auditEncodingProject(t, schemaProject(t, "twin_utf8.sql"))
	if !res.Measured {
		t.Fatalf("Measured = false on a perfectly readable schema; note = %q", res.Note)
	}
	for _, phrase := range []string{"read NOTHING", "declares nothing outside"} {
		if strings.Contains(res.Note, phrase) {
			t.Errorf("note claims %q on a readable schema:\n%s", phrase, res.Note)
		}
	}
}

// THE DURABLE HALF. A BOM-less UTF-16 file is NOT decoded — codefit refuses to
// guess an encoding — and that refusal must not become the silence it replaced.
// The file is declared unread, and because it was the only configured source,
// the whole scan reports Measured=false: "audited, 0 findings" would be a lie.
func TestSensorDB_UTF16LENoBOM_IsDeclaredUnread_NotSilentlyClean(t *testing.T) {
	res := auditEncodingProject(t, schemaProject(t, "twin_utf16le_nobom.sql"))
	if res.Measured {
		t.Errorf("Measured = true over a file codefit read nothing from — that is the false all-clear")
	}
	if !strings.Contains(res.Note, "twin_utf16le_nobom.sql") {
		t.Errorf("note does not name the unread file:\n%s", res.Note)
	}
	if !strings.Contains(res.Note, "read NOTHING") {
		t.Errorf("note does not state that codefit read nothing:\n%s", res.Note)
	}
	if !strings.Contains(res.Note, "NUL") {
		t.Errorf("note does not say WHY the file could not be read as text:\n%s", res.Note)
	}
}

// The floor is written against the OUTCOME, not against a list of encodings: a
// file codefit reads as ordinary text and recognizes not one statement in lands
// on it too. This is the case no encoding table can enumerate — a future
// format, a corrupted file, a dialect nobody has written a branch for.
func TestSensorDB_TextThatYieldsNoStatement_IsDeclaredUnread(t *testing.T) {
	res := auditEncodingProject(t, inlineProject(t,
		[]string{"schema.sql"},
		map[string][]byte{"schema.sql": []byte("PRAGMA foreign_keys = ON;\nVACUUM FULL ANALYZE;\n")},
	))
	if res.Measured {
		t.Errorf("Measured = true over a file no statement of which reached any rule")
	}
	if !strings.Contains(res.Note, "schema.sql") || !strings.Contains(res.Note, "read NOTHING") {
		t.Errorf("note does not declare the file unread:\n%s", res.Note)
	}
	if strings.Contains(res.Note, "NUL") {
		t.Errorf("note blames an encoding for a file that decoded to perfectly good text:\n%s", res.Note)
	}
}

// A file that is genuinely empty, or holds nothing but comments, is LEGITIMATE.
// It is still recorded — so that "0 tables" is never ambiguous — but with its
// own wording, and it never flips Measured, because there was nothing to miss.
func TestSensorDB_EmptyAndCommentOnlyFiles_AreBenign_NotADefect(t *testing.T) {
	for name, body := range map[string]string{
		"empty":         "",
		"whitespace":    "\n\n   \t\n",
		"line comments": "-- nothing to declare yet\n-- (the first migration is next sprint)\n",
		"block comment": "/* staged for review\n   CREATE TABLE t (id int);\n*/\n",
	} {
		t.Run(name, func(t *testing.T) {
			res := auditEncodingProject(t, inlineProject(t,
				[]string{"schema.sql", "twin.sql"},
				map[string][]byte{
					"schema.sql": []byte(body),
					"twin.sql":   mustFixture(t, "twin_utf8.sql"),
				},
			))
			if !res.Measured {
				t.Errorf("Measured = false although a readable schema was configured alongside; note:\n%s", res.Note)
			}
			if !strings.Contains(res.Note, "declares nothing outside whitespace and comments") {
				t.Errorf("note does not record the empty file benignly:\n%s", res.Note)
			}
			if strings.Contains(res.Note, "read NOTHING") {
				t.Errorf("an empty file was reported as the defect:\n%s", res.Note)
			}
		})
	}
}

// PARTIAL is not TOTAL. When one source is readable and another is not, codefit
// DID audit something, so Measured stays true — and the note must still name
// the file it could not read, or the readable half would quietly speak for the
// whole schema.
func TestSensorDB_OneUnreadableSourceAmongReadableOnes_KeepsMeasuredAndNamesIt(t *testing.T) {
	res := auditEncodingProject(t, schemaProject(t, "twin_utf8.sql", "twin_utf16le_nobom.sql"))
	if !res.Measured {
		t.Fatalf("Measured = false although twin_utf8.sql was read; note:\n%s", res.Note)
	}
	if got := shapeOf(t, res); got.tables != 2 {
		t.Errorf("tables = %d, want the 2 from the readable half", got.tables)
	}
	if !strings.Contains(res.Note, "twin_utf16le_nobom.sql") {
		t.Errorf("note does not name the unreadable source:\n%s", res.Note)
	}
	if strings.Contains(res.Note, "twin_utf8.sql") {
		t.Errorf("note names a source that WAS read:\n%s", res.Note)
	}
}

// "Contributed" means contributed ANYTHING, not "declared a table". Ten of the
// twenty-eight corpora vendored in this repository declare only views,
// procedures and triggers and no table at all; reading the floor as
// "tables == 0" would report every one of them as unread. Measured through the
// real parser, not asserted about the implementation.
func TestSensorDB_FileDeclaringOnlyNonTableObjects_IsNotUnread(t *testing.T) {
	res := auditEncodingProject(t, inlineProject(t,
		[]string{"schema.sql"},
		map[string][]byte{"schema.sql": []byte(
			"CREATE VIEW active_customers AS SELECT 1;\n" +
				"CREATE FUNCTION touch() RETURNS trigger AS $$ BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;\n")},
	))
	if !res.Measured {
		t.Fatalf("Measured = false over a file whose objects codefit read fine; note:\n%s", res.Note)
	}
	if res.Schema == nil || len(res.Schema.Views)+len(res.Schema.Procedures) == 0 {
		t.Fatalf("fixture declares no view or routine the parser recognizes — the assertion below would pass vacuously")
	}
	if strings.Contains(res.Note, "read NOTHING") {
		t.Errorf("a file whose views and routines were read is reported unread:\n%s", res.Note)
	}
}

// The sharpest boundary in the floor: a file codefit read and DECLARED it could
// not reduce is the OPPOSITE of an unread one. ADR 0034 and ADR 0043 exist to
// produce exactly that record, and reporting it as "codefit read nothing here"
// would undo their work — telling an agent the parser was blind at precisely
// the point where it spoke up.
func TestSensorDB_FileWhoseOnlyTraceIsADeclaredDrop_IsNotUnread(t *testing.T) {
	res := auditEncodingProject(t, inlineProject(t,
		[]string{"schema.sql"},
		// CREATE FOREIGN TABLE lands on ADR 0043's table-shaped-head floor: no
		// table enters the model, one Schema.Unreduced record does.
		map[string][]byte{"schema.sql": []byte("CREATE FOREIGN TABLE remote_orders (id integer) SERVER s;\n")},
	))
	if res.Schema == nil || len(res.Schema.Unreduced) == 0 {
		t.Fatalf("fixture produced no Schema.Unreduced record — it no longer exercises the boundary this test names")
	}
	if !res.Measured {
		t.Errorf("Measured = false although codefit read the statement and declared it; note:\n%s", res.Note)
	}
	if strings.Contains(res.Note, "read NOTHING") {
		t.Errorf("a declared drop is being reported as an unread file:\n%s", res.Note)
	}
}

// The trace states the CONSEQUENCE, not just the fact. An agent that reads only
// the note must be able to tell "codefit read nothing from this file" apart
// from "codefit read this file and it is clean" — which is the entire point.
func TestSensorDB_UnreadNote_StatesTheConsequence(t *testing.T) {
	res := auditEncodingProject(t, schemaProject(t, "twin_utf16le_nobom.sql"))
	if !strings.Contains(res.Note, "not a clean bill of health") {
		t.Errorf("note does not say what its silence is NOT:\n%s", res.Note)
	}
}

func mustFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}
