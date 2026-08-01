package db_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// THE SCHEMA GATE IS NEVER SILENT (ADR 0037, CLAUDE.md "siempre se informan las
// consecuencias"). It changes what codefit reports in both directions, so both
// directions get a trace:
//
//   - CLOSED and roles withheld: codefit decided this is not a warehouse and
//     took warehouse roles away from tables that would have had them. The
//     consequence is invisible in the surface — items that WOULD have been
//     suppressed simply appear — so the note is the only place a developer can
//     learn the decision was made at all, and it names the escape hatch.
//   - OPEN: codefit concluded "warehouse", and must be able to say WHICH signals
//     made it conclude that. A count would have made this impossible to write,
//     which is one of the reasons the verdict names signals instead of counting
//     them.
//
// The note stays EMPTY when the gate had no consequence — the same never-spam
// rule the completeness inventory and the suppression trace already follow.

// starNoEvidence is the shape the whole slice is about: a textbook fact_/dim_
// star by NAME, with real fan-out and fan-in, inside a schema that shows no
// schema-wide warehouse evidence at all. Before stage 2 every one of these three
// tables promoted itself; now none of them does.
const starNoEvidence = `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id          Int          @id
  customer_id Int
  product_id  Int
  customer    dim_customer @relation(fields: [customer_id], references: [id])
  product     dim_product  @relation(fields: [product_id], references: [id])
}

model dim_customer {
  id Int @id
}

model dim_product {
  id        Int    @id
  category1 String
  category2 String
}
`

// starWithSurrogateKeys is the SAME star carrying the _sk surrogate-key
// convention — 3 _sk columns across 2 tables, which is the measured
// surrogate_key_names signal (3 warehouse fires, 0 transactional, over 26
// corpora). It is the minimal real warehouse evidence that does NOT also
// declare a calendar, so DW-005 keeps firing and this fixture stays comparable
// to the one above.
const starWithSurrogateKeys = `datasource db {
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

const yamlAuto = `version: "1"
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

func auditSchema(t *testing.T, prisma, yaml string) sdb.Result {
	t.Helper()
	ctx := writeProject(t, "prisma/schema.prisma", prisma, yaml)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !r.Measured {
		t.Fatalf("Measured=false: %q", r.Note)
	}
	return r
}

// TestSensorDB_SchemaGateNote_ClosedGate_ReportsWhatWasWithheld is the risk side
// of the slice made visible. Nothing in the surface says "a decision was made
// here"; only the note does.
func TestSensorDB_SchemaGateNote_ClosedGate_ReportsWhatWasWithheld(t *testing.T) {
	r := auditSchema(t, starNoEvidence, yamlAuto)

	if r.Note == "" {
		t.Fatal("Note is empty — the schema gate withheld three warehouse roles and said nothing")
	}
	for _, want := range []string{
		"3",          // how many roles were withheld
		"fact_sales", // and which tables
		"dim_customer",
		"dim_product",
		"calendar_table", // the deciding signals it looked for and did not find
		"surrogate_key_names",
		"type_profile_split",
		"database.paradigm", // the escape hatch, named
		"olap",
	} {
		if !strings.Contains(r.Note, want) {
			t.Errorf("Note = %q\n  missing %q", r.Note, want)
		}
	}

	// And the consequence is real, not just narrated: the 1NF surface these
	// tables would have had suppressed is present.
	if !hasSurfaceForTable(r.Res.Surface, "db-repeating-groups", "dim_product") {
		t.Error("DB-003 did not fire for dim_product — the note claims the 1NF surface was kept, so it must be there")
	}
}

// TestSensorDB_SchemaGateNote_OpenGate_NamesTheDecidingSignals is why the
// verdict names signals instead of counting them: "warehouse-ness 0.7" cannot
// be rendered into a sentence an agent can act on.
func TestSensorDB_SchemaGateNote_OpenGate_NamesTheDecidingSignals(t *testing.T) {
	r := auditSchema(t, starWithSurrogateKeys, yamlAuto)

	if !strings.Contains(r.Note, "surrogate_key_names") {
		t.Errorf("Note = %q, want it to name the deciding signal that opened the gate", r.Note)
	}
	// The signals that did NOT decide must not be presented as if they had.
	if strings.Contains(r.Note, "calendar_table") {
		t.Errorf("Note = %q, want only the signals that actually fired and decided", r.Note)
	}
	// The gate being open is what makes suppression possible again, and the
	// suppression trace is the separate, pre-existing half of the story.
	if !strings.Contains(r.Note, "3NF-suppression") {
		t.Errorf("Note = %q, want the suppression trace alongside the gate trace", r.Note)
	}
	if hasSurfaceForTable(r.Res.Surface, "db-repeating-groups", "dim_product") {
		t.Error("DB-003 fired for dim_product inside a qualifying warehouse — suppression did not happen")
	}
}

