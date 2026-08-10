package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestHandleCoverage_TypeScript_UnchangedFromPreChange is test contract item
// 3: TypeScript's coverage answer must be UNCHANGED by this change — its
// hand-written prose manifest stays authoritative, never replaced by the R1
// derived floor. The golden is the REAL pre-change response (commit
// 810b816, this branch's base) captured via `git worktree add --detach` and
// dumped with json.MarshalIndent over resp.Manifest — not a
// re-implementation of what the old manifest "should" say (see
// testdata/README.md's established pattern for these captures).
//
// Compared field for field (the whole Manifest, byte for byte): this
// change adds NOTHING to coverage.Manifest itself and never touches
// internal/providers/typescript/coverage.go, so TypeScript's manifest must
// be IDENTICAL, not merely "minus one added key" the way the scan-all
// goldens are (this response's only NEW field is the sibling
// CoverageResponse.Derived, which this test does not marshal — it compares
// resp.Manifest alone).
func TestHandleCoverage_TypeScript_UnchangedFromPreChange(t *testing.T) {
	resp, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleCoverage: %v", err)
	}
	if resp.Derived {
		t.Error("typescript must still serve its hand-written manifest, Derived must be false")
	}

	live, err := json.MarshalIndent(resp.Manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "coverage_ts_prechange.json"))
	if err != nil {
		t.Fatalf("reading pre-change golden: %v", err)
	}
	if string(live) != string(golden) {
		t.Errorf("typescript's coverage manifest changed from the pre-change (810b816) response — " +
			"R1's derived-manifest fallback must never touch a language that already has a hand-written one")
	}
}
