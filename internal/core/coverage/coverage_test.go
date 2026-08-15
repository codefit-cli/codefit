package coverage_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/coverage"
)

// sample is a manifest with an entry in every bucket, so a test that walks the
// index exercises all four statuses rather than whichever one happens to be
// first, and with TWO entries in one bucket, so a partial loss is
// distinguishable from a total one. Detail is deliberately absent on one entry:
// has_detail is a fact about that entry, not a constant.
func sample() coverage.Manifest {
	return coverage.Manifest{
		Language: "testlang",
		Deterministic: []coverage.Entry{
			{ID: "SEC-001", Claim: "a credential-named variable assigned a literal", Detail: "the long prose"},
			// A second entry in one bucket, so a control that drops SOME entries
			// is distinguishable from one that drops all of them. With one entry
			// per bucket an off-by-one over a bucket empties it, and the failure
			// looks identical to forgetting the bucket entirely.
			{ID: "SEC-002", Claim: "MD5 or SHA-1 hashing, wherever it appears", Detail: "the weak-crypto prose"},
		},
		Reasoning: []coverage.Entry{
			{ID: "surface.idor", Claim: "handlers reaching a resource by client-controlled id", Detail: "more prose"},
		},
		NotCovered: []coverage.Entry{
			{ID: "db.never-used-index", Claim: "an unused index needs runtime telemetry codefit cannot read"},
		},
		DeliveredElsewhere: []coverage.Entry{
			{ID: "db.nplus1-delivered-as-surface", Claim: "DB-201 ships as the provider's nplus1 surface category", Detail: "the mapping"},
		},
	}
}

// TestIndex_ConservesEveryEntry is the load-bearing control of this change (I5).
// An entry that disappears from the index is a declared limit the agent never
// learns about — the exact silent loss codefit exists to prevent. The budget
// authorizes withholding for scan-all; for coverage it authorizes nothing.
func TestIndex_ConservesEveryEntry(t *testing.T) {
	m := sample()
	authored := len(m.Deterministic) + len(m.Reasoning) + len(m.NotCovered) + len(m.DeliveredElsewhere)
	if authored == 0 {
		t.Fatal("vacuum: the fixture authored no entries, so conservation would hold trivially")
	}

	idx := m.Index()
	if len(idx) != authored {
		// Errorf, not Fatalf: the loop below is what NAMES the lost entry, and a
		// count alone would make an author hunt for which one went missing.
		t.Errorf("the index dropped entries: authored %d, indexed %d", authored, len(idx))
	}

	got := map[string]bool{}
	for _, e := range idx {
		got[e.ID] = true
	}
	for _, bucket := range [][]coverage.Entry{m.Deterministic, m.Reasoning, m.NotCovered, m.DeliveredElsewhere} {
		for _, e := range bucket {
			if !got[e.ID] {
				t.Errorf("entry %q was authored but is missing from the index", e.ID)
			}
		}
	}
}

// TestIndex_StatusComesFromTheBucket locks D1: status is NOT a field an author
// can set, so an entry structurally cannot carry a status disagreeing with the
// bucket it was written in.
func TestIndex_StatusComesFromTheBucket(t *testing.T) {
	want := map[string]coverage.Status{
		"SEC-001":                        coverage.StatusDeterministic,
		"SEC-002":                        coverage.StatusDeterministic,
		"surface.idor":                   coverage.StatusReasoning,
		"db.never-used-index":            coverage.StatusNotCovered,
		"db.nplus1-delivered-as-surface": coverage.StatusDeliveredElsewhere,
	}
	seen := 0
	for _, e := range sample().Index() {
		w, ok := want[e.ID]
		if !ok {
			t.Errorf("unexpected id in the index: %q", e.ID)
			continue
		}
		if e.Status != w {
			t.Errorf("entry %q: status %q, want %q", e.ID, e.Status, w)
		}
		seen++
	}
	if seen != len(want) {
		t.Fatalf("checked %d entries, the fixture declares %d", seen, len(want))
	}
}

// TestIndex_HasDetailIsAFactNotAConstant: an index that always says has_detail
// would send the agent after prose that does not exist.
func TestIndex_HasDetailIsAFactNotAConstant(t *testing.T) {
	with, without := 0, 0
	for _, e := range sample().Index() {
		if e.HasDetail {
			with++
		} else {
			without++
		}
	}
	if with == 0 || without == 0 {
		t.Fatalf("has_detail did not discriminate: %d true, %d false", with, without)
	}
	for _, e := range sample().Index() {
		if e.ID == "db.never-used-index" && e.HasDetail {
			t.Error("db.never-used-index has no Detail, so has_detail must be false")
		}
		if e.ID == "SEC-001" && !e.HasDetail {
			t.Error("SEC-001 carries Detail, so has_detail must be true")
		}
	}
}

