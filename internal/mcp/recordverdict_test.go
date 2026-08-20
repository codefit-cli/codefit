package mcp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// realSurfaceItem returns one real, freshly-enumerated surface item from the
// fixture project via the actual security sensor — never a hand-built
// findings.SurfaceItem (CLAUDE.md: a hand-built fixture locks a reality
// production cannot produce). Every recordverdict test anchors its verdicts to
// this, so D5's re-validation is exercised against the real pipeline.
func realSurfaceItem(t *testing.T, root string) findings.SurfaceItem {
	t.Helper()
	resp, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("scan-security: %v", err)
	}
	if len(resp.Surface) == 0 {
		t.Fatal("fixture must produce at least one surface item")
	}
	return resp.Surface[0]
}

// A verdict anchored to a real, currently-existing surface item persists: it
// lands in the baseline with by:"agent", using the FRESH item's content-hash
// fingerprint (never the line-based surface id), and D1 holds through the MCP
// layer — recording it never sets Ack.
func TestRecordVerdict_ValidPersists(t *testing.T) {
	root := copyFixture(t)
	item := realSurfaceItem(t, root)

	resp, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{
		Root: root, Language: "typescript",
		Verdicts: []surface.Confirmation{
			{SurfaceID: item.ID, Category: item.Category, File: item.File, Line: item.Line,
				Verdict: surface.VerdictVulnerable, Reasoning: "no ownership check before returning the record", Confidence: 0.8},
		},
	})
	if err != nil {
		t.Fatalf("record-verdict: %v", err)
	}
	if len(resp.Persisted) != 1 || len(resp.Refused) != 0 {
		t.Fatalf("want 1 persisted, 0 refused, got %+v", resp)
	}
	if resp.Persisted[0].Fingerprint != item.Fingerprint {
		t.Errorf("persisted fingerprint must be the fresh item's content-hash fingerprint, got %q want %q",
			resp.Persisted[0].Fingerprint, item.Fingerprint)
	}

	b := loadBaseline(t, root)
	found := false
	for _, it := range b.Items {
		if it.FP != item.Fingerprint {
			continue
		}
		found = true
		if len(it.AgentVerdicts) != 1 {
			t.Fatalf("want 1 recorded verdict, got %d", len(it.AgentVerdicts))
		}
		if it.AgentVerdicts[0].By != baseline.ActorAgent {
			t.Errorf("a recorded verdict must be by:agent, got %q", it.AgentVerdicts[0].By)
		}
		if it.Ack != nil {
			t.Errorf("D1: recording a verdict must NEVER silence the item, got Ack=%+v", it.Ack)
		}
	}
	if !found {
		t.Fatalf("recorded fingerprint not found in the baseline")
	}
}

// A submitted surface_id that does not recompute from (file, line, category) is
// refused on the cheap pre-check, before any re-run.
func TestRecordVerdict_RefusesSurfaceIDMismatch(t *testing.T) {
	root := copyFixture(t)
	item := realSurfaceItem(t, root)

	resp, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{
		Root: root, Language: "typescript",
		Verdicts: []surface.Confirmation{
			{SurfaceID: "not-the-real-id", Category: item.Category, File: item.File, Line: item.Line, Verdict: surface.VerdictVulnerable, Confidence: 0.8},
		},
	})
	if err != nil {
		t.Fatalf("record-verdict: %v", err)
	}
	if len(resp.Persisted) != 0 || len(resp.Refused) != 1 {
		t.Fatalf("want 0 persisted, 1 refused, got %+v", resp)
	}
	if resp.Refused[0].Reason != mcp.ReasonSurfaceIDMismatch {
		t.Errorf("want reason %q, got %q", mcp.ReasonSurfaceIDMismatch, resp.Refused[0].Reason)
	}
}

