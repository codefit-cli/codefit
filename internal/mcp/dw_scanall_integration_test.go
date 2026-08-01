package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// TestDW_SurfacesEndToEnd is the DW family's scan-all wiring proof — same shape as
// the sibling DB-010 (db010_integration_test.go) and DB-013
// (db013_integration_test.go) tests, driving the PRODUCTION runDBForScanAll (the
// exact path codefit-scan-all uses). Unlike DB-010/DB-013, the DW rules do not run
// through crossrules: they run INSIDE dbsensor.Audit (internal/sensors/db/db.go),
// so this test only needs the schema, not a code file to extract query filters.
//
// It closes S2 verify report WARNING W2 (Engram #1238): the spec scenario
// "scan-all surfaces a DW finding" had no committed test — only a discarded
// throwaway probe against this same function during verify. This test is a
// regression LOCK over already-correct, already-shipped behavior (S2 landed in
// commits d6972ba..2dd45e9); it was written after its production code, not
// before, and it verifies rather than fixes anything.
func TestDW_SurfacesEndToEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prisma"), 0o755); err != nil {
		t.Fatal(err)
	}
	// fact_sales fans out to two dimensions (so DW-001 must NOT fire); dim_product
	// is keyed by a natural string code (DW-002) and there is no time dimension
	// anywhere (DW-005). Same fixture shape as the sensor-level
	// TestSensorDB_ParadigmAssembly_DWRulesRunOnAStarSchema in
	// internal/sensors/db/db_test.go.
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id           Int          @id
  customer_sk  Int
  product_sk   Int
  customerKey  Int
  productCode  String
  amount       Float
  dim_customer dim_customer @relation(fields: [customerKey], references: [customer_key])
  dim_product  dim_product  @relation(fields: [productCode], references: [product_code])
}

model dim_customer {
  customer_key Int          @id
  customer_sk  Int
  name         String
  fact_sales   fact_sales[]
}

model dim_product {
  product_code String       @id
  category     String
  fact_sales   fact_sales[]
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
	_, res, ran := runDBForScanAll(root, "typescript", cfg, crossrules.All())
	if !ran {
		t.Fatal("db did not run")
	}

	scope := map[string]bool{}
	for _, c := range dbsensor.New(nil).OwnedCategories() {
		scope[c] = true
	}

	for _, want := range []string{
		string(surface.CategoryDWDimensionNoSurrogateKey), // dim_product's natural key
		string(surface.CategoryDWNoTimeDimension),         // no dim_date anywhere
	} {
		found := false
		for _, it := range res.Surface {
			if it.Category != want {
				continue
			}
			found = true
			if it.Fingerprint == "" {
				t.Errorf("%s surface item has an empty fingerprint — the sensor did not stamp it, it could never be baselined", want)
			}
			if it.ID == "" {
				t.Errorf("%s surface item has an empty stable id", want)
			}
			if it.File != "prisma/schema.prisma" {
				t.Errorf("%s anchored at %s, want the schema file", want, it.File)
			}
			if !scope[it.Category] {
				t.Errorf("%s is not in dbsensor.OwnedCategories() — it would fall outside the baseline", want)
			}
		}
		if !found {
			t.Errorf("no %s item in the production scan-all db bucket — the DW family did not reach it", want)
		}
	}
}
