package baseline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// vulnerable/notVulnerable build minimal agent verdicts for the two opposing
// directions the conflict tests need. By is intentionally left unset here —
// RecordVerdict must stamp it itself (handler-assigned, never caller-supplied).
func vulnerable(reasoning string) baseline.AgentVerdict {
	return baseline.AgentVerdict{Verdict: surface.VerdictVulnerable, Reasoning: reasoning, Confidence: 0.8, At: "2026-08-20"}
}
func notVulnerable(reasoning string) baseline.AgentVerdict {
	return baseline.AgentVerdict{Verdict: surface.VerdictNotVulnerable, Reasoning: reasoning, Confidence: 0.7, At: "2026-08-20"}
}

// RecordVerdict appends to a NEW fp without ever touching Ack (D1: an agent
// verdict never silences).
func TestRecordVerdict_CreatesItemNeverTouchesAck(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}
	it := b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", vulnerable("reads params.id with no ownership check"))
	if it == nil {
		t.Fatal("RecordVerdict must return the item")
	}
	if it.Ack != nil {
		t.Errorf("RecordVerdict must never touch Ack, got %+v", it.Ack)
	}
	if len(it.AgentVerdicts) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(it.AgentVerdicts))
	}
	if it.AgentVerdicts[0].By != baseline.ActorAgent {
		t.Errorf("By must be stamped ActorAgent, got %q", it.AgentVerdicts[0].By)
	}
	if len(b.Items) != 1 || b.Items[0].FP != "fp1" {
		t.Fatalf("item must be created in the baseline, got %+v", b.Items)
	}
}

// RecordVerdict on an EXISTING fp appends to that item rather than creating a
// second one, and still never touches Ack.
func TestRecordVerdict_AppendsToExistingItem(t *testing.T) {
	b := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "fp1", Category: "idor", File: "app/x/route.ts", Snippet: "GET /x"},
	}}
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", vulnerable("first pass"))
	if len(b.Items) != 1 {
		t.Fatalf("must not create a duplicate item, got %d", len(b.Items))
	}
	if len(b.Items[0].AgentVerdicts) != 1 {
		t.Fatalf("want 1 verdict on the existing item, got %d", len(b.Items[0].AgentVerdicts))
	}
	// The comment above promised this and the body did not check it. D1 is the
	// autonomy principle in one behaviour, and the APPEND path is the common one
	// (the item already exists from a prior scan), so it needs the assertion more
	// than the create path does, not less.
	if b.Items[0].Ack != nil {
		t.Errorf("appending a verdict must never touch Ack, got %+v", b.Items[0].Ack)
	}
	// The dangerous direction (D4): a not_vulnerable verdict REMOVES alarm, so it
	// is the one that must never silence on its own. Recorded on an EXISTING item,
	// which is where a real re-scan puts it.
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", notVulnerable("agent says the guard is upstream"))
	if b.Items[0].Ack != nil {
		t.Errorf("a not_vulnerable verdict must never silence: only a human acknowledges, got %+v", b.Items[0].Ack)
	}
}

// A single verdict never conflicts.
func TestInConflict_SingleVerdictFalse(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", vulnerable("reasoning"))
	if b.Items[0].InConflict() {
		t.Errorf("a single verdict must never conflict")
	}
}

// D2 — two opposing verdicts on the same fp are BOTH kept, and InConflict is
// true. Nothing overwrites, nothing is dropped.
func TestInConflict_OpposingVerdictsBothKeptAndFlagged(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", vulnerable("agent A: reads params.id with no ownership check"))
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", notVulnerable("agent B: ownership enforced upstream by middleware"))

	if len(b.Items) != 1 {
		t.Fatalf("both verdicts must land on the SAME item, got %d items", len(b.Items))
	}
	if len(b.Items[0].AgentVerdicts) != 2 {
		t.Fatalf("both verdicts must be kept, got %d", len(b.Items[0].AgentVerdicts))
	}
	if !b.Items[0].InConflict() {
		t.Errorf("opposing verdicts must be flagged in conflict")
	}
}

// Two agreeing verdicts (both vulnerable) never conflict.
func TestInConflict_AgreeingVerdictsFalse(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", vulnerable("agent A"))
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", vulnerable("agent B"))
	if b.Items[0].InConflict() {
		t.Errorf("two agreeing verdicts must not conflict")
	}
}

