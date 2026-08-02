package db_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// ADR 0043 — the withholding trace.
//
// The failure these tests exist to end was MEASURED through this exact path:
// a schema whose only statement was a CREATE UNLOGGED TABLE audited as
// Measured=true, Note="", findings=0, surface=0, over a schema codefit had
// never read. "Audited, 0 findings" over unread DDL is the worst state an
// auditor can be in, and it is indistinguishable from a clean bill of health.
//
// Withholding is a DIFFERENT fact from unreducibility and gets a different
// trace, but the rule it obeys is the same one: it is never silent. codefit
// decides; the developer is told what that decision cost them.

// auditSQL drives the REAL sqlddl parser through the REAL Sensor.Audit — the
// production path, not a hand-built db.Schema, which can hold shapes the
// reducer never produces.
func auditSQL(t *testing.T, dbType, sql string) sdb.Result {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "schema.sql"), []byte(sql), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: \"1\"\nproject:\n  name: t\n  language: java\n  framework: spring\ndatabase:\n  type: " + dbType +
		"\n  schema_paths:\n    - schema.sql\n"
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	r, err := sdb.New(sqlddl.New()).Audit(auditctx.AuditContext{ProjectRoot: root, Language: "java", Config: cfg})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	return r
}

func TestSensorDB_WithheldNote_DeclaresWhatWasNotModeled(t *testing.T) {
	r := auditSQL(t, "postgresql",
		"CREATE TABLE orders (id integer PRIMARY KEY);\n"+
			"CREATE TEMPORARY TABLE stage_orders (id integer);\n")

	if !r.Measured {
		t.Fatalf("Measured = false, want true")
	}
	for _, want := range []string{"stage_orders", "1", "did NOT model"} {
		if !strings.Contains(r.Note, want) {
			t.Errorf("Note = %q, want it to contain %q", r.Note, want)
		}
	}
	// The reason must reach the note from the core's closed vocabulary, so the
	// developer learns WHY it was withheld and not merely that it was.
	if !strings.Contains(r.Note, "session") {
		t.Errorf("Note = %q, want it to state the reason (session-scoped)", r.Note)
	}
	// The measurement/diagnostics boundary (ADR 0034 §2.8): the note carries an
	// inventory, never parser telemetry.
	for _, forbidden := range []string{".go:", "func ", "regex", "dispatch", "reducer"} {
		if strings.Contains(r.Note, forbidden) {
			t.Errorf("Note = %q, contains the parser-internal token %q", r.Note, forbidden)
		}
	}
}

// The measured false-clean state, asserted directly: a schema codefit modeled
// NOTHING out of must not audit as a silent success.
func TestSensorDB_WithheldOnly_IsNotAFalseCleanScan(t *testing.T) {
	r := auditSQL(t, "postgresql", "CREATE TEMP TABLE scratch (id integer);\n")

	if len(r.Res.Findings) != 0 || len(r.Res.Surface) != 0 {
		t.Fatalf("findings=%d surface=%d, want 0/0 — a temporary table must not reach any rule", len(r.Res.Findings), len(r.Res.Surface))
	}
	if r.Note == "" {
		t.Errorf("Note is empty with 0 findings and 0 surface over a schema whose only statement was not modeled — this is the false 'audited, 0 findings' state")
	}
}

// The catcher's half of the same guarantee, through the same channel: an
// unrecognized table-shaped head reaches the developer as an inventory line,
// not as silence.
func TestSensorDB_UnrecognizedTableShape_ReachesTheNote(t *testing.T) {
	r := auditSQL(t, "postgresql", "CREATE FOREIGN TABLE external_orders (id integer) SERVER remote_srv;\n")

	if r.Note == "" {
		t.Fatalf("Note is empty — a CREATE ... TABLE head the reducer could not reduce left no trace at all")
	}
	if !strings.Contains(r.Note, "could not be reduced") {
		t.Errorf("Note = %q, want the unreduced-statement inventory line", r.Note)
	}
	if !strings.Contains(r.Note, "schema.sql:1") {
		t.Errorf("Note = %q, want the file:line of the statement so the agent can read the source", r.Note)
	}
}

// Never spammed. Every ordinary transactional scan must land here.
func TestSensorDB_WithheldNote_EmptyWhenNothingIsWithheld(t *testing.T) {
	r := auditSQL(t, "postgresql", "CREATE TABLE orders (id integer PRIMARY KEY, created_at timestamp);\n")

	if r.Note != "" {
		t.Errorf("Note = %q, want empty — nothing was withheld and nothing was unreduced", r.Note)
	}
}

// BOUNDED. A migration suite that stages 200 temporary tables is ONE line
// naming the reason, a count and a sample — never 200 lines. Developer
// autonomy requires the trace; it does not require an unreadable one.
func TestSensorDB_WithheldNote_IsBoundedByReasonNotByTable(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE orders (id integer PRIMARY KEY);\n")
	const n = 200
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "CREATE TEMPORARY TABLE stage_%03d (id integer);\n", i)
	}
	r := auditSQL(t, "postgresql", sb.String())

	if !strings.Contains(r.Note, "200") {
		t.Errorf("Note = %q, want the full count (200)", r.Note)
	}
	if !strings.Contains(r.Note, "(+195 more)") {
		t.Errorf("Note = %q, want it to elide all but the first 5 names", r.Note)
	}
	if strings.Contains(r.Note, "stage_006") {
		t.Errorf("Note = %q, names a table past the cap — the trace is not bounded", r.Note)
	}
	if len(r.Note) > 1000 {
		t.Errorf("Note is %d bytes for 200 withheld tables — the trace must be O(1) in schema size:\n%s", len(r.Note), r.Note)
	}
}

// The traces COMPOSE and their order is FIXED, so each qualifies the ones after
// it: what codefit could not measure, then what it chose not to model, then the
// schema-gate verdict those two shaped.
func TestSensorDB_WithheldNote_ComposesAfterTheCompletenessInventory(t *testing.T) {
	r := auditSQL(t, "postgresql",
		"CREATE TABLE orders (id integer);\n"+
			"ALTER TABLE orders INHERIT parent_a;\n"+
			"CREATE TEMPORARY TABLE stage_orders (id integer);\n")

	inventory := strings.Index(r.Note, "could not prove the structure")
	withheld := strings.Index(r.Note, "stage_orders")
	if inventory < 0 {
		t.Fatalf("Note = %q, want the completeness inventory (the unreduced ALTER TABLE)", r.Note)
	}
	if withheld < 0 {
		t.Fatalf("Note = %q, want the withholding trace", r.Note)
	}
	if inventory > withheld {
		t.Errorf("Note = %q, want the measurement inventory BEFORE the withholding trace", r.Note)
	}
}
