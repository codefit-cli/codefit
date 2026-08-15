package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/coverage"
	"github.com/codefit-cli/codefit/internal/core/namematch"
	"github.com/codefit-cli/codefit/internal/mcp"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

func coverageOf(t *testing.T, req mcp.CoverageRequest) mcp.CoverageResponse {
	t.Helper()
	resp, err := mcp.HandleCoverage(req)
	if err != nil {
		t.Fatalf("HandleCoverage(%+v): %v", req, err)
	}
	return resp
}

// TestCoverage_IndexNamesEveryEntryAndDeclaresItsOwnSize is I5 at the wire: the
// response says how many entries it holds, how many bytes it took, and that it
// withheld nothing — in words, not by staying quiet about it. Silence and "we
// dropped nothing" are not the same bytes.
func TestCoverage_IndexNamesEveryEntryAndDeclaresItsOwnSize(t *testing.T) {
	resp := coverageOf(t, mcp.CoverageRequest{Language: "typescript"})

	if len(resp.Index) == 0 {
		t.Fatal("vacuum: the index is empty, so every check below would pass by saying nothing")
	}
	if resp.Entries != len(resp.Index) {
		t.Errorf("the response declares %d entries but the index holds %d", resp.Entries, len(resp.Index))
	}
	if resp.Withheld != 0 {
		t.Errorf("coverage withheld %d entries — the budget authorizes no withholding here", resp.Withheld)
	}
	if resp.WithheldNote == "" {
		t.Error("the response withheld nothing and does not SAY so — an agent cannot tell that apart from a response that stayed quiet about truncating")
	}
	if resp.Bytes <= 0 {
		t.Errorf("the response does not declare its own size: bytes = %d", resp.Bytes)
	}
	raw, err := json.Marshal(resp.Index)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if resp.Bytes != len(raw) {
		t.Errorf("declared %d bytes, the serialized index is %d", resp.Bytes, len(raw))
	}

	// Entries counts the index, so it agrees with a TRUNCATED index just as
	// happily as with a complete one. Conservation is therefore anchored to the
	// producer: the real provider's real manifest, not a struct built here, and
	// not the response's own account of itself.
	authored := typescript.New().CoverageManifest().Index()
	if len(authored) == 0 {
		t.Fatal("vacuum: the provider authored no entries, so conservation would hold trivially")
	}
	onTheWire := map[string]bool{}
	for _, e := range resp.Index {
		onTheWire[e.ID] = true
	}
	for _, e := range authored {
		if !onTheWire[e.ID] {
			t.Errorf("entry %q is declared by the provider and never reaches the wire — "+
				"a declared limit the agent cannot learn about", e.ID)
		}
	}
	if len(resp.Index) != len(authored) {
		t.Errorf("the provider declares %d entries and the response carries %d", len(authored), len(resp.Index))
	}
}

// TestCoverage_IndexCarriesNoPayloadSizedString is the acceptance bar for this
// whole change. The pre-change response carried a single 34,226-character
// string — a serialized ADR inside a JSON list — and no declared limit may ever
// be shaped like that again in an index.
func TestCoverage_IndexCarriesNoPayloadSizedString(t *testing.T) {
	resp := coverageOf(t, mcp.CoverageRequest{Language: "typescript"})
	if len(resp.Index) == 0 {
		t.Fatal("vacuum: nothing to measure")
	}
	longest := 0
	var worst string
	for _, e := range resp.Index {
		if len(e.Claim) > longest {
			longest, worst = len(e.Claim), e.ID
		}
	}
	if longest > coverage.MaxClaimBytes {
		t.Errorf("entry %q carries a %d-byte claim in the index, over the %d-byte cap — shorten the claim, never drop the entry",
			worst, longest, coverage.MaxClaimBytes)
	}
}

