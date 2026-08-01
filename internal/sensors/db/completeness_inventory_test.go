package db_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/typescript"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// D7 (design SS7a) — the per-scan completeness INVENTORY. Result.Note now
// carries two independent, composed traces: the measurement inventory
// (which tables codefit could not prove complete, and why) FIRST, then the
// pre-existing 3NF-suppression trace. Aggregated by REASON, never by table
// (a systematic parser gap must be one line, not N).

const yamlNoParadigm = `version: "1"
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

func TestSensorDB_CompletenessInventory_NamesUnprovenTables(t *testing.T) {
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
	ctx := writeProject(t, "prisma/schema.prisma", schema, yamlNoParadigm)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !strings.Contains(r.Note, "Widget") {
		t.Errorf("Note = %q, want it to name the unproven table Widget", r.Note)
	}
	if !strings.Contains(r.Note, "1") {
		t.Errorf("Note = %q, want it to state the count (1)", r.Note)
	}
	// The boundary doctrine (design SS7a): no parser-internal diagnostics
	// leak into the note — no Go identifiers, no package/function names.
	// Deliberately does NOT forbid the bare substring "reduce": the
	// legitimate reason prose itself says "could not be REDUCEd" (English),
	// which contains that substring — the exact coarse-lock collision design
	// SS7a's N7 predicts. The identifiers below are unambiguous.
	forbidden := []string{".go:", "func ", "applyAlter", "splitTopLevelParts", "dialect."}
	for _, tok := range forbidden {
		if strings.Contains(r.Note, tok) {
			t.Errorf("Note = %q, contains forbidden parser-internal token %q", r.Note, tok)
		}
	}
}

func TestSensorDB_CompletenessInventory_EmptyWhenEverythingProven(t *testing.T) {
	ctx := writeProject(t, "prisma/schema.prisma", happySchema, yamlNoParadigm)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if r.Note != "" {
		t.Errorf("Note = %q, want empty — nothing was unproven, the inventory must never spam", r.Note)
	}
}

// The inventory and the 3NF-suppression trace COMPOSE — neither clobbers the
// other — and the measurement inventory is FIRST when both are present.
//
// The _sk columns are the schema gate's price of admission (ADR 0037): without
// schema-wide warehouse evidence no table holds a warehouse role, nothing is
// suppressed, and there is no second trace to compose with. That the SUPPRESSION
// trace is what this test looks for — not merely "a non-empty note" — is what
// keeps it honest now that a third producer shares the same channel.
func TestSensorDB_CompletenessInventory_ComposesWithSuppressionNote(t *testing.T) {
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id          Int          @id
  customer_sk Int
  product_sk  Int
  customer_id Int
  product_id  Int
  customer    dim_customer @relation(fields: [customer_id], references: [id])
  product     dim_product  @relation(fields: [product_id], references: [id])
  @@index([customer_id],
    map: "idx_fact_sales_customer")
}

model dim_customer {
  id          Int @id
  customer_sk Int
}

model dim_product {
  id        Int    @id
  category1 String
  category2 String
}
`
	ctx := writeProject(t, "prisma/schema.prisma", schema, yamlNoParadigm)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !strings.Contains(r.Note, "fact_sales") {
		t.Errorf("Note = %q, want the measurement inventory naming fact_sales", r.Note)
	}
	if !strings.Contains(r.Note, "3NF-suppression") {
		t.Errorf("Note = %q, want the pre-existing 3NF-suppression trace still present", r.Note)
	}
	factIdx := strings.Index(r.Note, "fact_sales")
	suppressIdx := strings.Index(r.Note, "3NF-suppression")
	if factIdx < 0 || suppressIdx < 0 || factIdx > suppressIdx {
		t.Errorf("Note = %q, want the measurement inventory FIRST, then the suppression trace", r.Note)
	}
}
