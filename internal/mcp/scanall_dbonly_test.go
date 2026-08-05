package mcp_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// goYAMLWithSchema configures a Go project (no resolvable security provider)
// with a SQL-DDL schema — the DB dimension's parser does not depend on
// req.Language, so this is the smallest fixture that proves the DB dimension
// runs independently of the language resolution.
const goYAMLWithSchema = `version: "1"
project:
  name: t
  language: go
database:
  type: postgresql
  schema_paths:
    - db/schema.sql
`

// goYAMLNoSchema is the same Go project with no database section at all — the
// nothing-measurable case (Phase 3): no security provider AND no DB dimension.
const goYAMLNoSchema = `version: "1"
project:
  name: t
  language: go
`

const goNoPKSchema = `CREATE TABLE no_key (
  name TEXT NOT NULL
);
`

// writeGoSchemaProj is the Go+schema fixture shared by this file's tests: a
// minimal Go module with a configured SQL-DDL schema.
func writeGoSchemaProj(t *testing.T) string {
	t.Helper()
	return writeProj(t, map[string]string{
		".codefit.yaml": goYAMLWithSchema,
		"db/schema.sql": goNoPKSchema,
		"main.go":       "package main\n\nfunc main() {}\n",
		"go.mod":        "module example.com/t\n\ngo 1.25\n",
	})
}

// TestHandleScanAll_GoProjectWithSchema_AuditsDB is the P0-5 defect made a RED
// test (spec: "scan-all measures DB without a resolved provider"): a Go
// project has no resolvable security provider, but its configured schema must
// still be measured by the DB dimension, and the handler must not error.
func TestHandleScanAll_GoProjectWithSchema_AuditsDB(t *testing.T) {
	root := writeGoSchemaProj(t)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "go"})
	if err != nil {
		t.Fatalf("HandleScanAll over a Go project with a configured schema must not error, got: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured over a Go project's schema, got %+v", resp.DB)
	}
	if resp.Score.ByDimension[findings.DimensionSecurity] != nil {
		t.Errorf("security must be NOT MEASURED (nil) for a Go project, got %+v",
			resp.Score.ByDimension[findings.DimensionSecurity])
	}
	if resp.Score.ByDimension[findings.DimensionDB] == nil {
		t.Error("db must be measured (non-nil) for a Go project with a configured schema")
	}
}
