package mcp_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// projectWithExtraRoutes copies the real fixture and adds n MORE real route
// files, each in the shape the fixture already proves produces surface. Nothing
// here is a hand-built Item or Baseline: the surface comes out of the real
// parser and the real sensor, so the identities the test reasons about are the
// ones production computes (CLAUDE.md — a hand-built fixture locks a reality
// production cannot produce).
func projectWithExtraRoutes(t *testing.T, n int) string {
	t.Helper()
	root := copyFixture(t)
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, "app", fmt.Sprintf("gen%02d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("import { prisma } from \"@/lib/prisma\";\n\n"+
			"// Local Prisma find with NO select and no authorization check.\n"+
			"export async function GET() {\n"+
			"  return Response.json(await prisma.model%02d.findMany());\n}\n", i)
		if err := os.WriteFile(filepath.Join(dir, "route.ts"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// recordVerdicts drives the REAL record-verdict handler over the REAL surface of
// root, returning the fingerprints it persisted.
func recordVerdicts(t *testing.T, root string, v surface.Verdict, reasoning string, limit int) []string {
	t.Helper()
	sec, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("scan-security: %v", err)
	}
	if len(sec.Surface) == 0 {
		t.Fatal("fixture produced no surface — the probe itself is broken")
	}
	var batch []surface.Confirmation
	for i, it := range sec.Surface {
		if limit > 0 && i >= limit {
			break
		}
		batch = append(batch, surface.Confirmation{
			SurfaceID: it.ID, Category: it.Category, File: it.File, Line: it.Line,
			Verdict: v, Reasoning: reasoning, Confidence: 0.8,
		})
	}
	resp, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{
		Root: root, Language: "typescript", Verdicts: batch,
	})
	if err != nil {
		t.Fatalf("record-verdict: %v", err)
	}
	if len(resp.Persisted) == 0 {
		t.Fatalf("nothing persisted, refused=%+v", resp.Refused)
	}
	fps := make([]string, 0, len(resp.Persisted))
	for _, p := range resp.Persisted {
		fps = append(fps, p.Fingerprint)
	}
	return fps
}

// R4 — scan-all must report that an agent reasoned an item, and report it
// WITHOUT the report itself silencing anything.
//
// This is the read-back half of the closing protocol, and it is load-bearing for
// a reason the design could not have known when it was written: a reasoned
// SURFACE item goes `known` on the next scan and stops appearing in the endpoint
// buckets — the baseline's own pre-existing safeguard, measured on two real
// projects. Without this delta the response gives an agent no way to learn that
// anything was ever reasoned. Recording still does not silence; what silences is
// a rule that applied before agent verdicts existed.
func TestScanAll_DeltaReportsWhatAnAgentReasoned(t *testing.T) {
	root := copyFixture(t)
	if _, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatalf("first scan-all: %v", err)
	}
	fps := recordVerdicts(t, root, surface.VerdictNotVulnerable, "ownership is enforced by the caller", 1)

	after, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("scan-all after recording: %v", err)
	}
	d := after.Baseline

	if d.ReasonedByAgent != 1 {
		t.Errorf("ReasonedByAgent = %d, want 1 — the response must say an agent reasoned this project", d.ReasonedByAgent)
	}
	if len(d.ReasonedItems) != 1 {
		t.Fatalf("ReasonedItems = %d items, want 1: %+v", len(d.ReasonedItems), d.ReasonedItems)
	}
	got := d.ReasonedItems[0]
	if got.Fingerprint != fps[0] {
		t.Errorf("ReasonedItems[0].Fingerprint = %q, want the persisted fp %q", got.Fingerprint, fps[0])
	}
	if got.Verdict != string(surface.VerdictNotVulnerable) {
		t.Errorf("ReasonedItems[0].Verdict = %q, want %q", got.Verdict, surface.VerdictNotVulnerable)
	}
	if got.At == "" || got.File == "" || got.Category == "" {
		t.Errorf("a reasoned item must name when, where and what: %+v", got)
	}
	if got.Conflict {
		t.Errorf("one verdict cannot be a conflict: %+v", got)
	}

	// D1 through the reporting layer: reporting the verdict must not acknowledge it.
	if d.Acknowledged != 0 {
		t.Errorf("Acknowledged = %d, want 0 — reporting an agent verdict must never accept it", d.Acknowledged)
	}
	// And the delta must carry NO reasoning prose: that is what keeps it inside
	// the response budget, and it is a promise codefit-baseline-list depends on.
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "ownership is enforced by the caller") {
		t.Errorf("the delta must not carry reasoning prose — it is unbounded at 500 runes per verdict:\n%s", raw)
	}
}

// R5 — items whose verdicts DISAGREE get their own list. A disagreement is a
// question for a human; averaging it into new/changed/known/acknowledged would
// bury exactly the thing that needs attention.
func TestScanAll_DeltaNamesItemsWhoseVerdictsDisagree(t *testing.T) {
	root := copyFixture(t)
	if _, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatalf("first scan-all: %v", err)
	}
	a := recordVerdicts(t, root, surface.VerdictVulnerable, "agent one: no ownership check on the read path", 1)
	b := recordVerdicts(t, root, surface.VerdictNotVulnerable, "agent two: the middleware enforces scope", 1)
	if a[0] != b[0] {
		t.Fatalf("the probe must put both verdicts on the SAME item, got %q and %q", a[0], b[0])
	}

	after, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	d := after.Baseline

	if d.InConflictCount != 1 {
		t.Errorf("InConflictCount = %d, want 1", d.InConflictCount)
	}
	if len(d.InConflict) != 1 || d.InConflict[0].Fingerprint != a[0] {
		t.Fatalf("InConflict must name the disagreed item, got %+v", d.InConflict)
	}
	if d.InConflict[0].File == "" || d.InConflict[0].Category == "" {
		t.Errorf("a conflict must be locatable by a human: %+v", d.InConflict[0])
	}
	// The same fact must be readable from the reasoned list alone, without
	// cross-referencing.
	if len(d.ReasonedItems) != 1 || !d.ReasonedItems[0].Conflict {
		t.Errorf("ReasonedItems must carry Conflict for the disagreed item: %+v", d.ReasonedItems)
	}
	if d.Acknowledged != 0 {
		t.Errorf("Acknowledged = %d, want 0 — a conflict is never a decision", d.Acknowledged)
	}
}

