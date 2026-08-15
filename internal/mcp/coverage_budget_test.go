package mcp

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestCoverage_AnOverBudgetIndexKeepsEveryEntryAndSaysSo is the missing guard
// over the one branch of the coverage budget that had none.
//
// The detail branch has been covered since the size declaration was fixed
// (TestCoverage_ADetailResponseDeclaresTheSizeOfWhatItReturns). The INDEX branch
// was not: nothing in the suite asserted coverageOverBudgetNote, and a correct
// behaviour with no control is exactly what drifts. The behaviour was proved
// correct by hand at review time and then left unguarded — the same half-closure
// this test exists to end.
//
// WHY A LOWERED BUDGET AND NOT A SYNTHETIC MANIFEST. The branch is unreachable
// at the shipped budget: the real index is ~22 KB against 40,000. Growing a
// fixture until it crosses 40,000 would measure the fixture's arithmetic; the
// answer under test here is the one codefit actually serves, so the manifest
// stays real and the budget moves. handleCoverageBudgeted is the same seam
// handleScanAllBudgeted already is, for the same reason.
//
// THE HONEST TRIPLE. Coverage authorizes NO withholding under any condition, so
// an index that does not fit is still complete. The response therefore has to
// say three things at once, and it is the conjunction that is load-bearing:
// every entry is still there, withheld is still zero, and the response admits it
// is over budget anyway. Any two of the three can be satisfied by a response
// that lies about the third — a complete index that calls itself within budget
// is byte for byte the defect PR #128 closed for scan-all, and a response that
// admits the overage by dropping entries is the failure this whole change
// exists to prevent.
//
// The note is checked too, because the note IS the instruction. An over-budget
// index is an AUTHORING problem — someone wrote a claim too long — and the only
// acceptable repair is to shorten a claim. Telling that caller to ask for fewer
// ids per call, which is the detail branch's advice, would send them to fix
// something that is not broken.
func TestCoverage_AnOverBudgetIndexKeepsEveryEntryAndSaysSo(t *testing.T) {
	// The reference answer at the SHIPPED budget. It also pins the precondition:
	// if the real index ever grows past 40,000 this test's premise is gone and
	// the fixture-free setup below stops meaning what it says.
	shipped, err := HandleCoverage(CoverageRequest{Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleCoverage(typescript): %v", err)
	}
	if len(shipped.Index) == 0 {
		t.Fatal("vacuum: the index is empty, so every check below would pass by saying nothing")
	}
	if shipped.OverBudget {
		t.Fatalf("the shipped index is %d bytes and already over the %d-byte budget — this test can no "+
			"longer tell an over-budget answer apart from the everyday one", shipped.Bytes, ResponseBudgetBytes)
	}

	// Low enough that the index ALONE exceeds it, derived from the measured index
	// rather than typed in, so it cannot go stale as the manifest grows.
	budget := shipped.IndexBytes / 2
	if budget <= 0 {
		t.Fatalf("the index measured %d bytes, so there is no budget below it", shipped.IndexBytes)
	}

	resp, err := handleCoverageBudgeted(CoverageRequest{Language: "typescript"}, budget)
	if err != nil {
		t.Fatalf("handleCoverageBudgeted(typescript, %d): %v", budget, err)
	}

	// The size figure has to describe what is being carried, measured here rather
	// than taken from the response's own account of itself. No detail was asked
	// for, so the payload IS the index.
	raw, err := json.Marshal(resp.Index)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if resp.IndexBytes != len(raw) {
		t.Errorf("the response declares an index of %d bytes and serializes %d", resp.IndexBytes, len(raw))
	}
	if resp.Bytes != resp.IndexBytes {
		t.Errorf("no detail was asked for, so the payload IS the index: bytes=%d index_bytes=%d",
			resp.Bytes, resp.IndexBytes)
	}
	if resp.Bytes <= budget {
		t.Fatalf("the answer measured %d bytes against a %d-byte budget — it does not exceed it, so the "+
			"branch under test never ran", resp.Bytes, budget)
	}

	// 1. EVERY ENTRY IS STILL THERE. Anchored to the shipped answer's ids, not to
	// the response's own Entries field: a count computed from a truncated index
	// agrees with the truncation just as happily as with a complete answer.
	onTheWire := map[string]bool{}
	for _, e := range resp.Index {
		onTheWire[e.ID] = true
	}
	var dropped []string
	for _, e := range shipped.Index {
		if !onTheWire[e.ID] {
			dropped = append(dropped, e.ID)
		}
	}
	if len(dropped) > 0 {
		sort.Strings(dropped)
		t.Errorf("%d of %d entries were dropped to fit the budget: %v\n"+
			"Coverage authorizes no withholding: an index that does not fit is reported, never trimmed.",
			len(dropped), len(shipped.Index), dropped)
	}
	if len(resp.Index) != len(shipped.Index) {
		t.Errorf("the over-budget answer carries %d entries and the within-budget one carries %d",
			len(resp.Index), len(shipped.Index))
	}
	if resp.Entries != len(resp.Index) {
		t.Errorf("the response declares %d entries and the index holds %d", resp.Entries, len(resp.Index))
	}

	// 2. NOTHING WAS WITHHELD, AND IT SAYS SO. Silence about truncation and "we
	// truncated nothing" are not the same bytes to a reader who cannot see both.
	if resp.Withheld != 0 {
		t.Errorf("coverage withheld %d entries — the budget authorizes withholding for scan-all, "+
			"for coverage it authorizes nothing", resp.Withheld)
	}
	if resp.WithheldNote == "" {
		t.Error("the over-budget answer withheld nothing and does not SAY so")
	}

	// 3. AND IT ADMITS THE OVERAGE ANYWAY.
	if !resp.OverBudget {
		t.Errorf("the index is %d bytes against a %d-byte budget and does not declare itself over it — "+
			"a response that reports a size verdict smaller than what it carries is the defect PR #128 "+
			"closed for scan-all", resp.Bytes, budget)
	}
	if resp.BudgetNote == "" {
		t.Fatal("an over-budget response must SAY it is over budget — silence and 'within budget' are not the same bytes")
	}
	if !strings.Contains(resp.BudgetNote, "still complete") {
		t.Errorf("the note does not tell the caller the over-budget index is COMPLETE: %q", resp.BudgetNote)
	}
	if !strings.Contains(resp.BudgetNote, "Shorten a claim, never drop an entry") {
		t.Errorf("an over-budget INDEX is an authoring problem and the note does not give the authoring "+
			"repair: %q", resp.BudgetNote)
	}
	// The detail branch's note would satisfy a looser check and point the caller
	// at the wrong repair, so the two are told apart explicitly.
	if resp.BudgetNote == coverageDetailOverBudgetNote {
		t.Errorf("nothing was asked for by id, and the answer blames the caller for the detail it "+
			"requested: %q", resp.BudgetNote)
	}

	t.Logf("OVER-BUDGET INDEX — entries %d (of %d shipped), bytes %d, index_bytes %d, budget %d, "+
		"over_budget %v, withheld %d", resp.Entries, len(shipped.Index), resp.Bytes, resp.IndexBytes,
		budget, resp.OverBudget, resp.Withheld)
}
