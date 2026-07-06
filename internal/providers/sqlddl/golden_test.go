package sqlddl_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// update regenerates golden files when set (go test ./... -update). Golden
// files are otherwise read-only regression baselines — see go-testing skill.
var update = flag.Bool("update", false, "update golden files")

// TestPagila_GoldenSchema is the PostgreSQL NO-REGRESSION GATE (RF-03.5): it
// pins the neutral db.Schema produced by parsing the Pagila excerpt, captured
// BEFORE the dialect-descriptor layer existed. Every later work unit re-runs
// this test; it must stay byte-identical for the life of the dialect change.
func TestPagila_GoldenSchema(t *testing.T) {
	assertGolden(t, "pagila_excerpt.sql", "pagila_excerpt.schema.golden.json", sqlddl.New())
}

func assertGolden(t *testing.T, sqlFile, goldenFile string, p *sqlddl.Parser) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", sqlFile))
	if err != nil {
		t.Fatalf("read %s: %v", sqlFile, err)
	}
	s, err := p.ParseSchema([]providers.SourceFile{{Path: sqlFile, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	got, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", goldenFile)
	if *update {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", goldenPath, err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create it): %v", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf("schema for %s does not match golden %s (run with -update to inspect/regenerate):\n--- got ---\n%s\n--- want ---\n%s", sqlFile, goldenFile, got, want)
	}
}
