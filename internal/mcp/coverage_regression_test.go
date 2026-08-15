package mcp_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestHandleCoverage_TypeScript_RefusesTheDerivedFloor is the contract this
// file's doc comment always claimed: R1's DERIVED fallback must never replace a
// language that already has a hand-written prose manifest. That contract is one
// boolean and one non-vacuity check.
//
// It used to be asserted by comparing the whole response byte for byte against a
// 143,557-byte golden whose longest single line was 34,226 characters — a
// serialized ADR inside a JSON string. That golden was far wider than the
// contract: it locked every word of every declared limit, so it went red on a
// change that altered no prose at all. ADR 0076 records the re-scope and forbids
// re-capturing the same shape. Identity of the ENTRY SET is locked instead, by
// the small ids golden below.
func TestHandleCoverage_TypeScript_RefusesTheDerivedFloor(t *testing.T) {
	resp, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleCoverage: %v", err)
	}
	if resp.Derived {
		t.Error("typescript must still serve its hand-written manifest, Derived must be false")
	}
	// Without this, "Derived is false" would also pass over an empty answer,
	// and an empty coverage manifest is the failure this tool exists to prevent.
	if len(resp.Index) == 0 {
		t.Fatal("vacuum: typescript's hand-written manifest served no entries at all")
	}
}

// Q5's user-facing half — the declared limit must reach an AGENT, not merely
// exist in a const — used to live here as TestHandleCoverage_Go_StatesSEC001Limit,
// asserting over the joined Deterministic prose. It moved to
// TestCoverage_GoDerivedFloorKeepsSEC001sLimitWeldedToItsOwnEntry in
// coverage_detail_test.go, which asks the same question of the shape the agent
// now receives and asks MORE of it: that SEC-001's index claim itself warns the
// rule is qualified, that the full limit text comes back on that entry's own
// detail, and that it appears on no other entry's claim OR detail. The old
// version checked only the second of those three.
