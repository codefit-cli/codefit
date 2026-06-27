package baseline_test

import (
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
)

// surf and affirm build observed items: a surface question and a deterministic
// affirmation, respectively.
func surf(fp, cat, file string) baseline.Observed {
	return baseline.Observed{FP: fp, Category: cat, File: file, Snippet: cat + " " + file, Affirms: false}
}
func affirm(fp, file string) baseline.Observed {
	return baseline.Observed{FP: fp, Category: "security", File: file, Snippet: "Possible hardcoded API key", Affirms: true}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	b, err := baseline.Load(filepath.Join(t.TempDir(), ".codefit-baseline"))
	if err != nil {
		t.Fatalf("Load on missing file must not error: %v", err)
	}
	if b == nil || len(b.Items) != 0 {
		t.Errorf("missing baseline must load as empty, got %+v", b)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codefit-baseline")
	b := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "aaa", Category: "overfetch", File: "a.ts", Snippet: "prisma.user.findMany()"},
		{FP: "bbb", Category: "authz", File: "b.ts", Snippet: "GET()", Ack: &baseline.Ack{Reason: "public", At: "2026-06-26", By: "human"}},
	}}
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := baseline.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[1].Ack == nil || got.Items[1].Ack.Reason != "public" {
		t.Errorf("round-trip lost data: %+v", got)
	}
}

// First scan: no previous baseline → everything is new and shown.
func TestDiffFirstScanAllNew(t *testing.T) {
	prev := &baseline.Baseline{Version: "1"}
	res := baseline.Diff(prev, []baseline.Observed{surf("a", "overfetch", "a.ts"), surf("b", "authz", "b.ts")})
	if res.Counts.New != 2 || res.Counts.Known != 0 {
		t.Errorf("first scan must be all new, got %+v", res.Counts)
	}
	if !res.Shown["a"] || !res.Shown["b"] {
		t.Errorf("new items must be shown")
	}
	if len(res.Next.Items) != 2 {
		t.Errorf("next baseline must record both items, got %d", len(res.Next.Items))
	}
}

// Second scan, no change: surface items are known → SILENCED (the goal).
func TestDiffKnownSurfaceSilenced(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{{FP: "a", Category: "overfetch", File: "a.ts"}}}
	res := baseline.Diff(prev, []baseline.Observed{surf("a", "overfetch", "a.ts")})
	if res.Counts.Known != 1 || res.Counts.New != 0 {
		t.Errorf("unchanged surface must be known, got %+v", res.Counts)
	}
	if res.Shown["a"] {
		t.Errorf("a known surface item must be silenced")
	}
}

// SAFETY: a deterministic affirmation that is KNOWN (seen before) but NOT
// acknowledged must STILL be shown on every scan — never auto-silenced.
func TestDiffKnownDeterministicStillShown(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{{FP: "sec", Category: "security", File: "c.ts"}}}
	res := baseline.Diff(prev, []baseline.Observed{affirm("sec", "c.ts")})
	if !res.Shown["sec"] {
		t.Errorf("a known, unaccepted deterministic affirmation must still be shown")
	}
}

// A deterministic affirmation that was ACKNOWLEDGED (with reason) is silenced.
func TestDiffAcknowledgedDeterministicSilenced(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "sec", Category: "security", File: "c.ts", Ack: &baseline.Ack{Reason: "test placeholder", At: "2026-06-26", By: "human"}},
	}}
	res := baseline.Diff(prev, []baseline.Observed{affirm("sec", "c.ts")})
	if res.Shown["sec"] {
		t.Errorf("an acknowledged deterministic finding must be silenced")
	}
	if res.Counts.Acknowledged != 1 {
		t.Errorf("acknowledged count = %d, want 1", res.Counts.Acknowledged)
	}
}

// An item in the baseline that no longer appears is GONE (prune candidate) and
// lingers in the next baseline until pruned.
func TestDiffGoneLingers(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{{FP: "old", Category: "idor", File: "x.ts"}}}
	res := baseline.Diff(prev, nil)
	if res.Counts.Gone != 1 || len(res.Gone) != 1 || res.Gone[0].FP != "old" {
		t.Errorf("removed item must be gone, got %+v", res.Counts)
	}
	if len(res.Next.Items) != 1 {
		t.Errorf("gone item must linger until pruned, got %d items", len(res.Next.Items))
	}
}