// TestResolve_ReturnsTheProseByteForByte: detail is a lookup in the same
// in-memory value the index projects, so what comes back is what was authored.
func TestResolve_ReturnsTheProseByteForByte(t *testing.T) {
	found, unrecognized := sample().Resolve([]string{"SEC-001", "surface.idor"})
	if len(unrecognized) != 0 {
		t.Fatalf("both ids are real, got unrecognized: %v", unrecognized)
	}
	if len(found) != 2 {
		t.Fatalf("resolved %d entries, want 2", len(found))
	}
	byID := map[string]coverage.Entry{}
	for _, e := range found {
		byID[e.ID] = e
	}
	if byID["SEC-001"].Detail != "the long prose" {
		t.Errorf("SEC-001 detail: %q, want %q", byID["SEC-001"].Detail, "the long prose")
	}
	if byID["surface.idor"].Detail != "more prose" {
		t.Errorf("surface.idor detail: %q, want %q", byID["surface.idor"].Detail, "more prose")
	}
}

// TestResolve_NamesAnUnrecognizedID: an empty success would tell the agent the
// entry has nothing to say, which is a different and false answer from "that id
// does not exist".
func TestResolve_NamesAnUnrecognizedID(t *testing.T) {
	found, unrecognized := sample().Resolve([]string{"SEC-001", "NOPE-999"})
	if len(found) != 1 || found[0].ID != "SEC-001" {
		t.Fatalf("the real id must still resolve, got %d entries: %+v", len(found), found)
	}
	if len(unrecognized) != 1 || unrecognized[0] != "NOPE-999" {
		t.Fatalf("NOPE-999 must be named as unrecognized, got %v", unrecognized)
	}
}

// TestIndexAndResolve_AreABijection is the spec's structural guarantee: the
// index is a projection of the same values Resolve reads, so an id the agent
// sees is always an id the agent can expand, in both directions.
func TestIndexAndResolve_AreABijection(t *testing.T) {
	m := sample()
	ids := make([]string, 0, len(m.Index()))
	for _, e := range m.Index() {
		ids = append(ids, e.ID)
	}
	if len(ids) == 0 {
		t.Fatal("vacuum: no ids in the index")
	}
	found, unrecognized := m.Resolve(ids)
	if len(unrecognized) != 0 {
		t.Errorf("ids visible in the index did not resolve: %v", unrecognized)
	}
	if len(found) != len(ids) {
		t.Errorf("index has %d ids but only %d resolved", len(ids), len(found))
	}
	for _, e := range found {
		var inIndex bool
		for _, i := range m.Index() {
			if i.ID == e.ID {
				inIndex = true
			}
		}
		if !inIndex {
			t.Errorf("resolvable id %q is not visible in the index", e.ID)
		}
	}
}

// TestMaxClaimBytes_IsDerivedFromTheResponseBudget guards the arithmetic that
// produced the constant, so a later edit to the number has to restate the
// reasoning rather than just pick a rounder one.
func TestMaxClaimBytes_IsDerivedFromTheResponseBudget(t *testing.T) {
	const responseBudget = 40_000 // ADR 0062, mirrored here rather than imported (core must not import mcp)
	const expectedEntries = 70
	const framing = 60

	if coverage.MaxClaimBytes <= 0 {
		t.Fatal("MaxClaimBytes must be positive")
	}
	if worst := expectedEntries * (coverage.MaxClaimBytes + framing); worst >= responseBudget {
		t.Errorf("the derivation no longer holds: %d entries × (%d + %d framing) = %d, budget is %d — "+
			"re-run the derivation before raising the cap",
			expectedEntries, coverage.MaxClaimBytes, framing, worst, responseBudget)
	}
}

// TestIndex_ClaimIsNeverEmpty: an empty claim is an I5 failure wearing an id —
// the entry is counted, named, and says nothing.
func TestIndex_ClaimIsNeverEmpty(t *testing.T) {
	idx := sample().Index()
	if len(idx) == 0 {
		t.Fatal("vacuum: nothing to check")
	}
	for _, e := range idx {
		if strings.TrimSpace(e.Claim) == "" {
			t.Errorf("entry %q has an empty claim", e.ID)
		}
	}
}