// TestCoverage_DetailReturnsTheProseByteForByte: an id that appears in the index
// must expand to exactly what was authored, with nothing rewritten on the way
// out. The id under test is the one that used to BE the payload problem.
func TestCoverage_DetailReturnsTheProseByteForByte(t *testing.T) {
	const id = "db.sqlddl-dialect-limits"

	index := coverageOf(t, mcp.CoverageRequest{Language: "typescript"})
	var listed *coverage.IndexEntry
	for i := range index.Index {
		if index.Index[i].ID == id {
			listed = &index.Index[i]
		}
	}
	if listed == nil {
		t.Fatalf("%s is not in the index at all", id)
	}
	if !listed.HasDetail {
		t.Fatalf("%s is the longest declared limit codefit publishes and the index says it has no detail", id)
	}

	resp := coverageOf(t, mcp.CoverageRequest{Language: "typescript", Detail: []string{id}})
	if len(resp.Detail) != 1 || resp.Detail[0].ID != id {
		t.Fatalf("asked for %s, got %d entries back: %+v", id, len(resp.Detail), resp.Detail)
	}
	if len(resp.Unrecognized) != 0 {
		t.Errorf("a real id came back unrecognized: %v", resp.Unrecognized)
	}
	// The prose must be the FULL declaration, not a summary of it. This is the
	// entry that was 34,226 characters, so a detail that came back short would
	// mean the answer was quietly abridged on the way out.
	if len(resp.Detail[0].Detail) < 30_000 {
		t.Errorf("%s's detail came back %d bytes — the authored prose is far longer, so something abridged it",
			id, len(resp.Detail[0].Detail))
	}
	// And the index still ships alongside it: asking for detail never costs the
	// agent the index it used to find the id.
	if len(resp.Index) != len(index.Index) {
		t.Errorf("asking for detail changed the index: %d entries, was %d", len(resp.Index), len(index.Index))
	}
}

// TestCoverage_ARuleIDIsAskableByName is what the per-rule split buys the agent,
// asked at the handler rather than at the leaf. Before the split a rule id like
// DB-011a existed only INSIDE another entry's detail: it was in the payload, but
// it was not in the index, so an agent could not name it and asking for it came
// back unrecognized. Now each rule is an entry of its own.
//
// The three ids under test come from three DIFFERENT pre-split blobs, so one
// unsplit blob cannot satisfy all of them, and each assertion reads the prose
// back to confirm the entry is ABOUT that rule rather than merely named after
// it — an entry keyed DB-011a carrying some other rule's paragraph would pass a
// lookup-only check.
func TestCoverage_ARuleIDIsAskableByName(t *testing.T) {
	cases := []struct {
		id   string
		says string // a phrase the rule's own prose must carry
	}{
		{"DB-011a", "Exact duplicate indexes"},
		{"DB-053", "Sensitive column stored in the clear"},
		{"DW-010", "SCD-2 dimension without a currency index"},
	}

	index := coverageOf(t, mcp.CoverageRequest{Language: "typescript"})
	listed := map[string]coverage.IndexEntry{}
	for _, e := range index.Index {
		listed[e.ID] = e
	}
	if len(listed) == 0 {
		t.Fatal("vacuum: the index is empty, so every lookup below would fail for the wrong reason")
	}

	for _, tc := range cases {
		e, ok := listed[tc.id]
		if !ok {
			t.Errorf("rule %s is not in the index — an agent cannot name it", tc.id)
			continue
		}
		if !e.HasDetail {
			t.Errorf("rule %s is in the index and declares no detail to ask for", tc.id)
		}
		resp := coverageOf(t, mcp.CoverageRequest{Language: "typescript", Detail: []string{tc.id}})
		if len(resp.Unrecognized) != 0 {
			t.Errorf("rule %s came back unrecognized: %v", tc.id, resp.Unrecognized)
			continue
		}
		if len(resp.Detail) != 1 || resp.Detail[0].ID != tc.id {
			t.Errorf("asked for %s, got %+v", tc.id, resp.Detail)
			continue
		}
		if !strings.Contains(coverage.ProseOf(resp.Detail[0]), tc.says) {
			t.Errorf("entry %s came back without its own subject %q — it is named after a rule it does not describe",
				tc.id, tc.says)
		}
	}
}

// TestCoverage_DetailTakesManyIDsAtOnce: one call, many ids. An agent that has
// to make one round trip per declared limit will stop asking.
func TestCoverage_DetailTakesManyIDsAtOnce(t *testing.T) {
	ids := []string{"db.sqlddl-dialect-limits", "DB-012", "ts.idor"}
	resp := coverageOf(t, mcp.CoverageRequest{Language: "typescript", Detail: ids})
	if len(resp.Detail) != len(ids) {
		t.Fatalf("asked for %d ids, got %d entries", len(ids), len(resp.Detail))
	}
	got := map[string]bool{}
	for _, e := range resp.Detail {
		got[e.ID] = true
		if coverage.ProseOf(e) == "" {
			t.Errorf("%s resolved to nothing at all", e.ID)
		}
	}
	for _, id := range ids {
		if !got[id] {
			t.Errorf("%s was asked for and did not come back", id)
		}
	}
}

