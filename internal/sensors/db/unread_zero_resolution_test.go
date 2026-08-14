package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// THE HOLE THIS FILE LOCKS.
//
// The unread floor (unread.go) reasons about a configured schema FILE that
// contributed nothing. Its denominator was the count of files the resolver
// managed to produce, which is a quantity the resolver itself decides — so a
// configured PATH that resolved to no file at all was subtracted from both
// sides of the predicate and vanished. `wholeScanUnproductive` was handed
// total=0, its `total > 0` guard short-circuited, the floor never engaged, and
// the sensor reported Measured=true with score 100 over content codefit never
// read: a clean bill of health for a directory it never opened a byte of.
//
// The ordinary golang-migrate embed layout is exactly that shape — a package
// directory holding an embed.go and, in the case that matters, no .sql beside
// it. Case 2 below is worse, and is why the unit of account had to move rather
// than the guard being patched: with one good path and one empty one the scan
// IS legitimately measured, real findings survive, and on main the empty path
// was mentioned NOWHERE. That is a partial result that does not declare itself
// partial (invariant I4), and unlike Case 1 it looks completely normal.
//
// Every control here drives the REAL sensor over a real temp project with the
// real SQL-DDL parser. A hand-built []unreadSource would be worse than no test:
// production cannot produce an empty resolved-source slice next to a non-empty
// unread list, so such a fixture would lock a reality that does not exist.

// embedGoSource is the ordinary golang-migrate/embed package file: a directory
// entry that is unmistakably CONTENT, and unmistakably not a schema file the
// SQL-DDL resolver can order.
const embedGoSource = "package migrations\n\nimport \"embed\"\n\n//go:embed *.sql\nvar FS embed.FS\n"

// oneTableSchema is the contributing half of the partial fixture: a single
// table, declared through the real parser, whose findings must survive a scan
// that also names an empty path.
const oneTableSchema = "CREATE TABLE account (\n  id bigserial PRIMARY KEY,\n  email text NOT NULL\n);\n"

const (
	emptyDirPath  = "db/empty"
	realFilePath  = "db/real/0001_account.sql"
	realDirPath   = "db/real"
	singleDirPath = "db/migrations"
)

// assertResolvesToNoSQL is the CONTENT guard, and it is the difference between
// a control and an ornament. The fixture's whole claim is "this directory holds
// files but not one the resolver will take", and a test that never checks that
// claim passes just as green over a directory that quietly grew a .sql — at
// which point it is asserting the OPPOSITE case while still reporting success.
//
// It is written against the same predicate flywayOrderedSQL applies
// (case-insensitive .sql extension, subdirectories skipped), not against a
// guessed one, so a fixture the resolver would accept can never satisfy it.
func assertResolvesToNoSQL(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading fixture dir %s: %v", dir, err)
	}
	sql, other := 0, 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".sql") {
			sql++
			continue
		}
		other++
	}
	if sql != 0 {
		t.Fatalf("fixture %s holds %d .sql file(s), so it does NOT resolve to zero schema files — "+
			"this test would pass while exercising the opposite case", dir, sql)
	}
	if other == 0 {
		t.Fatalf("fixture %s holds no file at all — an empty directory is a different fixture, and this one "+
			"must prove codefit skipped CONTENT that exists", dir)
	}
}

// writeDBProject writes files into a temp root, writes a .codefit.yaml
// configuring paths as database.schema_paths IN ORDER, and returns the loaded
// AuditContext.
func writeDBProject(t *testing.T, files map[string]string, paths ...string) auditctx.AuditContext {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	yaml := "project:\n  name: zeroresolution\n  language: typescript\ndatabase:\n  schema_paths:\n"
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
	if len(cfg.Database.SchemaPaths) != len(paths) {
		t.Fatalf("config carries %d schema_paths, want %d — the fixture does not configure what it claims",
			len(cfg.Database.SchemaPaths), len(paths))
	}
	return auditctx.AuditContext{ProjectRoot: root, Config: cfg}
}

// zeroResolutionProject is Case 1: ONE configured path, a directory holding one
// .go file and zero .sql. On main this returned Measured=true, score 100.
func zeroResolutionProject(t *testing.T) auditctx.AuditContext {
	t.Helper()
	ctx := writeDBProject(t, map[string]string{
		singleDirPath + "/embed.go": embedGoSource,
	}, singleDirPath)
	assertResolvesToNoSQL(t, filepath.Join(ctx.ProjectRoot, filepath.FromSlash(singleDirPath)))
	return ctx
}

