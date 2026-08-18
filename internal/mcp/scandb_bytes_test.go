package mcp

import (
	"strings"
	"testing"
)

// TestScanDB_OverBudget_IndexOnly_SaysSoAndStaysComplete drives the REAL
// sensor's REAL output through a deliberately lowered budget (the seam
// handleScanDBBudgeted exists for, mirroring handleCoverageBudgeted) rather
// than a synthetic struct — task 6.1/6.2, design D5. With no detail
// requested, an over-budget index gets the INDEX-over note.
func TestScanDB_OverBudget_IndexOnly_SaysSoAndStaysComplete(t *testing.T) {
	root := t.TempDir()
	writeSchemaProject(t, root, generateNoTimestampSchema(400))

	resp, err := handleScanDBBudgeted(ScanDBRequest{Root: root, Language: "typescript"}, 2_000)
	if err != nil {
		t.Fatalf("handleScanDBBudgeted: %v", err)
	}
	if !resp.Measured {
		t.Fatalf("Measured=false, want true; note=%q", resp.Note)
	}
	if resp.Count == 0 {
		t.Fatal("Count is 0 — the fixture produced no measurable index")
	}
	if len(resp.Surface) != resp.Count {
		t.Fatalf("an index-only response must stay COMPLETE even over budget: len(Surface)=%d, Count=%d",
			len(resp.Surface), resp.Count)
	}
	if !resp.OverBudget {
		t.Fatalf("Bytes=%d must exceed the 2000-byte budget for a 400-item index", resp.Bytes)
	}
	if resp.BudgetNote != dbIndexOverBudgetNote {
		t.Errorf("BudgetNote = %q, want the INDEX-over note (no detail was requested)", resp.BudgetNote)
	}
}

// TestScanDB_OverBudget_WithDetail_DifferentNote proves the SECOND note:
// when detail crosses the budget (not the bare index), the note is
// different — an authoring problem for the index vs. "you asked for a lot at
// once" for detail (design D5, mirroring coverage's two notes).
func TestScanDB_OverBudget_WithDetail_DifferentNote(t *testing.T) {
	root := t.TempDir()
	// Fewer items than the index-only test above: the INDEX must fit the
	// shipped 40,000-byte budget on its own so this test isolates the detail
	// branch; the heavier per-item DETAIL (snippet + signals + reason_to_review)
	// is what pushes the combined response over.
	writeSchemaProject(t, root, generateNoTimestampSchema(100))

	index, err := handleScanDBBudgeted(ScanDBRequest{Root: root, Language: "typescript"}, ResponseBudgetBytes)
	if err != nil {
		t.Fatalf("handleScanDBBudgeted (index): %v", err)
	}
	if index.OverBudget {
		t.Fatal("the index alone must fit the shipped budget for this sub-test to isolate the detail branch")
	}
	ids := make([]string, 0, len(index.Surface))
	for _, e := range index.Surface {
		ids = append(ids, e.ID)
	}

	resp, err := handleScanDBBudgeted(ScanDBRequest{Root: root, Language: "typescript", Detail: ids}, ResponseBudgetBytes)
	if err != nil {
		t.Fatalf("handleScanDBBudgeted (detail): %v", err)
	}
	if !resp.OverBudget {
		t.Fatalf("requesting detail for all %d items must cross the %d-byte budget, Bytes=%d", len(ids), ResponseBudgetBytes, resp.Bytes)
	}
	if resp.BudgetNote != dbDetailOverBudgetNote {
		t.Errorf("BudgetNote = %q, want the DETAIL-over note", resp.BudgetNote)
	}
	if resp.BudgetNote == dbIndexOverBudgetNote {
		t.Error("the detail-over-budget response must NOT reuse the index-over note")
	}
	if len(resp.Detail) != len(ids) {
		t.Errorf("an over-budget DETAIL response must still return every requested item: got %d, want %d", len(resp.Detail), len(ids))
	}
	if !strings.Contains(resp.BudgetNote, "fewer ids") {
		t.Errorf("the detail-over note must tell the agent what to do about it: %q", resp.BudgetNote)
	}
}
