package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
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

// TestHandleCoverage_Go_StatesSEC001Limit is Q5's user-facing half: the
// declared limit must reach an AGENT, not merely exist in a const.
//
// It is asserted on the SAME STRING as the SEC-001 claim, not on a sibling
// array, and that placement is the decision being locked. An agent cannot read
// "SEC-001 (security rule, declared)" and miss the caveat, because there is no
// version of the line without it. A separate DeclaredLimits array would be
// droppable by any summariser between codefit and the model — which is the
// failure mode this repo's coverage chain exists to prevent.
func TestHandleCoverage_Go_StatesSEC001Limit(t *testing.T) {
	resp, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "go"})
	if err != nil {
		t.Fatalf("HandleCoverage(go): %v", err)
	}
	if !resp.Derived {
		t.Error("go has no hand-written manifest, so its answer must be Derived (ADR 0065)")
	}
	if len(resp.Manifest.Deterministic) == 0 {
		t.Fatal("vacuum: go's derived manifest declares no deterministic rules at all")
	}

	var sec001 string
	for _, d := range resp.Manifest.Deterministic {
		if strings.HasPrefix(d, "SEC-001 ") {
			sec001 = d
		}
	}
	if sec001 == "" {
		t.Fatalf("no SEC-001 line in go's Deterministic list: %v", resp.Manifest.Deterministic)
	}
	if !strings.Contains(sec001, namematch.LimitLowercaseConcatenation) {
		t.Errorf("SEC-001's line does not carry the declared limit.\n got: %s\nwant it to contain: %s",
			sec001, namematch.LimitLowercaseConcatenation)
	}

	// The limit must be attached to SEC-001 and to nothing else. A limit
	// smeared across every rule id would be worse than absent: it would
	// under-claim five rules that have no such gap.
	for _, d := range resp.Manifest.Deterministic {
		if d == sec001 {
			continue
		}
		if strings.Contains(d, namematch.LimitLowercaseConcatenation) {
			t.Errorf("the SEC-001 limit leaked onto another rule's line: %s", d)
		}
	}
}