// partialResolutionProject is Case 2: two configured paths, one resolving to a
// real table and one to nothing. The scan is legitimately measured; on main the
// empty path appeared nowhere in the response.
func partialResolutionProject(t *testing.T) auditctx.AuditContext {
	t.Helper()
	ctx := writeDBProject(t, map[string]string{
		realFilePath:             oneTableSchema,
		emptyDirPath + "/go.mod": "module fixture\n",
	}, realDirPath, emptyDirPath)
	assertResolvesToNoSQL(t, filepath.Join(ctx.ProjectRoot, filepath.FromSlash(emptyDirPath)))
	return ctx
}

// C1. A configured path that resolved to no schema file at all makes the scan
// NOT a measurement. The mutation that breaks it is the code on main: compute
// the floor's denominator over resolved FILES instead of configured PATHS.
func TestSensorDB_ZeroResolutionDirectory_IsNotMeasured(t *testing.T) {
	ctx := zeroResolutionProject(t)

	res, err := New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit over a directory holding no .sql must not error: %v", err)
	}
	if res.Measured {
		t.Fatalf("a scan whose only configured path resolved to ZERO schema files reports Measured=true "+
			"with score %d — a clean bill of health over content codefit never read; note: %q",
			res.Res.Score, res.Note)
	}
	if res.Res.Score != 0 {
		t.Errorf("a not-measured result must publish no score, got %d", res.Res.Score)
	}
}

// C2. The note NAMES the configured path, exactly as written in .codefit.yaml.
// The mutation that breaks it is a pathless note — which is not hypothetical:
// the sibling not-measured branch at db.go:118 returns exactly that, and
// copying it is the natural mistake.
func TestSensorDB_ZeroResolutionDirectory_NoteNamesTheConfiguredPath(t *testing.T) {
	ctx := zeroResolutionProject(t)

	res, err := New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if res.Note == "" {
		t.Fatal("a not-measured result must carry a note")
	}
	if !strings.Contains(res.Note, singleDirPath) {
		t.Errorf("the note does not name the configured path %q, so a reader cannot act on it: %q",
			singleDirPath, res.Note)
	}
}

// The note's CONTRACT (spec R3), asserted as behaviour rather than as a frozen
// sentence: the fact, the consequence, and — the part that makes it actionable —
// what to do. And the negative half, which matters just as much: the note must
// not DIAGNOSE. "It wanted a .sql" and "it orders by V<n>__" are statements about
// today's resolver that go stale the moment the resolver grows, and a stale
// instruction is worse than none. The action does not go stale.
func TestSensorDB_ZeroResolutionDirectory_NoteStatesFactConsequenceAndAction(t *testing.T) {
	ctx := zeroResolutionProject(t)

	res, err := New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	for _, want := range []string{
		"no readable schema file",           // the FACT
		"not a clean bill of health",        // the CONSEQUENCE
		"database.schema_paths",             // the ACTION names the key to edit
		"point the entry at the schema fil", // the ACTION itself
	} {
		if !strings.Contains(res.Note, want) {
			t.Errorf("the note is missing %q — it states a fact a reader cannot act on: %q", want, res.Note)
		}
	}
	for _, forbidden := range []string{".sql", "Flyway", "flyway", "extension", "V<n>"} {
		if strings.Contains(res.Note, forbidden) {
			t.Errorf("the note diagnoses with %q. Which shape the resolver accepts is a property of today's "+
				"resolver; the note must state the outcome and the action, both of which stay true: %q",
				forbidden, res.Note)
		}
	}
}

// C3a. A partial resolution stays MEASURED and keeps its real findings. The
// mutation that breaks it is the over-correction — letting any unproductive
// path force Measured=false — which throws away everything codefit did audit.
func TestSensorDB_PartialResolution_StaysMeasuredAndKeepsFindings(t *testing.T) {
	ctx := partialResolutionProject(t)

	res, err := New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !res.Measured {
		t.Fatalf("one configured path resolved to a real table, so the scan IS a measurement; reporting it "+
			"unmeasured discards findings codefit actually made. note: %q", res.Note)
	}
	if res.Schema == nil || len(res.Schema.Tables) != 1 {
		t.Fatalf("the contributing path must still be parsed into exactly 1 table, got %+v", res.Schema)
	}
}

