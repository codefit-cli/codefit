package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	coredb "github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// THE HINGE THIS FILE LOCKS.
//
// reasonFor's `census.Total == 0` case is the ONLY thing standing between the
// statement census and a classifier that fails OPEN. The census is filled by the
// SQL-DDL reducer; no other schema parser fills it, and the Prisma parser — the
// one every .prisma project in the wild runs through — leaves db.Schema.Sources
// nil for every file it reads.
//
// A nil census is indistinguishable, ARITHMETICALLY, from a file whose every
// statement was explained: Total is 0, so Unaccounted() is 0, so
// "already-satisfied" is 0, and the classifier falls through to its last branch.
// Without the guard, that last branch is `reasonDeclaresNoSchema` — the BENIGN
// fact "they declare no schema at all", stated about a file codefit did not read
// one line of. Every traceless file under a census-less parser would flip from
// the blindness sentence to a reassuring one, which is the exact fail-open
// direction ADR 0068 §4 and invariant I2 forbid: a zero census must degrade to
// what codefit reported BEFORE the census existed — noisy, never a false
// all-clear.
//
// The guard is one line and deleting it broke no test anywhere in the repository
// until this file existed. An invariant with no mechanical control is an
// intention, not an invariant.
//
// Everything below drives the REAL production path: the real source reader
// (readSchemaSources), the real Prisma parser, the real classifier. A hand-built
// db.Schema with a nil Sources map would be a fixture asserting a shape nobody
// produced; here the census-less schema arrives the only way production ever
// produces one.

// A Prisma view block is the honest census-less blind spot: ParseSchema
// documents that it skips view blocks without error, so the file is read as
// perfectly good text, declares something, and leaves not one position behind.
const (
	prismaModelSource = "model User {\n" +
		"  id    Int    @id @default(autoincrement())\n" +
		"  email String @unique\n" +
		"}\n"
	prismaViewSource = "view ActiveUser {\n" +
		"  id    Int    @unique\n" +
		"  email String\n" +
		"}\n"
)

const (
	prismaModelPath = "schema.prisma"
	prismaViewPath  = "views.prisma"
)

// censusLessProject writes the two .prisma sources into a temp project root and
// returns the AuditContext that configures both as schema_paths, in order.
func censusLessProject(t *testing.T) auditctx.AuditContext {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		prismaModelPath: prismaModelSource,
		prismaViewPath:  prismaViewSource,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	yaml := "project:\n  name: censusless\n  language: typescript\ndatabase:\n  schema_paths:\n" +
		"    - " + prismaModelPath + "\n    - " + prismaViewPath + "\n"
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("loading test .codefit.yaml: %v", err)
	}
	return auditctx.AuditContext{ProjectRoot: root, Config: cfg}
}

// parseCensusLess runs the production reader and the production Prisma parser
// over the temp project, and returns both halves the classifier consumes.
func parseCensusLess(t *testing.T, ctx auditctx.AuditContext) (schemaResolution, *coredb.Schema) {
	t.Helper()
	resolution, err := readSchemaSources(ctx.ProjectRoot, ctx.Config.Database.SchemaPaths)
	if err != nil {
		t.Fatalf("readSchemaSources: %v", err)
	}
	schema, err := typescript.New().ParseSchema(resolution.sources())
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return resolution, schema
}

