package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// unresolvedYAMLWithSchema configures a project in an UNREGISTERED language
// (no resolvable security provider — "python" is not in
// internal/providers/registry) with a SQL-DDL schema — the DB dimension's
// parser does not depend on req.Language, so this is the smallest fixture
// that proves the DB dimension runs independently of the language
// resolution. Was "go" before roadmap P4-1 exposed Go's security provider
// (docs/specs/declared-partial-language-exposure.md): the DB-only scenario
// this file locks is a property of "no resolvable provider", not of Go
// specifically, so the fixture moved to a language that still has none —
// the guard's own behaviour is unchanged.
const unresolvedYAMLWithSchema = `version: "1"
project:
  name: t
  language: python
database:
  type: postgresql
  schema_paths:
    - db/schema.sql
`

// unresolvedYAMLNoSchema is the same unresolved-language project with no
// database section at all — the nothing-measurable case (Phase 3): no
// security provider AND no DB dimension.
const unresolvedYAMLNoSchema = `version: "1"
project:
  name: t
  language: python
`

const unresolvedNoPKSchema = `CREATE TABLE no_key (
  name TEXT NOT NULL
);
`

// writeUnresolvedSchemaProj is the unresolved-language+schema fixture shared
// by this file's tests: a project in a language with no resolvable security
// provider, carrying a configured SQL-DDL schema.
func writeUnresolvedSchemaProj(t *testing.T) string {
	t.Helper()
	return writeProj(t, map[string]string{
		".codefit.yaml": unresolvedYAMLWithSchema,
		"db/schema.sql": unresolvedNoPKSchema,
	})
}

// TestHandleScanAll_UnresolvedLanguageWithSchema_AuditsDB is the P0-5 defect
// made a RED test (spec: "scan-all measures DB without a resolved provider"):
// a project has no resolvable security provider, but its configured schema
// must still be measured by the DB dimension, and the handler must not
// error.
func TestHandleScanAll_UnresolvedLanguageWithSchema_AuditsDB(t *testing.T) {
	root := writeUnresolvedSchemaProj(t)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err != nil {
		t.Fatalf("HandleScanAll over an unresolved-language project with a configured schema must not error, got: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured over the project's schema, got %+v", resp.DB)
	}
	if resp.Score.ByDimension[findings.DimensionSecurity] != nil {
		t.Errorf("security must be NOT MEASURED (nil) for an unresolved-language project, got %+v",
			resp.Score.ByDimension[findings.DimensionSecurity])
	}
	if resp.Score.ByDimension[findings.DimensionDB] == nil {
		t.Error("db must be measured (non-nil) for an unresolved-language project with a configured schema")
	}
}

// TestHandleScanAll_SecuritySkipped_DoesNotPruneSecurityBaseline is the D2
// lock: invariant SCANNED-OPT-IN. A security-owned baseline item (authz) must
// survive a DB-only scan-all pass over an unresolved-language project —
// neither marked Gone nor listed as a GoneCandidate — and a following
// codefit-baseline-prune must leave it in the file.
//
// Mutation proof (recorded manually during apply, not committed): removing the
// `if secRan` guard around the `scanned` union reproduces the exact defect
// this test locks — `Gone=1`, the security item wrongly pruned. Restoring the
// guard turns it green again.
func TestHandleScanAll_SecuritySkipped_DoesNotPruneSecurityBaseline(t *testing.T) {
	root := writeUnresolvedSchemaProj(t)

	// A security-owned item already tracked in the committed baseline (from a
	// prior run when a security provider resolved for this project), owned by
	// the security sensor which will NOT run this pass (no provider for
	// "python").
	bl := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "authzfp", Category: "authz", File: "app/x/route.ts", Snippet: "requirePermission missing"},
	}}
	if err := bl.Save(filepath.Join(root, baseline.Name)); err != nil {
		t.Fatal(err)
	}

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Baseline.Gone != 0 {
		t.Errorf("a DB-only run must NOT mark the security item gone, got Gone=%d (%+v)",
			resp.Baseline.Gone, resp.Baseline.GoneCandidates)
	}
	for _, g := range resp.Baseline.GoneCandidates {
		if g.Fingerprint == "authzfp" {
			t.Errorf("the security item must not appear in gone_candidates, got %+v", resp.Baseline.GoneCandidates)
		}
	}

	after, err := baseline.Load(filepath.Join(root, baseline.Name))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range after.Items {
		if it.FP == "authzfp" {
			found = true
		}
	}
	if !found {
		t.Error("the security item must survive in the baseline after a DB-only scan-all run")
	}

	// codefit-baseline-prune must also leave the item in place: for an
	// unresolved-language project (no security provider), the prune re-scan
	// itself cannot run (it always requires a full security re-scan), so the
	// file must be untouched either way — the item is never a prune
	// candidate.
	if _, err := mcp.HandleBaselinePrune(mcp.BaselinePruneRequest{Root: root, Language: "python"}); err == nil {
		t.Log("baseline-prune unexpectedly succeeded for an unresolved-language project — checking the file directly")
	}
	afterPrune, err := baseline.Load(filepath.Join(root, baseline.Name))
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, it := range afterPrune.Items {
		if it.FP == "authzfp" {
			found = true
		}
	}
	if !found {
		t.Error("codefit-baseline-prune must leave the security item in the file")
	}
}

