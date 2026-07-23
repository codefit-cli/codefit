package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// TestDB013_SurfacesEndToEnd is DB-013's production wiring proof (same shape as
// DB-010's, distinct from the seam gate): with the REAL rule set (crossrules.All(),
// holding DB-013), a project whose code filters TWO columns together that no
// composite index covers must surface a db-no-composite-index item — with a
// non-empty fingerprint, proving the adapter stamps the cross output. The single-
// column DB-010 rule must NOT also fire on this multi-column filter (the precedence
// split, ADR 0031).
func TestDB013_SurfacesEndToEnd(t *testing.T) {
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

model Event {
  id       Int    @id
  tenantId String
  status   String
}
`
	code := `export async function GET() {
  return await prisma.event.findMany({ where: { tenantId: "t", status: "s" } })
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
	_, res, ran := runDBForScanAll(root, "typescript", cfg, crossrules.All())
	if !ran {
		t.Fatal("db did not run")
	}

	var composite, single int
	for _, it := range res.Surface {
		switch it.Category {
		case string(surface.CategoryDBNoCompositeIndex):
			composite++
			if it.Fingerprint == "" {
				t.Error("DB-013 surface item has an empty fingerprint — the adapter did not stamp the cross output")
			}
			if it.ID == "" {
				t.Error("DB-013 surface item has an empty stable id")
			}
			if it.File != "prisma/schema.prisma" {
				t.Errorf("DB-013 anchored at %s, want the schema file", it.File)
			}
		case string(surface.CategoryDBFilteredColumnNoIndex):
			single++
		}
	}
	if composite != 1 {
		t.Fatalf("want exactly 1 DB-013 surface item in the production db result, got %d", composite)
	}
	if single != 0 {
		t.Errorf("DB-010 must not fire on a multi-column filter (precedence) — got %d single-column items", single)
	}
}
