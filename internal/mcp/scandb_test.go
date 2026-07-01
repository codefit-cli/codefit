package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

const dbSchema = `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model NoKey {
  name String
}
`

const dbYAML = `version: "1"
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

func TestHandleScanDB_Measures(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prisma"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prisma", "schema.prisma"), []byte(dbSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(dbYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanDB: %v", err)
	}
	if !resp.Measured {
		t.Fatalf("Measured=false, want true; note=%q", resp.Note)
	}
	var db050 int
	for _, f := range resp.Findings {
		if f.ID == "DB-050" {
			db050++
		}
	}
	if db050 != 1 {
		t.Errorf("DB-050 = %d, want 1 (NoKey)", db050)
	}
}

func TestHandleScanDB_NoConfig_NotMeasured(t *testing.T) {
	// A project with no .codefit.yaml → no schema_paths → not measured, no error.
	resp, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: t.TempDir(), Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanDB: %v", err)
	}
	if resp.Measured || resp.Note == "" {
		t.Errorf("no config must be not-measured with a note, got Measured=%v note=%q", resp.Measured, resp.Note)
	}
}
