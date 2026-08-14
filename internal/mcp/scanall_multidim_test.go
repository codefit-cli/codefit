package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// writeProj writes a set of relative files into a temp root and returns the root.
func writeProj(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const tsYAMLNoDB = `version: "1"
project:
  name: t
  language: typescript
  framework: next
`

const tsYAMLWithDB = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`

const noPKSchema = `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model NoKey {
  name String
}
`

// GOLDEN no-regression: a project with no database has NO db section, and the
// marshaled response contains no "db" key — byte-identical shape to today.
func TestScanAll_NoDB_Regression(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":  tsYAMLNoDB,
		"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	// The DB *section* (findings/surface) is still omitted with no database.
	if resp.DB != nil {
		t.Errorf("a project with no schema_paths must have DB==nil, got %+v", resp.DB)
	}
	// The score object is now always present; the "db" string legitimately appears
	// inside by_dimension (declared null), so the section-omission is asserted via
	// resp.DB above, not by scanning the JSON.
	data, _ := json.Marshal(resp)
	if !strings.Contains(string(data), "\"score\"") {
		t.Errorf("the response must always carry a score object: %s", data)
	}
	if resp.Score.ByDimension == nil {
		t.Error("score.by_dimension must always be present")
	}
}

// A project with a schema gets a populated, parallel DB section; the endpoint
// buckets are untouched.
func TestScanAll_WithDB_SectionPopulated(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":        tsYAMLWithDB,
		"prisma/schema.prisma": noPKSchema,
		"app/x/route.ts":       "export async function GET() { return Response.json({}); }\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured, got %+v", resp.DB)
	}
	var db050 int
	for _, f := range resp.DB.Findings {
		if f.ID == "DB-050" {
			db050++
		}
	}
	if db050 != 1 {
		t.Errorf("DB.Findings must include DB-050 for the PK-less table, got %d", db050)
	}
}

const tsYAMLWithSQLDDL = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - db/schema.sql
`

const noPKSQLSchema = `CREATE TABLE no_key (
  name TEXT NOT NULL
);
`

const tsYAMLWithSQLDDLMySQL = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: mysql
  schema_paths:
    - db/schema.sql
`

// mysqlNoPKSchema is a MySQL-dialect-EXCLUSIVE construct (backtick-quoted
// identifiers + ENUM): under the Postgres dialect the backtick-quoted table
// name does not canonicalize as a CREATE TABLE at all, so the table would be
// silently dropped and DB-050 would never fire. This makes the fixture
// discriminate — it only produces DB-050 if the MySQL dialect was actually
// bound (see TestScanAll_WithDB_MySQLSQLDDL_DialectPlumbingLocksIn).
const mysqlNoPKSchema = "CREATE TABLE `no_key` (\n  `status` ENUM('a','b') NOT NULL\n);\n"

// TestScanAll_WithDB_PostgresSQLDDL_DimensionUnchanged proves the .sql
// (SQL-DDL) scan-all path runs and populates the DB section exactly like the
// pre-Unit-H (unwired) call site did. It does NOT, by itself, prove the
// dbType→dialect plumbing (H6, H7): the fixture is dialect-neutral, and
// sqlddl.New()'s default dialect is Postgres, so this test would still pass
// even if cfg.Database.Type were dropped/hardcoded to "" before reaching
// schemasource.ParserForPaths. TestScanAll_WithDB_MySQLSQLDDL_DialectPlumbingLocksIn
// (below) is the test that actually locks the plumbing, via a dialect
// -exclusive fixture that Postgres cannot parse correctly.
func TestScanAll_WithDB_PostgresSQLDDL_DimensionUnchanged(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":  tsYAMLWithSQLDDL,
		"db/schema.sql":  noPKSQLSchema,
		"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured for a PostgreSQL SQL-DDL project, got %+v", resp.DB)
	}
	var db050 int
	for _, f := range resp.DB.Findings {
		if f.ID == "DB-050" {
			db050++
		}
	}
	if db050 != 1 {
		t.Errorf("DB.Findings must include DB-050 for the PK-less table, got %d", db050)
	}
}

// TestScanAll_WithDB_MySQLSQLDDL_DialectPlumbingLocksIn is the discriminating
// H8 DoD closure: it proves cfg.Database.Type actually reaches
// schemasource.ParserForPaths → sqlDialectParser and binds the MySQL dialect
// descriptor. The fixture (mysqlNoPKSchema) uses backtick-quoted identifiers,
// a MySQL-exclusive construct: under the WRONG dialect (e.g. Postgres, which
// is sqlddl.New()'s default) the backtick-quoted table does not canonicalize
// as a CREATE TABLE, so the table — and therefore DB-050 — would silently
// disappear. If the dbType plumbing regresses (dropped/hardcoded to a
// non-MySQL default), this test fails; TestScanAll_WithDB_PostgresSQLDDL_
// DimensionUnchanged above would not catch that regression.
func TestScanAll_WithDB_MySQLSQLDDL_DialectPlumbingLocksIn(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":  tsYAMLWithSQLDDLMySQL,
		"db/schema.sql":  mysqlNoPKSchema,
		"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured for a MySQL SQL-DDL project, got %+v", resp.DB)
	}
	var db050 int
	for _, f := range resp.DB.Findings {
		if f.ID == "DB-050" {
			db050++
		}
	}
	if db050 != 1 {
		t.Errorf("DB.Findings must include DB-050 for the backtick-quoted PK-less table (proves the MySQL dialect was bound), got %d", db050)
	}
}

// Honest states: a configured-but-missing schema is SOFT in scan-all — reported in
// the DB section, never fatal, never invalidating security.
func TestScanAll_DB_MissingSchema_Soft(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":  tsYAMLWithDB, // points at prisma/schema.prisma
		"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
		// prisma/schema.prisma intentionally absent
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("a DB error must not fail scan-all: %v", err)
	}
	if resp.DB == nil || resp.DB.Measured {
		t.Fatalf("missing schema → DB measured=false with a note, got %+v", resp.DB)
	}
	if resp.DB.Note == "" {
		t.Error("not-measured DB must carry a note")
	}
}

// Unified baseline end-to-end: with both sensors running, a second unchanged scan
// marks nothing gone (neither dimension marks the other gone).
func TestScanAll_UnifiedBaseline_NoCrossGone(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":        tsYAMLWithDB,
		"prisma/schema.prisma": noPKSchema,
		"app/x/route.ts":       "export async function GET(req) { const id = req.nextUrl.searchParams.get('id'); return Response.json(await db.user.findUnique({ where: { id } })); }\n",
	})
	// First scan writes the baseline.
	if _, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	// Second, unchanged scan: nothing gone.
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if resp.Baseline.Gone != 0 {
		t.Errorf("unchanged re-scan with both sensors must mark nothing gone, got Gone=%d (%+v)", resp.Baseline.Gone, resp.Baseline.GoneCandidates)
	}
}

// THE asymmetric case, end-to-end (the ADR 0019 corruption scenario at the
// scan-all level): a DB item is in the committed baseline, but the project has no
// schema_paths so only security runs. The DB item must NOT be marked gone and must
// survive in the saved baseline — confirming HandleScanAll passes the right scope
// when db does not run, not just that baseline.Diff does the right thing in a unit.
func TestScanAll_SecurityOnly_KeepsDBBaselineItem(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":  tsYAMLNoDB, // no database section → db does not run
		"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
	})
	// A DB item already tracked in the committed baseline (from a prior run when the
	// schema was configured), owned by the db sensor which will NOT run this pass.
	bl := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "dbfp", Category: "db-fk-no-index", File: "prisma/schema.prisma"},
	}}
	if err := bl.Save(filepath.Join(root, baseline.Name)); err != nil {
		t.Fatal(err)
	}

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Baseline.Gone != 0 {
		t.Errorf("a security-only run must NOT mark the db item gone, got Gone=%d (%+v)", resp.Baseline.Gone, resp.Baseline.GoneCandidates)
	}
	after, err := baseline.Load(filepath.Join(root, baseline.Name))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range after.Items {
		if it.FP == "dbfp" {
			found = true
		}
	}
	if !found {
		t.Error("the db item must survive in the baseline after a security-only scan-all run")
	}
}
