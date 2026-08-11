package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// THE DEFECT THIS FILE CLOSES.
//
// budgetNote derived its fit claim from the WITHHELD-ENDPOINT COUNT, not from
// the response's size. With zero endpoints withheld it returned, unconditionally:
//
//	"the complete endpoint list fit within this response's 40000-byte budget:
//	 NOTHING was withheld, every endpoint codefit classified is named here."
//
// fitToBudget had already MEASURED the opposite and handed it over as
// `stillOver`; the note simply returned before consulting it. So a DB-heavy
// project with no security provider — zero endpoints, a large `db.surface` —
// received a response that exceeded its declared budget while asserting it fit.
// The measurement was there; only the sentence lied.
//
// Both tests below assert `stillOver` FIRST. Without that assertion a fixture
// that is not actually over budget passes vacuously, which is exactly how a test
// like this comes to protect nothing.

// oversizedDBResponse builds the shape observed in a real project: no
// endpoints in any bucket, and a db.surface large enough ON ITS OWN to
// exceed the budget.
func oversizedDBResponse(budget int) ScanAllResponse {
	var surface []findings.SurfaceItem
	for i := 0; i < 200; i++ {
		surface = append(surface, findings.SurfaceItem{
			Category: "db-table-structure-unproven",
			File:     "prisma/schema.prisma",
			Line:     i + 1,
			Snippet:  strings.Repeat("this table's structure could not be proven complete; read the raw statement. ", 6),
		})
	}
	return ScanAllResponse{
		Budget: BudgetBlock{Bytes: budget},
		DB:     &DBSection{Measured: true, Surface: surface, Score: 100},
	}
}

// C1 — a zero-endpoint response that does not fit says so.
func TestScanAllBudget_ZeroEndpointsOverBudget_DoesNotClaimItFit(t *testing.T) {
	const budget = ResponseBudgetBytes
	resp := oversizedDBResponse(budget)

	fitted, stillOver := fitToBudget(resp, budget)

	// FIRST: prove the fixture reproduces the shape. If it fits, every
	// assertion below would pass without exercising the branch at all.
	if !stillOver {
		raw, _ := json.Marshal(fitted)
		t.Fatalf("the fixture is NOT over budget (%d bytes vs %d) — this test would pass vacuously", len(raw), budget)
	}
	if fitted.Budget.Withheld != 0 {
		t.Fatalf("Withheld = %d, want 0 — the shape under test is over budget with NOTHING to withhold", fitted.Budget.Withheld)
	}

	note := fitted.Budget.Note
	if strings.Contains(note, "fit within this response") || strings.Contains(note, "NOTHING was withheld") {
		t.Errorf("the note claims the complete list fit a budget the response measurably exceeds:\n%s", note)
	}
	if !strings.Contains(note, "does NOT fit") {
		t.Errorf("the note does not state that the response exceeds its budget:\n%s", note)
	}
	if !strings.Contains(note, "no endpoints in this response") {
		t.Errorf("the note does not say WHY nothing could be withheld:\n%s", note)
	}
	if !strings.Contains(note, "changed_files") {
		t.Errorf("the note does not tell the agent what to do about it:\n%s", note)
	}
}

// C1b — the mirror control: a genuinely small, fully-fitting response is
// unaffected, and still affirms the fit rather than staying silent about it.
func TestScanAllBudget_SmallFittingResponse_StillAffirmsTheFit(t *testing.T) {
	const budget = ResponseBudgetBytes
	resp := ScanAllResponse{
		Budget: BudgetBlock{Bytes: budget},
		DB:     &DBSection{Measured: true, Score: 100},
	}

	fitted, stillOver := fitToBudget(resp, budget)
	if stillOver {
		t.Fatalf("the control fixture is over budget; it is supposed to fit")
	}
	if !strings.Contains(fitted.Budget.Note, "fit within this response") {
		t.Errorf("a response that genuinely fits no longer says so:\n%s", fitted.Budget.Note)
	}
	if strings.Contains(fitted.Budget.Note, "does NOT fit") {
		t.Errorf("a response that fits is reported as over budget:\n%s", fitted.Budget.Note)
	}
}

// C1c — the two reasons nothing could be withheld are DIFFERENT facts and the
// note must not conflate them. "There are no endpoints" and "every endpoint
// carries a deterministic finding codefit refuses to hide" lead an agent to
// different next steps.
func TestScanAllBudget_OverBudgetWithPinnedEndpoints_SaysWhyNothingWasWithheld(t *testing.T) {
	const budget = 2_000
	// `total` is the complete endpoint count codefit classified. Three endpoints
	// classified and none withheld can only mean every one of them was pinned.
	note := budgetNote(budget, 3, 0, 0, 0, true)
	if !strings.Contains(note, "every endpoint carries a deterministic finding") {
		t.Errorf("with endpoints present, the note still blames their absence:\n%s", note)
	}
	if strings.Contains(note, "no endpoints in this response") {
		t.Errorf("the note claims there are no endpoints in a response that has 3:\n%s", note)
	}
}