// A changed snippet (same file+category, different fp) is reported as changed and
// the old fp is replaced (not left as an orphan).
func TestDiffChangedReplacesOld(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{{FP: "old", Category: "overfetch", File: "a.ts"}}}
	res := baseline.Diff(prev, []baseline.Observed{surf("new", "overfetch", "a.ts")})
	if res.Counts.Changed != 1 || res.Counts.New != 0 || res.Counts.Gone != 0 {
		t.Errorf("an edited item must be 'changed', got %+v", res.Counts)
	}
	if !res.Shown["new"] {
		t.Errorf("changed items must be shown")
	}
	fps := map[string]bool{}
	for _, it := range res.Next.Items {
		fps[it.FP] = true
	}
	if fps["old"] || !fps["new"] {
		t.Errorf("changed must replace old fp with new, got %+v", fps)
	}
}

func TestAcceptRequiresReasonAndMarksHuman(t *testing.T) {
	b := &baseline.Baseline{Version: "1", Items: []baseline.Item{{FP: "a", Category: "overfetch", File: "a.ts"}}}
	if _, err := b.Accept([]string{"a"}, "", "2026-06-26"); err == nil {
		t.Errorf("accept with empty reason must error")
	}
	accepted, err := b.Accept([]string{"a"}, "false positive", "2026-06-26")
	if err != nil || len(accepted) != 1 {
		t.Fatalf("accept failed: %v", err)
	}
	it := b.Items[0]
	if it.Ack == nil || it.Ack.Reason != "false positive" || it.Ack.By != "human" || it.Ack.At != "2026-06-26" {
		t.Errorf("accept must record reason + date + by:human, got %+v", it.Ack)
	}
}

func TestAcceptRejectsWhitespaceReason(t *testing.T) {
	b := &baseline.Baseline{Version: "1", Items: []baseline.Item{{FP: "a"}}}
	if _, err := b.Accept([]string{"a"}, "   ", "2026-06-26"); err == nil {
		t.Errorf("a whitespace-only reason must be rejected")
	}
}

// SAFETY: when a deterministic item's content changes, the old fp is replaced and
// the new one is shown WITHOUT inheriting the acknowledgment — the human must
// re-accept the changed affirmation.
func TestDiffChangedDropsAck(t *testing.T) {
	prev := &baseline.Baseline{Version: "1", Items: []baseline.Item{
		{FP: "old", Category: "security", File: "c.ts", Ack: &baseline.Ack{Reason: "ok", At: "2026-06-26", By: "human"}},
	}}
	res := baseline.Diff(prev, []baseline.Observed{{FP: "new", Category: "security", File: "c.ts", Affirms: true}})
	if res.State["new"] != baseline.StateChanged || !res.Shown["new"] {
		t.Errorf("changed affirmation must be shown, got state=%q shown=%v", res.State["new"], res.Shown["new"])
	}
	for _, it := range res.Next.Items {
		if it.FP == "new" && it.Ack != nil {
			t.Errorf("a changed item must NOT inherit the old acknowledgment")
		}
	}
}

func TestAcceptUnknownFingerprintErrors(t *testing.T) {
	b := &baseline.Baseline{Version: "1", Items: []baseline.Item{{FP: "a"}}}
	if _, err := b.Accept([]string{"zzz"}, "x", "2026-06-26"); err == nil {
		t.Errorf("accept of an unknown fingerprint must error")
	}
}

func TestPruneRemovesGivenFPs(t *testing.T) {
	b := &baseline.Baseline{Version: "1", Items: []baseline.Item{{FP: "a"}, {FP: "b"}, {FP: "c"}}}
	pruned := b.Prune([]string{"a", "c"})
	if len(pruned) != 2 || len(b.Items) != 1 || b.Items[0].FP != "b" {
		t.Errorf("prune must remove given fps, got items %+v pruned %+v", b.Items, pruned)
	}
}
