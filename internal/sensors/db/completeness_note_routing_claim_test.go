package db_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// F2 (4R ledger, obs #1282, CRITICAL, verified): the inventory note used to
// tell the agent, unconditionally, that DB-050 "routed them to the
// db-table-structure-unproven surface items — read those items for the raw
// statements and their file:line." But DB-050 (dbrules/rules.go) checks
// `len(t.PrimaryKey) > 0` BEFORE the routing check — an unproven table that
// ALREADY shows a primary key in the (possibly incomplete) model is never
// routed anywhere. The note pointed the agent at nothing. Reproduced here
// with the ledger's own repro case: `model Widget { id Int @id }` plus one
// unrecognized body line.
func TestSensorDB_CompletenessNote_DoesNotClaimRoutingForTablesWithAPK(t *testing.T) {
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

	// The bug's own symptom: confirm Widget genuinely has NO routed item —
	// the note is about to make a claim about it, so first confirm there is
	// nothing to claim.
	for _, it := range r.Res.Surface {
		if it.Category == string(surface.CategoryDBTableStructureUnproven) {
			t.Fatalf("Widget was routed (%+v) — this fixture no longer reproduces F2's premise (a PK-bearing "+
				"unproven table); adjust the fixture, do not adjust this assertion", it)
		}
	}

	if strings.Contains(r.Note, "DB-050 routed them to the db-table-structure-unproven") {
		t.Errorf("Note = %q, want it to NOT claim DB-050 routed Widget — Widget has a PK, so DB-050's own "+
			"len(PrimaryKey)>0 guard means it was never routed", r.Note)
	}
	if !strings.Contains(r.Note, "did not route") {
		t.Errorf("Note = %q, want it to explicitly say DB-050 did not route this table (no promise of a "+
			"surface item that does not exist)", r.Note)
	}
}

// Mixed reason group: one unproven table WITH a PK (not routed), one
// unproven table for the SAME reason WITHOUT a PK (routed) — the note must
// distinguish the two, not apply one blanket claim to both.
func TestSensorDB_CompletenessNote_MixedRoutedAndUnrouted_DistinguishesBoth(t *testing.T) {
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model HasPK {
  id Int @id
  @@index([id],
    map: "idx_a")
}

model NoPK {
  name String
  @@index([name],
    map: "idx_b")
}
`
	ctx := writeProject(t, "prisma/schema.prisma", schema, yamlNoParadigm)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}

	var routedNames []string
	for _, it := range r.Res.Surface {
		if it.Category == string(surface.CategoryDBTableStructureUnproven) {
			for _, sig := range it.StructuralSignals {
				if strings.HasPrefix(sig, "table: ") {
					routedNames = append(routedNames, strings.TrimPrefix(sig, "table: "))
				}
			}
		}
	}
	if len(routedNames) != 1 || routedNames[0] != "NoPK" {
		t.Fatalf("routed tables = %v, want exactly [NoPK]", routedNames)
	}

	if !strings.Contains(r.Note, "1 of them") {
		t.Errorf("Note = %q, want it to state exactly 1 of the 2 unproven tables was routed", r.Note)
	}
	if !strings.Contains(r.Note, "already show a primary key") {
		t.Errorf("Note = %q, want it to acknowledge HasPK was NOT routed (already shows a PK)", r.Note)
	}
}
