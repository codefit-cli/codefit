package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/scope"
)

// --- baseline-write-gate spec, test contract items 1/2/3/5 ---------------------
//
// The defect, reproduced live against a real MCP client: a response the client
// REJECTED for exceeding its token limit had already written .codefit-baseline
// in full. The write happened ~100 lines before the response was returned, and
// before every check codefit performs on its own output. R1 moves the write
// after scoring.MissingWeights, ScopeBlock.Validate() and fitToBudget's
// stillOver — the census named stillOver the MOST dangerous of the three,
// because the probability of a response not fitting RISES with the number of
// findings, so the second run is quieter than the first exactly when it
// should be louder (item 5). Every assertion here reads the file directly
// with os.Stat / file bytes — never inferred from the returned error — the
// same assertion shape TestHandleScanAll_NothingMeasurable_Errors already
// uses for the D5 guard, extended to the delivery path that had no analogue.

// --- item 5: the stillOver path leaves the baseline untouched ------------------

func TestScanAllWriteGate_StillOver_NothingWritten(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)
	baselinePath := filepath.Join(root, baseline.Name)

	// 500 bytes cannot fit even the empty response (proven by
	// TestScanAllBudget_WithholdingIsDeclaredNeverSilent's "impossible" case at
	// the same budget) — the response is STILL over budget after withholding
	// every droppable endpoint. It must not reach its reader as a complete,
	// valid result.
	if _, err := handleScanAllBudgeted(ScanAllRequest{Root: root, Language: "typescript"},
		scope.Full(), baselinePath, 500); err != nil {
		t.Fatalf("a still-over response must still be RETURNED (not an error) with its WARNING note, got: %v", err)
	}

	if _, statErr := os.Stat(baselinePath); !os.IsNotExist(statErr) {
		t.Errorf("a response that is STILL OVER BUDGET after withholding everything must not write the baseline "+
			"— the client will not receive it as a complete result, but %s exists", baseline.Name)
	}
}

// --- item 2: a PRE-EXISTING baseline is not modified on a failed response ------
//
// Not merely "not created" — plant a baseline from a successful run, force the
// SAME project to fail on a much tighter budget, and assert the file's bytes
// are byte-for-byte identical to what the first run wrote.
func TestScanAllWriteGate_StillOver_PreExistingBaselineUntouched(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)
	baselinePath := filepath.Join(root, baseline.Name)

	// Plant a real baseline with a generous budget that fits.
	if _, err := handleScanAllBudgeted(ScanAllRequest{Root: root, Language: "typescript"},
		scope.Full(), baselinePath, ResponseBudgetBytes); err != nil {
		t.Fatalf("planting the baseline: %v", err)
	}
	before, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("reading the planted baseline: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("the planted baseline is empty — this test would prove nothing")
	}

	// Add a NEW endpoint the planted baseline has never seen, so a diff computed
	// on the second call would differ from the first — this is what makes the
	// assertion below discriminate a real write from a no-op re-save of
	// identical content (an unchanged project would pass even under the bug).
	newDir := filepath.Join(root, "app", "newthing")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, newDir, "route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.newThing.findUnique({ where: { id: params.id } }));
}`)

	// Same project (now larger), same call, a budget so small the response is
	// still over after withholding everything.
	if _, err := handleScanAllBudgeted(ScanAllRequest{Root: root, Language: "typescript"},
		scope.Full(), baselinePath, 500); err != nil {
		t.Fatalf("a still-over response must still be RETURNED, got: %v", err)
	}

	after, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("reading the baseline after the failed response: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("a failed response modified a PRE-EXISTING baseline (the new endpoint leaked in).\nbefore: %s\nafter:  %s", before, after)
	}
}

// --- item 3: the happy path is unchanged ----------------------------------------
//
// A response that fits its budget still gets its baseline written — the SAME
// content as before this change, just after the checks instead of before them.
func TestScanAllWriteGate_HappyPath_BaselineStillWritten(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)
	baselinePath := filepath.Join(root, baseline.Name)

	resp, err := handleScanAllBudgeted(ScanAllRequest{Root: root, Language: "typescript"},
		scope.Full(), baselinePath, ResponseBudgetBytes)
	if err != nil {
		t.Fatalf("HandleScanAll over a fixture well within budget: %v", err)
	}
	if resp.Budget.Withheld != 0 {
		t.Fatalf("the fixture must fit the real budget for this test to prove the happy path — withheld=%d", resp.Budget.Withheld)
	}
	if resp.Baseline.New == 0 {
		t.Fatal("the fixture produced no new baseline item — this test would prove nothing")
	}

	b, err := baseline.Load(baselinePath)
	if err != nil {
		t.Fatalf("the baseline must exist and load after a response that fit its budget: %v", err)
	}
	if len(b.Items) != resp.Baseline.New {
		t.Errorf("the saved baseline has %d item(s), the response reported %d new", len(b.Items), resp.Baseline.New)
	}
}

// --- item 1: the defect's own regression, across the two reachable checks ------
//
// scoring.MissingWeights and ScopeBlock.Validate() guard states the rest of
// this codebase already proves unreachable through a legitimate scan (every
// measured dimension has a weight by construction — scoring_test.go; every
// ScopeBlock this codebase builds satisfies Validate() by construction —
// TestScopeBlock_Validate_NoteInvariant in changedfiles_test.go). They are
// covered here STRUCTURALLY rather than behaviourally: buildScanAll no longer
// calls Save anywhere in its body (diffBaseline only computes), so ANY error
// it returns — for ANY reason, including a future wiring bug that trips one of
// those two guards — skips the write by construction, the same way the
// stillOver path above is skipped. This test locks that structural fact: an
// error from buildScanAll must never be followed by a write.
func TestScanAllWriteGate_BuildScanAllErrorNeverWrites(t *testing.T) {
	root := t.TempDir()
	// language: python (unregistered — no resolvable security provider), no
	// database section at all: neither a security provider resolves nor the
	// DB dimension runs, so buildScanAll's nothing-measurable guard (D5) must
	// error. "go" no longer proves this fixture (roadmap P4-1 exposed it —
	// docs/specs/declared-partial-language-exposure.md); the guard's own
	// behaviour is unchanged, only the language that demonstrates it moved.
	baselinePath := filepath.Join(root, baseline.Name)

	if _, err := handleScanAllBudgeted(ScanAllRequest{Root: root, Language: "python"},
		scope.Full(), baselinePath, ResponseBudgetBytes); err == nil {
		t.Fatal("expected the nothing-measurable guard to error")
	}
	if _, statErr := os.Stat(baselinePath); !os.IsNotExist(statErr) {
		t.Errorf("an error from buildScanAll must never be followed by a baseline write, but %s exists", baseline.Name)
	}
}
