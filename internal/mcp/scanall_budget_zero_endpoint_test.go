package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surfaceindex"
	"github.com/codefit-cli/codefit/internal/schemasource"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// THE DEFECT THIS FILE CLOSES.
//
// budgetNote derived its fit claim from the WITHHELD-ENDPOINT COUNT, not from
// the response's size. With zero endpoints withheld it returned, unconditionally:
//
//	"the complete endpoint list fit within this response's 40000-byte budget:
//	 NOTHING was withheld, every endpoint codefit classified is named here."
//
// fitToBudget had already MEASURED the opposite and handed it over as
// `stillOver`; the note simply returned before consulting it. So a DB-heavy
// project with no security provider — zero endpoints, a large `db.surface` —
// received a response that exceeded its declared budget while asserting it fit.
// The measurement was there; only the sentence lied.
//
// Both tests below assert `stillOver` FIRST. Without that assertion a fixture
// that is not actually over budget passes vacuously, which is exactly how a test
// like this comes to protect nothing.

// generateNoTimestampSchema builds n CREATE TABLE statements, each missing
// any audit-timestamp column, so DB-052 (surface.CategoryDBNoTimestamps)
// fires once per table when driven through the REAL parser + db sensor
// (design D7 — never a hand-assembled findings.SurfaceItem, the exact
// CLAUDE.md fixture-rule violation the pre-change fixture committed).
func generateNoTimestampSchema(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "CREATE TABLE t%d (\n  id INTEGER PRIMARY KEY,\n  name TEXT NOT NULL\n);\n\n", i)
	}
	return b.String()
}

// writeSchemaProject writes a minimal SQL-DDL project (config + schema file)
// into root, ready for the real config/schemasource/dbsensor pipeline or a
// full MCP handler call.
func writeSchemaProject(t *testing.T, root, sql string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "db", "schema.sql"), []byte(sql), 0o644); err != nil {
		t.Fatal(err)
	}
	const yaml = "version: \"1\"\nproject:\n  name: t\n  language: typescript\n  framework: next\ndatabase:\n" +
		"  type: postgresql\n  schema_paths:\n    - db/schema.sql\n"
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

