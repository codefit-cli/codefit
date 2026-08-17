package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/mcp"
	"github.com/codefit-cli/codefit/internal/schemasource"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// pagilaExcerptSQL reads the real Pagila excerpt fixture (design D7, task
// 2.1/2.3): driving the REAL db sensor over REAL DDL, never a hand-assembled
// struct.
func pagilaExcerptSQL(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "providers", "sqlddl", "testdata", "pagila_excerpt.sql"))
	if err != nil {
		t.Fatalf("reading pagila_excerpt.sql: %v", err)
	}
	return string(raw)
}

// realDBSensorSurfaceCount independently measures the db sensor's TRUE
// population over schemaSQL — bypassing surfaceindex, scan-all and scan-db
// entirely. This is the ground truth the conservation assertions below (and
// the M3 mutation test) anchor to, never the response's own index: counting
// a response's own (possibly truncated) index against itself is exactly the
// self-referential trap the coverage-chain archive (obs #1664) records.
func realDBSensorSurfaceCount(t *testing.T, root string) int {
	t.Helper()
	cfg, err := config.LoadOptional(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	parser, note := schemasource.ParserForPaths(root, cfg.Database.SchemaPaths, cfg.Database.Type)
	if parser == nil {
		t.Fatalf("no schema parser resolved: %s", note)
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "typescript", Config: cfg}
	r, err := dbsensor.New(parser).Audit(ctx)
	if err != nil {
		t.Fatalf("db sensor audit: %v", err)
	}
	if !r.Measured {
		t.Fatalf("db sensor did not measure: %s", r.Note)
	}
	return len(r.Res.Surface)
}

// assertNoHeavyFields checks the marshaled index bytes carry none of the
// heavy per-item fields the spec's "light fields only" scenario forbids.
func assertNoHeavyFields(t *testing.T, raw []byte) {
	t.Helper()
	s := string(raw)
	for _, forbidden := range []string{`"snippet"`, `"structural_signals"`, `"reason_to_review"`, `"indirect_call"`} {
		if strings.Contains(s, forbidden) {
			t.Errorf("index leaks a heavy field %s: %s", forbidden, s)
		}
	}
}

// TestScanAll_DBSurfaceIsIndexed_RealSensor is task 2.1/2.2: driving the REAL
// HandleScanAll over the real Pagila excerpt, db.surface must be the light
// index shape — none of the heavy fields — and non-empty (so the assertions
// above prove something).
func TestScanAll_DBSurfaceIsIndexed_RealSensor(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLWithSQLDDL,
		"db/schema.sql": pagilaExcerptSQL(t),
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured for the Pagila fixture, got %+v", resp.DB)
	}
	if len(resp.DB.Surface) == 0 {
		t.Fatal("the Pagila fixture produced no db surface items — this test would prove nothing")
	}
	raw, err := json.Marshal(resp.DB.Surface)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertNoHeavyFields(t, raw)
	for _, want := range []string{`"id"`, `"category"`, `"file"`, `"line"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("index is missing the light field %s: %s", want, raw)
		}
	}
}

// TestScanDB_SurfaceIsIndexed_ParityWithScanAll is task 2.3/2.4: the
// standalone codefit-scan-db carries the SAME light index shape as
// scan-all's db.surface, over the same fixture (no baseline filtering ever
// applies to the standalone tool, so its item set is the sensor's raw
// population).
func TestScanDB_SurfaceIsIndexed_ParityWithScanAll(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLWithSQLDDL,
		"db/schema.sql": pagilaExcerptSQL(t),
	})
	resp, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanDB: %v", err)
	}
	if !resp.Measured {
		t.Fatalf("Measured=false, want true; note=%q", resp.Note)
	}
	if len(resp.Surface) == 0 {
		t.Fatal("the Pagila fixture produced no db surface items — this test would prove nothing")
	}
	raw, err := json.Marshal(resp.Surface)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertNoHeavyFields(t, raw)

	want := realDBSensorSurfaceCount(t, root)
	if len(resp.Surface) != want {
		t.Errorf("scan-db surface has %d entries, want %d (the sensor's own population)", len(resp.Surface), want)
	}
}

// TestScanAll_DBSection_CountAndWithheldHonesty is task 3.1/3.2: Count equals
// the db sensor's own population (anchored independently, never read off the
// response's own possibly-truncated index — the coverage-chain trap, obs
// #1664), Withheld is always 0, and WithheldNote is present with wording of
// its own (not the endpoint-bucket pattern, not coverage's sentence — design
// D4: db withholds nothing because there is no ranking axis, not because a
// fixed manifest authorizes nothing).
func TestScanAll_DBSection_CountAndWithheldHonesty(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLWithSQLDDL,
		"db/schema.sql": pagilaExcerptSQL(t),
	})
	want := realDBSensorSurfaceCount(t, root)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured, got %+v", resp.DB)
	}
	if resp.DB.Count != want {
		t.Errorf("DB.Count = %d, want %d (the db sensor's own population, a first scan filters nothing)", resp.DB.Count, want)
	}
	if len(resp.DB.Surface) != want {
		t.Errorf("len(DB.Surface) = %d, want %d", len(resp.DB.Surface), want)
	}
	if resp.DB.Withheld != 0 {
		t.Errorf("DB.Withheld = %d, want 0 — db.surface has no ranking axis to withhold by (design D4)", resp.DB.Withheld)
	}
	if resp.DB.WithheldNote == "" {
		t.Error("DB.WithheldNote is empty — silence and \"nothing was withheld\" must not be the same bytes")
	}
	if strings.Contains(resp.DB.WithheldNote, "response budget authorizes withholding for scan-all; for coverage") {
		t.Error("DB.WithheldNote must not reuse coverage's sentence verbatim — the reason differs (D4)")
	}
}

// TestScanDB_CountAndWithheldHonesty mirrors the above for the standalone tool.
func TestScanDB_CountAndWithheldHonesty(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLWithSQLDDL,
		"db/schema.sql": pagilaExcerptSQL(t),
	})
	want := realDBSensorSurfaceCount(t, root)

	resp, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanDB: %v", err)
	}
	if resp.Count != want {
		t.Errorf("Count = %d, want %d", resp.Count, want)
	}
	if resp.Withheld != 0 {
		t.Errorf("Withheld = %d, want 0", resp.Withheld)
	}
	if resp.WithheldNote == "" {
		t.Error("WithheldNote is empty")
	}
}
