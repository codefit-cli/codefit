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

// TestSakila_GoldenSchema locks the neutral db.Schema produced by parsing the
// MySQL Sakila excerpt under the MySQL() dialect (Unit D, DoD closure item
// 1/3) — PK, FK, an index, an ENUM/SET column, backtick identifiers, and an
// UNSIGNED/AUTO_INCREMENT column.
func TestSakila_GoldenSchema(t *testing.T) {
	assertGolden(t, filepath.Join("mysql", "sakila_excerpt.sql"), filepath.Join("mysql", "sakila_excerpt.schema.golden.json"), sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
}

// TestAdventureWorks_GoldenSchema locks the neutral db.Schema produced by
// parsing the T-SQL AdventureWorks excerpt under the SQLServer() dialect
// (Unit G, DoD closure item 2/3) — bracket identifiers, schema-qualified
// names, IDENTITY, a single-column and a composite PK, a schema-qualified
// FOREIGN KEY, an index, and a spread of T-SQL types.
func TestAdventureWorks_GoldenSchema(t *testing.T) {
	assertGolden(t, filepath.Join("tsql", "adventureworks_excerpt.sql"), filepath.Join("tsql", "adventureworks_excerpt.schema.golden.json"), sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
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