// The two counts are ALWAYS on the wire, including at zero. A measured zero and
// an absent key are different claims, and an agent must be able to tell "codefit
// looked and found no reasoning" from "this codefit is too old to know".
// Asserted on the MARSHALLED bytes, not on Go struct fields — omitempty is
// invisible from the struct side.
func TestScanAll_ReasonedCountsArePresentOnTheWireEvenAtZero(t *testing.T) {
	root := copyFixture(t)
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Baseline.ReasonedByAgent != 0 {
		t.Fatalf("probe broken: a fresh project must have 0 reasoned items, got %d", resp.Baseline.ReasonedByAgent)
	}
	raw, err := json.Marshal(resp.Baseline)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	// Positive control: the probe can see a key that is definitely there.
	if _, ok := wire["new"]; !ok {
		t.Fatal("probe broken: the delta has no \"new\" key at all")
	}
	for _, k := range []string{"reasoned_by_agent", "in_conflict_count"} {
		if _, ok := wire[k]; !ok {
			t.Errorf("%q is absent at zero. An absent key reads as \"this build cannot measure it\"; "+
				"a zero reads as \"codefit looked and found none\". They are different claims.\n%s", k, raw)
		}
	}
	// The lists themselves are omitempty — nothing reasoned, nothing to name.
	for _, k := range []string{"reasoned_items", "in_conflict"} {
		if _, ok := wire[k]; ok {
			t.Errorf("%q should be omitted when empty, matching gone_candidates in the same struct", k)
		}
	}
}

// The rendered list is capped BY CONSTRUCTION, and what it cut is counted at the
// cut — never derived from len() of the rendered list.
//
// The cap is not taste. fitToBudget can only withhold endpoints from the three
// buckets; it cannot see this block at all. An unbounded list here could push a
// response past its budget with nothing the budget step is able to withhold —
// the exact failure P0-4's remaining half describes.
func TestScanAll_ReasonedListIsCappedAndDeclaresWhatItCut(t *testing.T) {
	root := projectWithExtraRoutes(t, 20)
	if _, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatalf("first scan-all: %v", err)
	}
	fps := recordVerdicts(t, root, surface.VerdictVulnerable, "probe: reasoned in bulk", 0)
	if len(fps) <= mcp.MaxRenderedReasoned {
		t.Fatalf("probe broken: needs MORE than the cap (%d) reasoned items, got %d",
			mcp.MaxRenderedReasoned, len(fps))
	}

	after, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	d := after.Baseline

	if d.ReasonedByAgent != len(fps) {
		t.Errorf("ReasonedByAgent = %d, want the COMPLETE %d — the count is never cut, only the list is",
			d.ReasonedByAgent, len(fps))
	}
	if len(d.ReasonedItems) != mcp.MaxRenderedReasoned {
		t.Errorf("rendered %d items, want the cap %d", len(d.ReasonedItems), mcp.MaxRenderedReasoned)
	}
	if want := len(fps) - mcp.MaxRenderedReasoned; d.ReasonedWithheld != want {
		t.Errorf("ReasonedWithheld = %d, want %d", d.ReasonedWithheld, want)
	}
	if d.AgentReasoningNote == "" {
		t.Error("a cut list must say it was cut, and why")
	}
	if !strings.Contains(d.AgentReasoningNote, string(mcp.ToolBaselineList)) {
		t.Errorf("the note must name where the full list is: %q", d.AgentReasoningNote)
	}
}

// The prose the delta deliberately omits is reachable through the targeted read
// that already exists. Without this the light delta would not be a trade-off, it
// would be a loss.
func TestBaselineList_CarriesTheReasoningTheDeltaOmits(t *testing.T) {
	root := copyFixture(t)
	long := strings.Repeat("the caller enforces ownership before this handler runs. ", 12)
	recordVerdicts(t, root, surface.VerdictNotVulnerable, long, 1)

	list, err := mcp.HandleBaselineList(mcp.BaselineListRequest{Root: root})
	if err != nil {
		t.Fatalf("baseline-list: %v", err)
	}
	var withVerdict int
	for _, e := range list.Items {
		if len(e.AgentVerdicts) == 0 {
			continue
		}
		withVerdict++
		v := e.AgentVerdicts[0]
		if v.Verdict != string(surface.VerdictNotVulnerable) {
			t.Errorf("Entry verdict = %q, want %q", v.Verdict, surface.VerdictNotVulnerable)
		}
		if v.By != "agent" {
			t.Errorf("Entry verdict By = %q, want \"agent\"", v.By)
		}
		if v.Reasoning == "" {
			t.Error("baseline-list is where the prose lives; it must not be empty here too")
		}
		if len([]rune(v.Reasoning)) > 201 {
			t.Errorf("reasoning must be truncated by the existing list-time idiom, got %d runes", len([]rune(v.Reasoning)))
		}
	}
	if withVerdict != 1 {
		t.Errorf("entries carrying a verdict = %d, want 1", withVerdict)
	}
}
