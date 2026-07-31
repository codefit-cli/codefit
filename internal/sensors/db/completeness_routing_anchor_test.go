package db_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// F1/F8 (4R ledger, obs #1282, CRITICAL + pairing WARNING) — the SQL-DDL
// reducer never set db.Table.Pos, so DB-050's routed db-table-structure-
// unproven item anchored on file:line ""/0 for EVERY unproven table in a
// project: StampSurface then computed an IDENTICAL Fingerprint and StableID
// for all of them. One codefit-baseline-accept would have silenced the
// category project-wide, and codefit-confirm-surface could never tell which
// table a verdict was about.
//
// This is a PRODUCTION-PATH lock, deliberately NOT hand-building
// db.Table{Pos: ...} the way dbrules/completeness_gate_test.go does — that
// fixture is MORE CAPABLE than the real reducer and is exactly why the
// original bug survived a green suite (4R's own diagnosis). This test drives
// the real sqlddl.Parser through Sensor.Audit, over a schema with TWO
// separately unproven tables, and asserts their routed items anchor at
// DIFFERENT, non-empty file:line and therefore carry DIFFERENT fingerprints.
func TestSensorDB_RoutedUnprovenItems_AnchorAtRealPositions_ProductionPath(t *testing.T) {
	root := t.TempDir()
	sql := "CREATE TABLE first_table (id int);\n" +
		"CREATE TABLE second_table (id int);\n" +
		"ALTER TABLE first_table INHERIT parent_a;\n" +
		"ALTER TABLE second_table INHERIT parent_b;\n"
	if err := os.WriteFile(filepath.Join(root, "schema.sql"), []byte(sql), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: \"1\"\nproject:\n  name: t\n  language: java\n  framework: spring\ndatabase:\n  type: postgresql\n  schema_paths:\n    - schema.sql\n"
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "java", Config: cfg}
	r, err := sdb.New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}

	var routed []struct {
		File, Line, FP, ID string
	}
	for _, it := range r.Res.Surface {
		if it.Category != string(surface.CategoryDBTableStructureUnproven) {
			continue
		}
		routed = append(routed, struct{ File, Line, FP, ID string }{
			File: it.File, Line: strconv.Itoa(it.Line), FP: it.Fingerprint, ID: it.ID,
		})
	}
	if len(routed) != 2 {
		t.Fatalf("routed db-table-structure-unproven items = %d, want 2 (first_table, second_table)", len(routed))
	}
	for _, item := range routed {
		if item.File == "" || item.Line == "0" {
			t.Errorf("item = %+v, want a real, non-empty file:line — production reducer must set Table.Pos (F1)", item)
		}
	}
	if routed[0].File == routed[1].File && routed[0].Line == routed[1].Line {
		t.Errorf("both items anchor at the SAME position %s:%s — Table.Pos is not distinguishing the two tables (F1)", routed[0].File, routed[0].Line)
	}
	if routed[0].FP == routed[1].FP {
		t.Errorf("both items share Fingerprint %q — one baseline-accept would silence BOTH tables at once (F1)", routed[0].FP)
	}
	if routed[0].ID == routed[1].ID {
		t.Errorf("both items share ID %q — codefit-confirm-surface could not distinguish them (F1)", routed[0].ID)
	}
}
