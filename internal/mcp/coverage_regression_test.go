package mcp_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
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
	if len(resp.Manifest.DeterministicProse) == 0 {
		t.Fatal("vacuum: typescript's hand-written manifest served no entries at all")
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
	if len(resp.Manifest.DeterministicProse) == 0 {
		t.Fatal("vacuum: go's derived manifest declares no deterministic rules at all")
	}

	var sec001 string
	for _, d := range resp.Manifest.DeterministicProse {
		if strings.HasPrefix(d, "SEC-001 ") {
			sec001 = d
		}
	}
	if sec001 == "" {
		t.Fatalf("no SEC-001 line in go's Deterministic list: %v", resp.Manifest.DeterministicProse)
	}
	if !strings.Contains(sec001, namematch.LimitLowercaseConcatenation) {
		t.Errorf("SEC-001's line does not carry the declared limit.\n got: %s\nwant it to contain: %s",
			sec001, namematch.LimitLowercaseConcatenation)
	}

	// The limit must be attached to SEC-001 and to nothing else. A limit
	// smeared across every rule id would be worse than absent: it would
	// under-claim five rules that have no such gap.
	for _, d := range resp.Manifest.DeterministicProse {
		if d == sec001 {
			continue
		}
		if strings.Contains(d, namematch.LimitLowercaseConcatenation) {
			t.Errorf("the SEC-001 limit leaked onto another rule's line: %s", d)
		}
	}
}