// TestHandleScanAll_NothingMeasurable_Errors is the D5 lock: when NEITHER a
// security provider resolves NOR the DB dimension runs, codefit-scan-all must
// return an error naming both missing inputs (plus the supported language
// set), not a 200-shaped response with every dimension null. The guard must
// fire BEFORE any baseline write.
func TestHandleScanAll_NothingMeasurable_Errors(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": unresolvedYAMLNoSchema, // language: python, no database section at all
	})

	_, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err == nil {
		t.Fatal("HandleScanAll over a project with no resolvable provider and no schema must return an error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"python"`) {
		t.Errorf("error must name the unresolved language, got %q", msg)
	}
	if !strings.Contains(msg, "database.schema_paths") {
		t.Errorf("error must name the missing database.schema_paths, got %q", msg)
	}
	if !strings.Contains(msg, "typescript") {
		t.Errorf("error must name the supported language set, got %q", msg)
	}
	if _, statErr := os.Stat(filepath.Join(root, baseline.Name)); !os.IsNotExist(statErr) {
		t.Errorf("the nothing-measurable guard must fire BEFORE any baseline write, but %s exists", baseline.Name)
	}
}

// TestHandleScanAll_UnresolvedLanguageWithSchema_SecurityNotMeasured is D3b:
// the Security section is ALWAYS present (never a zero value silently
// standing in for "not applicable"), and on a DB-only pass it honestly
// reports Measured=false with a non-empty note naming that the schema was
// audited while the code was not.
func TestHandleScanAll_UnresolvedLanguageWithSchema_SecurityNotMeasured(t *testing.T) {
	root := writeUnresolvedSchemaProj(t)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Security.Measured {
		t.Error("security must be reported NOT measured for an unresolved-language project (no provider)")
	}
	note := strings.ToLower(resp.Security.Note)
	if note == "" {
		t.Fatal("security.note must be non-empty when security did not run")
	}
	if !strings.Contains(note, "schema") || !strings.Contains(note, "code") {
		t.Errorf("security.note must say the schema was audited but the code was not, got %q", resp.Security.Note)
	}
}

// TestHandleScanAll_UnresolvedLanguageWithSchema_BucketNotesNonEmpty is the
// D1 site-15 lock: when security did not run,
// actionable/resolved_clean/frontier_pending are all zero — but the zero is
// because security did not run, NOT because codefit looked and found
// nothing, and each note must say so.
func TestHandleScanAll_UnresolvedLanguageWithSchema_BucketNotesNonEmpty(t *testing.T) {
	root := writeUnresolvedSchemaProj(t)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Actionable.Count != 0 || resp.ResolvedClean.Count != 0 || resp.FrontierPending.Count != 0 {
		t.Fatalf("an unresolved-language project with no security provider must have zero endpoints in every bucket, got %+v/%+v/%+v",
			resp.Actionable, resp.ResolvedClean, resp.FrontierPending)
	}
	for name, note := range map[string]string{
		"actionable":       resp.Actionable.Note,
		"resolved_clean":   resp.ResolvedClean.Note,
		"frontier_pending": resp.FrontierPending.Note,
	} {
		if note == "" {
			t.Errorf("%s.note must be non-empty when the zero count is because security did not run", name)
			continue
		}
		if !strings.Contains(strings.ToLower(note), "security did not run") {
			t.Errorf("%s.note must say the zero is because security did not run, got %q", name, note)
		}
	}
}

// TestHandleScanAll_UnresolvedLanguageWithSchema_AuditableTotalReflectsSchema
// is D3: on a DB-only pass, scope.auditable_total must reflect the distinct
// schema sources the DB dimension actually read — not the security-only
// value of 0, which would falsely assert "no auditable files exist".
func TestHandleScanAll_UnresolvedLanguageWithSchema_AuditableTotalReflectsSchema(t *testing.T) {
	root := writeUnresolvedSchemaProj(t)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Scope.AuditableTotal != 1 {
		t.Errorf("scope.auditable_total must equal the 1 distinct schema source read, got %d", resp.Scope.AuditableTotal)
	}
	if resp.Scope.Mode != mcp.ScopeModeFull || resp.Scope.Note != "" {
		t.Errorf("a full, unnarrowed scan must stay mode=full with no note, got mode=%q note=%q",
			resp.Scope.Mode, resp.Scope.Note)
	}
}

// TestHandleScanAll_UnresolvedLanguageWithSchema_CrossSkipNoted is D6: the DB
// section's note must name the skipped code x schema cross and the reason —
// an unresolved language has no QueryExtractor, so the cross rule family
// never runs on this path, and silence about it would look like the cross
// simply had nothing to say.
func TestHandleScanAll_UnresolvedLanguageWithSchema_CrossSkipNoted(t *testing.T) {
	root := writeUnresolvedSchemaProj(t)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "python"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured, got %+v", resp.DB)
	}
	note := strings.ToLower(resp.DB.Note)
	if !strings.Contains(note, "cross") {
		t.Errorf("db.note must name the skipped code x schema cross, got %q", resp.DB.Note)
	}
	if !strings.Contains(note, "query extractor") {
		t.Errorf("db.note must state there is no query extractor for this language, got %q", resp.DB.Note)
	}
}
