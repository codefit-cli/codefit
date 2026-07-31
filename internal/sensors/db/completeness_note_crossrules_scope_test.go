package db_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/typescript"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// F5 (4R ledger, obs #1282): DB-010/DB-013 (internal/core/crossrules) are
// absence-based, run in scan-all, and never consult StructureProven() —
// confirmed by a positive probe (zero hits for StructureProven in that
// package). The inventory note's "Absence-based DB/DW rules abstained on
// them" is true for dbrules/dwrules but reads as comprehensive; a reader
// could reasonably assume it covers every absence-based rule in the DB
// dimension, including the cross. It does not. This test asserts the note
// makes the exception explicit whenever it has anything to say at all — the
// ADR 0018 corollary (a declared limit must be machine-visible) applied to
// this specific gap.
func TestSensorDB_CompletenessNote_DeclaresCrossrulesExceptionWhenNonEmpty(t *testing.T) {
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
	if r.Note == "" {
		t.Fatal("test fixture setup produced an empty note — cannot observe the exception clause")
	}
	if !strings.Contains(r.Note, "DB-010") || !strings.Contains(r.Note, "DB-013") {
		t.Errorf("Note = %q, want it to name DB-010/DB-013 as a declared exception", r.Note)
	}
	if !strings.Contains(r.Note, "does not consult") {
		t.Errorf("Note = %q, want it to state the cross does not consult this completeness signal", r.Note)
	}
}

// The exception clause must NOT spam a clean scan — the note stays empty
// when nothing was unproven and nothing was suppressed.
func TestSensorDB_CompletenessNote_NoExceptionClauseWhenNoteEmpty(t *testing.T) {
	ctx := writeProject(t, "prisma/schema.prisma", happySchema, yamlNoParadigm)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if r.Note != "" {
		t.Errorf("Note = %q, want empty — nothing was unproven, the exception clause must not spam a clean scan", r.Note)
	}
}
