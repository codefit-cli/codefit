package mcp_test

import (
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// goYAMLWithSchema configures a Go project (no resolvable security provider)
// with a SQL-DDL schema — the DB dimension's parser does not depend on
// req.Language, so this is the smallest fixture that proves the DB dimension
// runs independently of the language resolution.
const goYAMLWithSchema = `version: "1"
project:
  name: t
  language: go
database:
  type: postgresql
  schema_paths:
    - db/schema.sql
`

// goYAMLNoSchema is the same Go project with no database section at all — the
// nothing-measurable case (Phase 3): no security provider AND no DB dimension.
const goYAMLNoSchema = `version: "1"
project:
  name: t
  language: go
`

const goNoPKSchema = `CREATE TABLE no_key (
  name TEXT NOT NULL
);
`

// writeGoSchemaProj is the Go+schema fixture shared by this file's tests: a
// minimal Go module with a configured SQL-DDL schema.
func writeGoSchemaProj(t *testing.T) string {
	t.Helper()
	return writeProj(t, map[string]string{
		".codefit.yaml": goYAMLWithSchema,
		"db/schema.sql": goNoPKSchema,
		"main.go":       "package main\n\nfunc main() {}\n",
		"go.mod":        "module example.com/t\n\ngo 1.25\n",
	})
}

// TestHandleScanAll_GoProjectWithSchema_AuditsDB is the P0-5 defect made a RED
// test (spec: "scan-all measures DB without a resolved provider"): a Go
// project has no resolvable security provider, but its configured schema must
// still be measured by the DB dimension, and the handler must not error.
func TestHandleScanAll_GoProjectWithSchema_AuditsDB(t *testing.T) {
	root := writeGoSchemaProj(t)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "go"})
	if err != nil {
		t.Fatalf("HandleScanAll over a Go project with a configured schema must not error, got: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured over a Go project's schema, got %+v", resp.DB)
	}
	if resp.Score.ByDimension[findings.DimensionSecurity] != nil {
		t.Errorf("security must be NOT MEASURED (nil) for a Go project, got %+v",
			resp.Score.ByDimension[findings.DimensionSecurity])
	}
	if resp.Score.ByDimension[findings.DimensionDB] == nil {
		t.Error("db must be measured (non-nil) for a Go project with a configured schema")
	}
}

// TestHandleScanAll_SecuritySkipped_DoesNotPruneSecurityBaseline is the D2
// lock: invariant SCANNED-OPT-IN. A security-owned baseline item (authz) must
// survive a DB-only scan-all pass over a Go project — neither marked Gone nor
// listed as a GoneCandidate — and a following codefit-baseline-prune must
// leave it in the file.
//
// Mutation proof (recorded manually during apply, not committed): removing the
// `if secRan` guard around the `scanned` union reproduces the exact defect
// this test locks — `Gone=1`, the security item wrongly pruned. Restoring the
// guard turns it green again.
func TestHandleScanAll_SecuritySkipped_DoesNotPruneSecurityBaseline(t *testing.T) {
	root := writeGoSchemaProj(t)

	// A security-owned item already tracked in the committed baseline (from a
	// prior run when a security provider resolved for this project), owned by
	// the security sensor which will NOT run this pass (no provider for "go").
	bl := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "authzfp", Category: "authz", File: "app/x/route.ts", Snippet: "requirePermission missing"},
	}}
	if err := bl.Save(filepath.Join(root, baseline.Name)); err != nil {
		t.Fatal(err)
	}

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "go"})
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

	// codefit-baseline-prune must also leave the item in place: for a Go project
	// (no security provider), the prune re-scan itself cannot run (it always
	// requires a full security re-scan), so the file must be untouched either
	// way — the item is never a prune candidate.
	if _, err := mcp.HandleBaselinePrune(mcp.BaselinePruneRequest{Root: root, Language: "go"}); err == nil {
		t.Log("baseline-prune unexpectedly succeeded for a Go project — checking the file directly")
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
