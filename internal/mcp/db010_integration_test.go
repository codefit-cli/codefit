package mcp

import (
	"github.com/codefit-cli/codefit/internal/core/scope"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// TestDB010_SurfacesEndToEnd is the production wiring proof (distinct from the seam
// gate, which injects nil): with the REAL rule set (crossrules.All(), holding
// DB-010), a project whose code filters an unindexed column must surface a
// db-filtered-column-no-index item in the db result — WITH a non-empty baseline
// fingerprint, proving the adapter stamps the cross output (which is produced after
// the sensor and would otherwise be dropped by observedFrom on an empty FP).
func TestDB010_SurfacesEndToEnd(t *testing.T) {
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
  id     Int    @id
  status String
}
`
	code := `export async function GET() {
  return await prisma.account.findMany({ where: { status: "x" } })
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

	cfg, err := config.LoadOptional(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, res, ran := runDBForScanAll(root, "typescript", cfg, crossrules.All(), scope.Full())
	if !ran {
		t.Fatal("db did not run")
	}

	var found int
	for _, it := range res.Surface {
		if it.Category != string(surface.CategoryDBFilteredColumnNoIndex) {
			continue
		}
		found++
		if it.Fingerprint == "" {
			t.Error("DB-010 surface item has an empty fingerprint — the adapter did not stamp the cross output")
		}
		if it.ID == "" {
			t.Error("DB-010 surface item has an empty stable id")
		}
		if it.File != "prisma/schema.prisma" {
			t.Errorf("DB-010 anchored at %s, want the schema file", it.File)
		}
	}
	if found != 1 {
		t.Fatalf("want exactly 1 DB-010 surface item in the production db result, got %d", found)
	}
}