// TestSensorDB_SchemaGateNote_ExplicitOverride_SaysWhoDecided keeps the two
// claims apart. "codefit judged this a warehouse" and "you told codefit this is
// a warehouse" are different statements, and only one of them is evidence.
func TestSensorDB_SchemaGateNote_ExplicitOverride_SaysWhoDecided(t *testing.T) {
	yaml := strings.Replace(yamlAuto, "  type: postgresql", "  type: postgresql\n  paradigm: mixed", 1)
	r := auditSchema(t, starNoEvidence, yaml)

	if !strings.Contains(r.Note, "database.paradigm: mixed") {
		t.Errorf("Note = %q, want it to name the explicit setting that opened the gate", r.Note)
	}
	if !strings.Contains(r.Note, "explicit") {
		t.Errorf("Note = %q, want it to say the gate was opened by configuration, not by evidence", r.Note)
	}
	// The developer's assertion wins: the roles are back, and suppression runs.
	if hasSurfaceForTable(r.Res.Surface, "db-repeating-groups", "dim_product") {
		t.Error("DB-003 fired for dim_product under explicit mixed — the override did not restore the dimension role")
	}
}

// TestSensorDB_SchemaGateNote_Empty_WhenTheGateChangedNothing: an ordinary
// transactional schema names no warehouse role, so the gate withheld nothing and
// granted nothing. A note there would be noise on every scan codefit ever runs.
func TestSensorDB_SchemaGateNote_Empty_WhenTheGateChangedNothing(t *testing.T) {
	plain := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model orders {
  id          Int      @id
  customer_id Int
  customer    customers @relation(fields: [customer_id], references: [id])
}

model customers {
  id     Int    @id
  name   String
  orders orders[]
}

model products {
  id   Int    @id
  name String
}
`
	r := auditSchema(t, plain, yamlAuto)
	if strings.Contains(r.Note, "Schema gate") {
		t.Errorf("Note = %q, want no schema-gate trace when the gate changed nothing (never spam)", r.Note)
	}
}

// TestSensorDB_SchemaGateNote_Empty_WhenAnOpenGateGrantedNothing is the OTHER
// no-consequence path, and it exists because the test above does not reach it:
// that schema closes the gate, so it exercises the withheld-nothing branch and
// leaves the granted-nothing branch untested. Proven by mutation — deleting the
// granted-nothing guard left the whole suite green until this case was written.
//
// An OPEN gate is permission to classify, not a classification. dim_date opens
// it (a declared calendar), but nothing references dim_date so the dimension
// candidate is demoted and no table ends up holding a warehouse role. Nothing
// changed, so nothing is reported.
func TestSensorDB_SchemaGateNote_Empty_WhenAnOpenGateGrantedNothing(t *testing.T) {
	calendarOnly := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model dim_date {
  id    Int    @id
  label String
}

model orders {
  id     Int    @id
  status String
}

model customers {
  id   Int    @id
  name String
}
`
	r := auditSchema(t, calendarOnly, yamlAuto)

	if r.Schema == nil {
		t.Fatal("nil schema")
	}
	// Positive probe: the gate really is OPEN here, or this test is asserting
	// the absence of a note for the wrong reason.
	if cls := paradigmDetect(r); !cls.Gate.Open {
		t.Fatalf("the schema gate is CLOSED on this fixture (Fired = %v) — it must be OPEN for this "+
			"test to exercise the granted-nothing branch", cls.Gate.Fired)
	}
	if strings.Contains(r.Note, "Schema gate") {
		t.Errorf("Note = %q, want no schema-gate trace when an open gate granted no role (never spam)", r.Note)
	}
}

// paradigmDetect re-runs detection over the sensor's parsed schema, so a test
// can assert the gate's state directly rather than inferring it from the note it
// is about to check.
func paradigmDetect(r sdb.Result) paradigm.Classification {
	return paradigm.Detect(r.Schema)
}
