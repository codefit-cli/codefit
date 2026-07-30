package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// db-model-completeness-contract, design SS4 "scanall.go Note Leak" — the
// MEASURED DB path (scanall.go, previously "&DBSection{Measured: true,
// Score: res.Score}") dropped res.Note, unlike the unmeasured path a few
// lines above it which already carries Note. This is also the delivery
// path for the per-scan incompleteness inventory (design SS7a): without
// this fix, the inventory would compute correctly in the sensor and then
// be silently discarded before reaching the agent.
func TestScanAll_MeasuredDBPath_CarriesNote(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prisma"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A model whose one body line is unrecognized (design SS7b) marks the
	// table unproven, which feeds the completeness inventory into
	// Result.Note — a non-empty note this test can observe end-to-end.
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model Widget {
  id Int @id
  @@index([id],
    map: "idx_foo")
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
  schema_paths:
    - prisma/schema.prisma
`
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("prisma/schema.prisma", schema)
	write(".codefit.yaml", yaml)

	cfg, err := config.LoadOptional(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	section, _, ran := runDBForScanAll(root, "typescript", cfg, crossrules.All())
	if !ran {
		t.Fatal("db did not run")
	}

	// Independently compute what the sensor itself produced, to compare
	// against what scanall's DBSection actually carried.
	want, err := dbsensor.New(typescript.New()).Audit(auditctx.AuditContext{ProjectRoot: root, Language: "typescript", Config: cfg})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if want.Note == "" {
		t.Fatal("test fixture setup produced no sensor Note — fixture must be adjusted, the leak cannot be observed against an empty Note")
	}
	if section.Note == "" {
		t.Error("DBSection.Note is empty on a MEASURED result whose sensor Result.Note is non-empty — the scanall.go:349 leak")
	}
	if section.Note != want.Note {
		t.Errorf("DBSection.Note = %q, want it to equal the sensor's Result.Note %q", section.Note, want.Note)
	}
}