// C3b. A partial resolution still NAMES the path that resolved to nothing. The
// mutation that breaks it is dropping zero-resolution entries before the note
// is composed — which is main's behaviour exactly, and the tempting "the note
// is about files" purism.
//
// This is the control that matters most: Case 1 at least looks suspicious,
// while this one looks completely normal.
func TestSensorDB_PartialResolution_NamesTheEmptyPath(t *testing.T) {
	ctx := partialResolutionProject(t)

	res, err := New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !res.Measured {
		t.Fatalf("precondition: the scan must be measured for this control to be about the note; note: %q", res.Note)
	}
	if !strings.Contains(res.Note, emptyDirPath) {
		t.Errorf("the scan is measured and the note never mentions %q, so a path codefit read nothing from "+
			"leaves no trace at all — a partial result that does not declare itself partial. note: %q",
			emptyDirPath, res.Note)
	}
	// The denominator is the PATH count, and stating it against the file count
	// would be a different lie in the same sentence: 1 of 2 configured paths, not
	// 1 of the 1 file that resolved.
	if !strings.Contains(res.Note, "1 of the 2 configured schema path(s)") {
		t.Errorf("the note must count the empty path against the CONFIGURED PATH total (2), not against the "+
			"resolved-file total: %q", res.Note)
	}
}

// C5, the CONSTRUCTION lock. It asserts the invariant the whole reshape rests
// on — every configured path yields an entry — rather than one of its
// consequences, so it fails on the mistake itself (a resolver that skips a path
// producing no file) instead of waiting for a note three layers away to notice.
func TestReadSchemaSources_EveryConfiguredPathYieldsAnEntry(t *testing.T) {
	ctx := partialResolutionProject(t)

	resolution, err := readSchemaSources(ctx.ProjectRoot, ctx.Config.Database.SchemaPaths)
	if err != nil {
		t.Fatalf("readSchemaSources: %v", err)
	}
	if len(resolution.Paths) != len(ctx.Config.Database.SchemaPaths) {
		t.Fatalf("resolution carries %d entries for %d configured paths — a path that resolved to nothing "+
			"was dropped, which is exactly how it disappears from every downstream count: %+v",
			len(resolution.Paths), len(ctx.Config.Database.SchemaPaths), resolution.Paths)
	}
	for i, p := range resolution.Paths {
		if p.Configured != ctx.Config.Database.SchemaPaths[i] {
			t.Errorf("entry %d carries Configured=%q, want the config entry %q verbatim — the note has to name "+
				"the string the developer will go and edit", i, p.Configured, ctx.Config.Database.SchemaPaths[i])
		}
	}
	empty := resolution.Paths[1]
	if len(empty.Files) != 0 {
		t.Errorf("%q resolved to %d file(s); the fixture's content guard says it holds none", empty.Configured, len(empty.Files))
	}
	// The flattening accessor must be exactly the old flat list: nothing extra
	// for the empty entry, and the contributing file still present.
	if got := resolution.sources(); len(got) != 1 || got[0].Path != realFilePath {
		t.Errorf("sources() = %+v, want exactly the one resolved file %q — the empty entry must contribute no "+
			"phantom SourceFile to the parser", got, realFilePath)
	}
}

// The two not-measured facts stay DISTINCT (spec R4). "No schema_paths
// configured" and "schema_paths configured, resolved, yielded nothing" are
// different problems with different fixes, and a reader who is told the wrong
// one goes and edits the wrong thing. Two separate fixtures, because the only
// way to prove two notes differ is to produce both.
func TestSensorDB_TheTwoNotMeasuredFactsDoNotCollapse(t *testing.T) {
	noPaths := writeDBProject(t, map[string]string{"db/migrations/embed.go": embedGoSource})
	if len(noPaths.Config.Database.SchemaPaths) != 0 {
		t.Fatalf("precondition: the no-paths fixture must configure none, got %v", noPaths.Config.Database.SchemaPaths)
	}
	resNone, err := New(sqlddl.New()).Audit(noPaths)
	if err != nil {
		t.Fatalf("Audit (no schema_paths): %v", err)
	}
	resZero, err := New(sqlddl.New()).Audit(zeroResolutionProject(t))
	if err != nil {
		t.Fatalf("Audit (zero resolution): %v", err)
	}

	if resNone.Measured || resZero.Measured {
		t.Fatalf("both states must be not-measured, got %v / %v", resNone.Measured, resZero.Measured)
	}
	if resNone.Note == resZero.Note {
		t.Fatalf("both states render the SAME note, so the reader cannot tell which problem they have: %q", resNone.Note)
	}
	if !strings.Contains(resNone.Note, "no database.schema_paths configured") {
		t.Errorf("the no-configuration note must name its own cause, got %q", resNone.Note)
	}
	if strings.Contains(resNone.Note, singleDirPath) {
		t.Errorf("the no-configuration note names a path nothing configured, got %q", resNone.Note)
	}
	if !strings.Contains(resZero.Note, singleDirPath) {
		t.Errorf("the zero-resolution note must name the configured path that resolved to nothing, got %q", resZero.Note)
	}
}