// The classification assertion, made against the reason VALUE rather than
// against the sentence it renders — wording is free to change; the verdict is
// not.
func TestSensorDB_CensusLessParser_TracelessFileIsClassifiedAsBlindness(t *testing.T) {
	ctx := censusLessProject(t)
	resolution, schema := parseCensusLess(t, ctx)

	// Anti-vacuity guards. Each one is a way this test could pass while
	// exercising nothing, so each is checked rather than assumed.
	if len(resolution.sources()) != 2 {
		t.Fatalf("read %d configured sources, want 2", len(resolution.sources()))
	}
	if len(schema.Sources) != 0 {
		t.Fatalf("the Prisma parser now fills db.Schema.Sources (%d entries) — this test no longer "+
			"exercises the census-less path and must be rewritten against a parser that still fills none",
			len(schema.Sources))
	}
	if got := schema.Sources[prismaViewPath]; got.Total != 0 || got.Unaccounted() != 0 {
		t.Fatalf("census for %s = %+v, want the zero census the guard exists for", prismaViewPath, got)
	}
	if !declaresSomething([]byte(prismaViewSource)) {
		t.Fatal("the view fixture declares nothing outside whitespace and comments — it would land on " +
			"reasonNoDeclarations and never reach the guard at all")
	}
	contributed := contributingFiles(schema)
	if !contributed[prismaModelPath] {
		t.Fatalf("%s contributed no position — the parse itself failed, so the assertion below would "+
			"prove nothing about the classifier", prismaModelPath)
	}
	if contributed[prismaViewPath] {
		t.Fatalf("%s left a position in the model, so it is not traceless and never reaches reasonFor",
			prismaViewPath)
	}

	unread, unproductive := unreadSources(resolution, schema)
	if len(unread) != 1 || unread[0].Path != prismaViewPath {
		t.Fatalf("unreadSources = %+v, want exactly one entry for %s", unread, prismaViewPath)
	}
	if unproductive != 1 {
		t.Fatalf("unproductive path count = %d, want 1 (views.prisma is its own configured path and "+
			"contributed nothing; schema.prisma contributed)", unproductive)
	}
	if unread[0].Reason != reasonNothingRecognized {
		t.Errorf("a file no parser position names, under a parser that accounts for NOTHING, is classified "+
			"%q — a zero census is not evidence that anything was explained, and reporting it as a benign "+
			"fact is the false all-clear the guard exists to prevent", unread[0].Reason)
	}
	if !unread[0].Reason.defect() {
		t.Errorf("the classification of a file codefit read nothing from is not a DEFECT, so the scan "+
			"reports it as an ordinary observation: %q", unread[0].Reason)
	}
}

// The same verdict, carried all the way to what the agent actually reads. The
// expected sentence is RENDERED from the reason rather than spelled out, so this
// test tracks the wording instead of freezing it — and still fails the moment the
// note switches to the other classification's sentence.
func TestSensorDB_CensusLessParser_NoteReportsTheBlindnessNotABenignFact(t *testing.T) {
	ctx := censusLessProject(t)

	res, err := New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !res.Measured {
		t.Fatalf("Measured = false although %s declared a table; note:\n%s", prismaModelPath, res.Note)
	}

	blind := unreadNote([]unreadSource{{Path: prismaViewPath, Reason: reasonNothingRecognized}}, 2, 2)
	benign := unreadNote([]unreadSource{{Path: prismaViewPath, Reason: reasonDeclaresNoSchema}}, 2, 2)
	if blind == benign {
		t.Fatal("the two classifications render the same sentence, so this test cannot tell them apart")
	}
	if !strings.Contains(res.Note, blind) {
		t.Errorf("the note does not report %s as blindness.\nwant it to contain:\n%s\ngot:\n%s",
			prismaViewPath, blind, res.Note)
	}
	if strings.Contains(res.Note, benign) {
		t.Errorf("the note reports %s with the benign \"declares no schema\" fact, about a file whose "+
			"content no rule ever saw:\n%s", prismaViewPath, res.Note)
	}
}

// The mirror control, and the reason the assertions above are about the CENSUS
// and not about the classifier being stuck on one verdict. The same classifier,
// the same "traceless and declaring" shape, under a parser that DOES fill the
// census: the pure-data file is correctly benign. The difference between the two
// tests is the evidence, which is the whole point of the guard.
func TestSensorDB_CensusFillingParser_TracelessDataFileStaysBenign(t *testing.T) {
	sources := []providers.SourceFile{
		{Path: "schema.sql", Content: []byte("CREATE TABLE role (id bigserial PRIMARY KEY, name text);\n")},
		{Path: "seed.sql", Content: []byte("INSERT INTO role (name) VALUES ('admin');\n")},
	}
	schema, err := sqlddl.New().ParseSchema(sources)
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if schema.Sources["seed.sql"].Total == 0 {
		t.Fatal("the SQL-DDL parser filled no census for seed.sql — the contrast this control draws does not exist")
	}

	// Each source is its own configured entry, which is how a two-file
	// schema_paths list resolves.
	resolution := schemaResolution{Paths: []resolvedPath{
		{Configured: sources[0].Path, Files: sources[:1]},
		{Configured: sources[1].Path, Files: sources[1:]},
	}}
	unread, unproductive := unreadSources(resolution, schema)
	if len(unread) != 1 || unread[0].Path != "seed.sql" {
		t.Fatalf("unreadSources = %+v, want exactly one entry for seed.sql", unread)
	}
	if unproductive != 1 {
		t.Fatalf("unproductive path count = %d, want 1 (seed.sql's entry contributed nothing)", unproductive)
	}
	if unread[0].Reason != reasonDeclaresNoSchema {
		t.Errorf("a traceless file whose every statement the reducer POSITIVELY explained is classified "+
			"%q instead of the benign fact it established", unread[0].Reason)
	}
}
