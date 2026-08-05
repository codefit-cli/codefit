package mcp

import (
	"bytes"
	"encoding/json"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// TestCrossSeam_CollectsFiltersButProducesNothing is the seam gate (ADR 0029): the
// wiring must COLLECT the code's query filters (proving the walk + extraction are
// live) yet produce ZERO findings/surface while crossrules.All() is empty (proving
// the seam is a no-op — the db result stays byte-identical to before the cross was
// wired, ADR 0020). If this ever produces output, a rule leaked in before its
// slice, or the wiring stopped being a no-op — FRENÁ.
func TestCrossSeam_CollectsFiltersButProducesNothing(t *testing.T) {
	root := t.TempDir()
	code := `export async function GET() {
		return await prisma.user.findMany({ where: { email: "x" } })
	}`
	if err := os.WriteFile(filepath.Join(root, "handler.ts"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := providerForLanguage("typescript", nil)
	ex, ok := provider.(providers.QueryExtractor)
	if !ok {
		t.Fatal("typescript provider must implement QueryExtractor")
	}

	// The walk + extraction are live: the code's where clause is collected.
	filters := collectQueryFilters(root, provider.FileExtensions(), ex, scope.Full())
	if len(filters) == 0 {
		t.Fatal("collectQueryFilters found no filter — the walk/extraction seam is not live")
	}
	if filters[0].Model != "User" {
		t.Errorf("collected filter model = %q, want User", filters[0].Model)
	}

	// The seam is a no-op with an empty rule set: a schema that WOULD reconcile still
	// yields nothing.
	schema := &db.Schema{Tables: []db.Table{{Name: "User", Columns: []db.Column{{Name: "email"}}}}}
	f, s, skip := runCross(root, "typescript", schema, nil, nil, scope.Full())
	if f != nil || s != nil {
		t.Errorf("runCross with an empty rule set must yield nothing, got findings=%v surface=%v", f, s)
	}
	if skip != "" {
		t.Errorf("runCross must actually RUN here (typescript has a QueryExtractor and a parsed schema), got skip reason %q", skip)
	}
}

// TestRunCross_NilSchema — no schema, no cross (a project with no database).
func TestRunCross_NilSchema(t *testing.T) {
	f, s, skip := runCross(t.TempDir(), "typescript", nil, nil, nil, scope.Full())
	if f != nil || s != nil {
		t.Error("runCross(nil schema, scope.Full()) must yield nothing")
	}
	if skip == "" {
		t.Error("runCross(nil schema) must report a non-empty skip reason (D6)")
	}
}

// crossSeamFixture writes a project that produces a POPULATED db result (dbrules
// findings AND surface) and has TS Prisma queries the cross walks and collects —
// so the seam gate compares a non-empty, serialized DBSection, not an empty one.
func crossSeamFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prisma"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model Account {
  id       Int    @id
  email    String
  password String
  @@index([email])
  @@index([email])
}

model NoKey {
  name String
}
`
	code := `export async function GET() {
  return await prisma.account.findMany({ where: { email: "x" } })
}
`
	yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  paradigm: oltp
  schema_paths:
    - prisma/schema.prisma
`
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("prisma/schema.prisma", schema)
	write("src/handler.ts", code)
	write(".codefit.yaml", yaml)
	return root
}

// TestCrossSeam_DBResultByteIdenticalOverPopulated is the REAL seam gate (ADR 0029,
// ADR 0020). Not the runner tautology: it runs runDBForScanAll — the wired path
// that walks the code, collects the query filters, runs crossrules.Run(schema,
// filters) and MERGES the output into the db result — over a POPULATED fixture, and
// asserts the serialized SensorResult is byte-identical to what the code produced
// BEFORE the cross was wired (dbsensor.Audit's Res directly). A phantom finding, a
// reordering, or a nil→[] slip in the merge makes these bytes differ and fails the
// test. DurationMs is timing noise, zeroed on both.
func TestCrossSeam_DBResultByteIdenticalOverPopulated(t *testing.T) {
	root := crossSeamFixture(t)
	cfg, err := config.LoadOptional(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	// Reference = db result BEFORE the cross wiring: exactly what runDBForScanAll
	// returned before the merge existed — dbsensor.Audit's Res.
	parser, note := schemaParserForPaths(root, cfg.Database.SchemaPaths, cfg.Database.Type)
	if parser == nil {
		t.Fatalf("no schema parser resolved: %s", note)
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "typescript", Config: cfg}
	ref, err := dbsensor.New(parser).Audit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ref.Measured {
		t.Fatalf("fixture not measured: %s", ref.Note)
	}

	// The section must be POPULATED — over an empty section "unchanged" is trivial.
	if len(ref.Res.Findings) == 0 || len(ref.Res.Surface) == 0 {
		t.Fatalf("fixture must produce a populated db result (findings AND surface); got %d findings, %d surface",
			len(ref.Res.Findings), len(ref.Res.Surface))
	}
	t.Logf("populated db result: %d findings, %d surface", len(ref.Res.Findings), len(ref.Res.Surface))

	// The cross must actually RUN over real filters, not skip vacuously.
	provider := providerForLanguage("typescript", nil)
	ex, _ := provider.(providers.QueryExtractor)
	if got := collectQueryFilters(root, provider.FileExtensions(), ex, scope.Full()); len(got) == 0 {
		t.Fatal("fixture must exercise the cross: no query filters collected from the code")
	}

	// Actual = the WIRED path, with an EMPTY rule set INJECTED. The seam gate proves
	// the MERGE mechanism (appending the runner's output leaves the db result
	// byte-identical) independently of what crossrules.All() holds — so it survives
	// DB-010 and every future rule landing in All().
	_, act, ran := runDBForScanAll(root, "typescript", cfg, nil, scope.Full())
	if !ran {
		t.Fatal("runDBForScanAll did not run")
	}

	// Byte-identical over the serialized, populated result.
	refRes, actRes := ref.Res, act
	refRes.DurationMs, actRes.DurationMs = 0, 0
	refJSON, err := json.Marshal(refRes)
	if err != nil {
		t.Fatal(err)
	}
	actJSON, err := json.Marshal(actRes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(refJSON, actJSON) {
		t.Errorf("cross seam changed the db result (must be byte-identical with empty All()):\n pre-cross: %s\n wired:     %s", refJSON, actJSON)
	}
}