// realNoTimestampSurfaceItems drives the REAL sqlddl parser and the REAL db
// sensor over n generated tables and returns the sensor's own
// findings.SurfaceItem population — every field (ID, Fingerprint,
// StructuralFacts, ReasonToReview) stamped by the production pipeline, never
// typed by hand. It FATALs if the generator's assumption (one DB-052 item per
// table) did not hold, rather than silently proceeding with a fixture that
// no longer reproduces what it claims to.
func realNoTimestampSurfaceItems(t *testing.T, n int) []findings.SurfaceItem {
	t.Helper()
	root := t.TempDir()
	writeSchemaProject(t, root, generateNoTimestampSchema(n))
	cfg, err := config.LoadOptional(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	parser, note := schemasource.ParserForPaths(root, cfg.Database.SchemaPaths, cfg.Database.Type)
	if parser == nil {
		t.Fatalf("no schema parser resolved: %s", note)
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "typescript", Config: cfg}
	r, err := dbsensor.New(parser).Audit(ctx)
	if err != nil {
		t.Fatalf("db sensor audit: %v", err)
	}
	if !r.Measured {
		t.Fatalf("db sensor did not measure the generated schema: %s", r.Note)
	}
	if len(r.Res.Surface) != n {
		t.Fatalf("the generated %d-table schema produced %d surface item(s) — the fixture generator's "+
			"assumption (one DB-052 item per table) no longer holds; this fixture reproduces nothing "+
			"until that is fixed", n, len(r.Res.Surface))
	}
	return r.Res.Surface
}

// oversizedDBResponse builds the shape observed in a real project: no
// endpoints in any bucket, and a db.surface large enough ON ITS OWN to
// exceed the budget — even AFTER indexing (design D7: the pre-change fixture
// stopped reproducing once db.surface became the light index, because its
// only heavy field was Snippet, which the index drops). The item count is
// large enough to survive that at the shipped index's measured weight; the
// vacuity guard in the caller is what actually PROVES it, not this comment.
func oversizedDBResponse(t *testing.T, budget int) ScanAllResponse {
	t.Helper()
	const n = 400
	items := realNoTimestampSurfaceItems(t, n)
	entries, count := surfaceindex.Index(items)
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("oversizedDBResponse: %d items, index %d bytes total (%.1f B/item, measured here, not frozen)",
		count, len(raw), float64(len(raw))/float64(count))
	return ScanAllResponse{
		Budget: BudgetBlock{Bytes: budget},
		DB:     &DBSection{Measured: true, Surface: entries, Count: count, WithheldNote: dbWithheldNote, Score: 100},
	}
}

// C1 — a zero-endpoint response that does not fit says so.
func TestScanAllBudget_ZeroEndpointsOverBudget_DoesNotClaimItFit(t *testing.T) {
	const budget = ResponseBudgetBytes
	resp := oversizedDBResponse(t, budget)

	fitted, stillOver := fitToBudget(resp, budget)

	// FIRST: prove the fixture reproduces the shape. If it fits, every
	// assertion below would pass without exercising the branch at all.
	if !stillOver {
		raw, _ := json.Marshal(fitted)
		t.Fatalf("the fixture is NOT over budget (%d bytes vs %d) — this test would pass vacuously", len(raw), budget)
	}
	if fitted.Budget.Withheld != 0 {
		t.Fatalf("Withheld = %d, want 0 — the shape under test is over budget with NOTHING to withhold", fitted.Budget.Withheld)
	}

	note := fitted.Budget.Note
	if strings.Contains(note, "fit within this response") || strings.Contains(note, "NOTHING was withheld") {
		t.Errorf("the note claims the complete list fit a budget the response measurably exceeds:\n%s", note)
	}
	if !strings.Contains(note, "does NOT fit") {
		t.Errorf("the note does not state that the response exceeds its budget:\n%s", note)
	}
	if !strings.Contains(note, "no endpoints in this response") {
		t.Errorf("the note does not say WHY nothing could be withheld:\n%s", note)
	}
	if !strings.Contains(note, "changed_files") {
		t.Errorf("the note does not tell the agent what to do about it:\n%s", note)
	}
}

// C1b — the mirror control: a genuinely small, fully-fitting response is
// unaffected, and still affirms the fit rather than staying silent about it.
func TestScanAllBudget_SmallFittingResponse_StillAffirmsTheFit(t *testing.T) {
	const budget = ResponseBudgetBytes
	resp := ScanAllResponse{
		Budget: BudgetBlock{Bytes: budget},
		DB:     &DBSection{Measured: true, Score: 100},
	}

	fitted, stillOver := fitToBudget(resp, budget)
	if stillOver {
		t.Fatalf("the control fixture is over budget; it is supposed to fit")
	}
	if !strings.Contains(fitted.Budget.Note, "fit within this response") {
		t.Errorf("a response that genuinely fits no longer says so:\n%s", fitted.Budget.Note)
	}
	if strings.Contains(fitted.Budget.Note, "does NOT fit") {
		t.Errorf("a response that fits is reported as over budget:\n%s", fitted.Budget.Note)
	}
}

// C1c — the two reasons nothing could be withheld are DIFFERENT facts and the
// note must not conflate them. "There are no endpoints" and "every endpoint
// carries a deterministic finding codefit refuses to hide" lead an agent to
// different next steps.
func TestScanAllBudget_OverBudgetWithPinnedEndpoints_SaysWhyNothingWasWithheld(t *testing.T) {
	const budget = 2_000
	// `total` is the complete endpoint count codefit classified. Three endpoints
	// classified and none withheld can only mean every one of them was pinned.
	note := budgetNote(budget, 3, 0, 0, 0, true)
	if !strings.Contains(note, "every endpoint carries a deterministic finding") {
		t.Errorf("with endpoints present, the note still blames their absence:\n%s", note)
	}
	if strings.Contains(note, "no endpoints in this response") {
		t.Errorf("the note claims there are no endpoints in a response that has 3:\n%s", note)
	}
}