// D5 — a verdict anchored to coordinates that are internally self-consistent
// (the id DOES recompute) but no longer correspond to any item on a fresh
// re-analysis (moved or fixed) is refused, never persisted as stale.
func TestRecordVerdict_RefusesMovedOrFixedItem(t *testing.T) {
	root := copyFixture(t)
	item := realSurfaceItem(t, root)

	fakeLine := item.Line + 9999
	resp, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{
		Root: root, Language: "typescript",
		Verdicts: []surface.Confirmation{
			{SurfaceID: surface.StableID(item.File, fakeLine, item.Category), Category: item.Category, File: item.File, Line: fakeLine, Verdict: surface.VerdictVulnerable, Confidence: 0.8},
		},
	})
	if err != nil {
		t.Fatalf("record-verdict: %v", err)
	}
	if len(resp.Persisted) != 0 || len(resp.Refused) != 1 {
		t.Fatalf("want 0 persisted, 1 refused, got %+v", resp)
	}
	if resp.Refused[0].Reason != mcp.ReasonNoSurfaceItemAtAnchor {
		t.Errorf("want reason %q, got %q", mcp.ReasonNoSurfaceItemAtAnchor, resp.Refused[0].Reason)
	}
}

// A verdict value outside the three surface.Verdict values is refused.
func TestRecordVerdict_RefusesUnknownVerdict(t *testing.T) {
	root := copyFixture(t)
	item := realSurfaceItem(t, root)

	resp, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{
		Root: root, Language: "typescript",
		Verdicts: []surface.Confirmation{
			{SurfaceID: item.ID, Category: item.Category, File: item.File, Line: item.Line, Verdict: surface.Verdict("maybe"), Confidence: 0.5},
		},
	})
	if err != nil {
		t.Fatalf("record-verdict: %v", err)
	}
	if len(resp.Persisted) != 0 || len(resp.Refused) != 1 {
		t.Fatalf("want 0 persisted, 1 refused, got %+v", resp)
	}
	if resp.Refused[0].Reason != mcp.ReasonUnknownVerdict {
		t.Errorf("want reason %q, got %q", mcp.ReasonUnknownVerdict, resp.Refused[0].Reason)
	}
}

// An empty batch returns early WITHOUT running any analysis: scope.Of(nil)
// resolves to a full scan, so falling through would silently full-scan the
// project for nothing. Asserted behaviorally, not just by response shape: no
// baseline file gets created where none existed.
func TestRecordVerdict_EmptyBatchReturnsEarlyNoFullScan(t *testing.T) {
	root := copyFixture(t) // no baseline yet
	resp, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("record-verdict: %v", err)
	}
	if len(resp.Persisted) != 0 || len(resp.Refused) != 0 {
		t.Errorf("an empty batch must persist and refuse nothing, got %+v", resp)
	}
	if _, err := os.Stat(filepath.Join(root, baseline.Name)); !os.IsNotExist(err) {
		t.Errorf("an empty batch must never trigger a full scan/save — the baseline file must stay absent, got err=%v", err)
	}
}

// A batch is per-entry, not all-or-nothing: the valid entry persists and the
// stale one is refused, both explicit in the same response.
func TestRecordVerdict_PartialBatchOneValidOneRefused(t *testing.T) {
	root := copyFixture(t)
	item := realSurfaceItem(t, root)

	resp, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{
		Root: root, Language: "typescript",
		Verdicts: []surface.Confirmation{
			{SurfaceID: item.ID, Category: item.Category, File: item.File, Line: item.Line, Verdict: surface.VerdictVulnerable, Confidence: 0.8},
			{SurfaceID: "stale-id", Category: item.Category, File: item.File, Line: item.Line, Verdict: surface.VerdictVulnerable, Confidence: 0.8},
		},
	})
	if err != nil {
		t.Fatalf("record-verdict: %v", err)
	}
	if len(resp.Persisted) != 1 || len(resp.Refused) != 1 {
		t.Fatalf("a partial batch must report both explicit in one response, got %+v", resp)
	}
}

// A whole-run failure (no provider for the language) returns an error, never a
// silent empty persist.
func TestRecordVerdict_WholeRunFailureReturnsError(t *testing.T) {
	root := copyFixture(t)
	_, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{
		Root: root, Language: "not-a-real-language",
		Verdicts: []surface.Confirmation{
			{SurfaceID: "x", Category: "idor", File: "app/users/route.ts", Line: 4, Verdict: surface.VerdictVulnerable, Confidence: 0.8},
		},
	})
	if err == nil {
		t.Fatal("an unsupported language must error, never a silent empty persist")
	}
}