// Reasoning is capped at 500 runes with a truncation marker, at STORAGE time —
// distinct from the existing 200-rune LIST-time cap (maxReasonLen).
func TestRecordVerdict_ReasoningCappedAt500Runes(t *testing.T) {
	long := strings.Repeat("x", 800)
	b := &baseline.Baseline{Version: "1"}
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", vulnerable(long))
	got := b.Items[0].AgentVerdicts[0].Reasoning
	r := []rune(got)
	if len(r) > 501 { // 500 + the truncation marker rune
		t.Fatalf("reasoning must be capped near 500 runes, got %d", len(r))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated reasoning must end with the truncation marker, got %q", got[len(got)-10:])
	}
}

// Reasoning under the cap is stored verbatim, no truncation marker added.
func TestRecordVerdict_ReasoningUnderCapUntouched(t *testing.T) {
	b := &baseline.Baseline{Version: "1"}
	b.RecordVerdict("fp1", "idor", "app/x/route.ts", "GET /x", vulnerable("short reasoning"))
	if got := b.Items[0].AgentVerdicts[0].Reasoning; got != "short reasoning" {
		t.Errorf("reasoning under the cap must be stored verbatim, got %q", got)
	}
}

// Diff carries AgentVerdicts forward on an UNCHANGED fp (via refresh) — a
// re-scan must not erase recorded reasoning about an item that has not changed.
func TestDiff_CarriesAgentVerdictsForwardOnUnchangedFP(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "a", Category: "overfetch", File: "a.ts", AgentVerdicts: []baseline.AgentVerdict{vulnerable("reasoned once")}},
	}}
	res := baseline.Diff(prev, []baseline.Observed{{FP: "a", Category: "overfetch", File: "a.ts", Affirms: false}}, secScope(), scope.Full())
	if len(res.Next.Items) != 1 || len(res.Next.Items[0].AgentVerdicts) != 1 {
		t.Fatalf("an unchanged fp must carry its agent verdicts forward, got %+v", res.Next.Items)
	}
}

// Diff DROPS AgentVerdicts on a CHANGED fp — the fingerprint identity changed
// (ADR 0009), so the old reasoning no longer applies to different content.
func TestDiff_DropsAgentVerdictsOnChangedFP(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "old", Category: "overfetch", File: "a.ts", AgentVerdicts: []baseline.AgentVerdict{vulnerable("reasoned about the old content")}},
	}}
	res := baseline.Diff(prev, []baseline.Observed{{FP: "new", Category: "overfetch", File: "a.ts", Affirms: false}}, secScope(), scope.Full())
	for _, it := range res.Next.Items {
		if it.FP == "new" && len(it.AgentVerdicts) != 0 {
			t.Errorf("a changed fp must NOT inherit the old fp's agent verdicts, got %+v", it.AgentVerdicts)
		}
	}
}

// D6-B: an unrecognized top-level YAML field survives Load -> Save (an older
// binary preserves what a NEWER one wrote, protecting the NEXT format
// addition — this does not fix the already-shipped v0.2.6-v0.2.9 case, which
// ADR 0081 documents as accepted per D6-A).
func TestLoadSave_PreservesUnknownTopLevelField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, baseline.Name)
	raw := "version: \"1\"\nitems: []\nfuture_field:\n  nested: yes\n  list: [1, 2, 3]\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := baseline.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data := string(got)
	if !strings.Contains(data, "future_field") || !strings.Contains(data, "nested") {
		t.Errorf("Load->Save must preserve an unknown top-level field, got:\n%s", data)
	}
}

// D6-B's trap: Diff builds Next from scratch, so the unknown field must be
// carried into Next explicitly or Load/Save alone are not enough.
func TestLoadDiffSave_PreservesUnknownTopLevelField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, baseline.Name)
	raw := "version: \"1\"\nitems: []\nfuture_field:\n  nested: yes\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := baseline.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	res := baseline.Diff(b, nil, secScope(), scope.Full())
	if err := res.Next.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data := string(got)
	if !strings.Contains(data, "future_field") {
		t.Errorf("Load->Diff->Save must still preserve an unknown top-level field, got:\n%s", data)
	}
}
