package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// TestCoverage_EntryIDsMatchTheGolden locks the identity of the ENTRY SET, which
// is what the deleted 143 KB golden was actually protecting worth protecting.
// Ids are append-only and a rename is a breaking change, so this golden's churn
// is meaningful: every line of a diff here is an id that appeared or vanished,
// and no line of it is a reworded sentence.
//
// Deliberately ids and statuses only. Re-capturing the prose in the new shape
// would re-import the exact defect ADR 0076 removed.
func TestCoverage_EntryIDsMatchTheGolden(t *testing.T) {
	type entryID struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	live := map[string][]entryID{}
	for _, lang := range []string{"typescript", "go"} {
		resp, err := mcp.HandleCoverage(mcp.CoverageRequest{Language: lang})
		if err != nil {
			t.Fatalf("HandleCoverage(%s): %v", lang, err)
		}
		if len(resp.Index) == 0 {
			t.Fatalf("vacuum: %s indexed nothing, so the golden would lock an empty answer", lang)
		}
		for _, e := range resp.Index {
			live[lang] = append(live[lang], entryID{ID: e.ID, Status: string(e.Status)})
		}
	}

	raw, err := json.MarshalIndent(live, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join("testdata", "coverage_entry_ids.json")
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the entry-id golden: %v", err)
	}
	if string(raw) == string(golden) {
		return
	}

	// Name what moved. A byte diff over ids is readable, but naming the added and
	// removed ids is what tells an author whether they renamed something on
	// purpose or dropped a declared limit by accident.
	var want map[string][]entryID
	if err := json.Unmarshal(golden, &want); err != nil {
		t.Fatalf("parsing the entry-id golden: %v", err)
	}
	for lang, got := range live {
		had := map[string]string{}
		for _, e := range want[lang] {
			had[e.ID] = e.Status
		}
		has := map[string]bool{}
		for _, e := range got {
			has[e.ID] = true
			status, known := had[e.ID]
			switch {
			case !known:
				t.Errorf("%s: entry id %q is NEW — ids are append-only, and a rename is a breaking change needing an ADR", lang, e.ID)
			case status != e.Status:
				t.Errorf("%s: entry %q moved from %s to %s", lang, e.ID, status, e.Status)
			}
		}
		for _, e := range want[lang] {
			if !has[e.ID] {
				t.Errorf("%s: entry id %q is GONE — a declared limit the agent can no longer ask for", lang, e.ID)
			}
		}
	}
	if !t.Failed() {
		t.Errorf("the entry-id golden differs but no id changed — the ORDER moved, which reorders the agent's index")
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