// TestCoverage_UnrecognizedIDIsNamedNotSilentlyEmpty: an empty success would
// tell the agent the entry has nothing to say, which is a different and false
// answer from "there is no such entry".
func TestCoverage_UnrecognizedIDIsNamedNotSilentlyEmpty(t *testing.T) {
	resp := coverageOf(t, mcp.CoverageRequest{
		Language: "typescript",
		Detail:   []string{"DB-012", "NOPE-999"},
	})
	if len(resp.Detail) != 1 || resp.Detail[0].ID != "DB-012" {
		t.Fatalf("the real id must still resolve, got %+v", resp.Detail)
	}
	if len(resp.Unrecognized) != 1 || resp.Unrecognized[0] != "NOPE-999" {
		t.Fatalf("NOPE-999 must be NAMED as unrecognized, got %v", resp.Unrecognized)
	}
	if resp.UnrecognizedNote == "" {
		t.Error("an unrecognized id came back with no explanation of what that means")
	}

	// And it must survive serialization: an unexported field or a json:"-" would
	// satisfy every check above and vanish before the agent ever sees it.
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), "NOPE-999") {
		t.Error("the unrecognized id does not reach the agent's JSON")
	}
}

// TestCoverage_EveryIndexedIDResolves is the bijection at the wire: an id the
// agent can see is an id the agent can expand. A listed id that resolves to
// nothing is a dead end the response advertised.
func TestCoverage_EveryIndexedIDResolves(t *testing.T) {
	for _, lang := range []string{"typescript", "go"} {
		t.Run(lang, func(t *testing.T) {
			index := coverageOf(t, mcp.CoverageRequest{Language: lang})
			if len(index.Index) == 0 {
				t.Fatalf("vacuum: %s indexed nothing", lang)
			}
			ids := make([]string, 0, len(index.Index))
			for _, e := range index.Index {
				ids = append(ids, e.ID)
			}
			resp := coverageOf(t, mcp.CoverageRequest{Language: lang, Detail: ids})
			if len(resp.Unrecognized) != 0 {
				t.Errorf("ids the index advertised did not resolve: %v", resp.Unrecognized)
			}
			if len(resp.Detail) != len(ids) {
				t.Errorf("%d ids in the index, %d resolved", len(ids), len(resp.Detail))
			}
		})
	}
}

// TestCoverage_GoDerivedFloorKeepsSEC001sLimitWeldedToItsOwnEntry is ADR 0075
// carried into the new shape. The limit must stay on SEC-001's own entry, and
// the claim an agent reads without asking for anything must not let it think the
// rule is unqualified.
func TestCoverage_GoDerivedFloorKeepsSEC001sLimitWeldedToItsOwnEntry(t *testing.T) {
	index := coverageOf(t, mcp.CoverageRequest{Language: "go"})
	if !index.Derived {
		t.Fatal("go has no hand-written manifest, so its answer must be the derived floor")
	}
	if len(index.Index) == 0 {
		t.Fatal("vacuum: go's derived floor indexed nothing")
	}

	var sec001 coverage.IndexEntry
	for _, e := range index.Index {
		if e.ID == "SEC-001" {
			sec001 = e
		}
		// The limit's own text must never be smeared across other rules: that
		// would under-claim five rules that have no such gap.
		if e.ID != "SEC-001" && strings.Contains(e.Claim, namematch.LimitLowercaseConcatenation) {
			t.Errorf("SEC-001's declared limit leaked onto %s's claim", e.ID)
		}
	}
	if sec001.ID == "" {
		t.Fatal("go's derived floor does not carry a SEC-001 entry at all")
	}
	if !sec001.HasDetail {
		t.Error("SEC-001 carries a declared limit and its index entry says it has no detail")
	}
	if !strings.Contains(strings.ToLower(sec001.Claim), "limit") {
		t.Errorf("an agent reading SEC-001's claim alone gets no hint that it is qualified: %q", sec001.Claim)
	}

	resp := coverageOf(t, mcp.CoverageRequest{Language: "go", Detail: []string{"SEC-001"}})
	if len(resp.Detail) != 1 {
		t.Fatalf("SEC-001 did not resolve: %+v", resp)
	}
	if !strings.Contains(resp.Detail[0].Detail, namematch.LimitLowercaseConcatenation) {
		t.Errorf("SEC-001's detail does not carry the declared limit:\n%s", resp.Detail[0].Detail)
	}
	for _, e := range coverageOf(t, mcp.CoverageRequest{Language: "go", Detail: idsOf(index.Index)}).Detail {
		if e.ID != "SEC-001" && strings.Contains(e.Detail, namematch.LimitLowercaseConcatenation) {
			t.Errorf("SEC-001's declared limit also appears on %s's detail", e.ID)
		}
	}
}

func idsOf(index []coverage.IndexEntry) []string {
	out := make([]string, 0, len(index))
	for _, e := range index {
		out = append(out, e.ID)
	}
	return out
}
