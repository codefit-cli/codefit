package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/mcp"
)

func scanAll(t *testing.T, root string) mcp.ScanAllResponse {
	t.Helper()
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("scan-all: %v", err)
	}
	return resp
}

func addFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadBaseline(t *testing.T, root string) *baseline.Baseline {
	t.Helper()
	b, err := baseline.Load(filepath.Join(root, baseline.Name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// surfaceFP returns the fingerprint of the first actionable surface concern; det
// returns the first deterministic (affirmed) concern's fingerprint.
func anyActionableFP(resp mcp.ScanAllResponse, affirmedOnly bool) string {
	for _, ep := range resp.Actionable {
		for _, c := range ep.Concerns {
			if c.Fingerprint == "" {
				continue
			}
			if affirmedOnly && c.Gap != "affirmed" {
				continue
			}
			if !affirmedOnly && c.Gap == "affirmed" {
				continue
			}
			return c.Fingerprint
		}
	}
	return ""
}

const secretFile = "lib/config.ts"
const secretSrc = "export const apiKey = \"sk-ABCDEF0123456789ABCDEF0123\";\n"

func TestBaselineFirstScanCreatesAllNew(t *testing.T) {
	root := copyFixture(t)
	resp := scanAll(t, root)
	if resp.Baseline.New == 0 {
		t.Errorf("first scan must report items as new, got %+v", resp.Baseline)
	}
	if resp.Baseline.Known != 0 {
		t.Errorf("first scan can have nothing known, got %d", resp.Baseline.Known)
	}
	if _, err := os.Stat(filepath.Join(root, baseline.Name)); err != nil {
		t.Errorf("first scan must create the baseline file: %v", err)
	}
}

// The core goal: a second identical scan silences known surface (0 new, actionable
// surface dropped from the view).
func TestBaselineSecondScanSilencesKnownSurface(t *testing.T) {
	root := copyFixture(t)
	scanAll(t, root)         // creates baseline
	resp := scanAll(t, root) // everything known now
	if resp.Baseline.New != 0 || resp.Baseline.Changed != 0 {
		t.Errorf("an unchanged second scan must have 0 new/changed, got %+v", resp.Baseline)
	}
	if resp.Baseline.Known == 0 {
		t.Errorf("second scan must count known items")
	}
	if len(resp.Actionable) != 0 {
		t.Errorf("known surface must be silenced from actionable, got %d endpoints", len(resp.Actionable))
	}
}

func TestBaselineNewItemAppears(t *testing.T) {
	root := copyFixture(t)
	scanAll(t, root)
	addFile(t, root, "app/orders/route.ts",
		"import { prisma } from \"@/lib/prisma\";\nexport async function GET() { return Response.json(await prisma.order.findMany()); }\n")
	resp := scanAll(t, root)
	if resp.Baseline.New == 0 {
		t.Errorf("a newly added endpoint must show as new, got %+v", resp.Baseline)
	}
	found := false
	for _, ep := range resp.Actionable {
		if filepath.Base(filepath.Dir(ep.File)) == "orders" {
			found = true
		}
	}
	if !found {
		t.Errorf("the new endpoint must appear in actionable")
	}
}

func TestBaselineAcceptSilencesSurface(t *testing.T) {
	root := copyFixture(t)
	first := scanAll(t, root)
	fp := anyActionableFP(first, false)
	if fp == "" {
		t.Fatal("expected at least one actionable surface concern with a fingerprint")
	}
	if _, err := mcp.HandleBaselineAccept(mcp.BaselineAcceptRequest{
		Root: root, Fingerprints: []string{fp}, Reason: "known false positive, internal endpoint",
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	resp := scanAll(t, root)
	for _, ep := range resp.Actionable {
		for _, c := range ep.Concerns {
			if c.Fingerprint == fp {
				t.Errorf("accepted item must no longer be actionable")
			}
		}
	}
	if resp.Baseline.Acknowledged == 0 {
		t.Errorf("accepted item must be counted as acknowledged")
	}
	// The reason + human marker are persisted.
	b := loadBaseline(t, root)
	ok := false
	for _, it := range b.Items {
		if it.FP == fp {
			if it.Ack == nil || it.Ack.Reason == "" || it.Ack.By != "human" || it.Ack.At == "" {
				t.Errorf("accepted item must record reason+date+by:human, got %+v", it.Ack)
			}
			ok = true
		}
	}
	if !ok {
		t.Errorf("accepted fingerprint not found in baseline")
	}
}

// SAFETY: a deterministic affirmation is NOT auto-silenced — it shows on every
// scan until explicitly accepted with a reason.
func TestBaselineDeterministicShownUntilAccepted(t *testing.T) {
	root := copyFixture(t)
	addFile(t, root, secretFile, secretSrc)

	scanAll(t, root) // 1st: the secret is new
	second := scanAll(t, root)
	fp := anyActionableFP(second, true)
	if fp == "" {
		t.Fatal("a known but unaccepted deterministic finding must still be actionable on re-scan")
	}
	if second.Baseline.AffirmationsShown == 0 {
		t.Errorf("affirmations_shown must count the re-shown deterministic finding")
	}

	if _, err := mcp.HandleBaselineAccept(mcp.BaselineAcceptRequest{
		Root: root, Fingerprints: []string{fp}, Reason: "test placeholder, not a real key",
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	third := scanAll(t, root)
	if anyActionableFP(third, true) == fp {
		t.Errorf("an accepted deterministic finding must be silenced")
	}

	// SECURITY: the committed baseline must never contain the raw secret.
	data, err := os.ReadFile(filepath.Join(root, baseline.Name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-ABCDEF0123456789ABCDEF0123") {
		t.Errorf("the committed baseline leaked the secret value")
	}
}

func TestBaselineGoneThenPrune(t *testing.T) {
	root := copyFixture(t)
	scanAll(t, root)
	if err := os.Remove(filepath.Join(root, "app/feed/route.ts")); err != nil {
		t.Fatal(err)
	}
	gone := scanAll(t, root)
	if gone.Baseline.Gone == 0 || len(gone.Baseline.GoneCandidates) == 0 {
		t.Fatalf("a removed endpoint must show as gone, got %+v", gone.Baseline)
	}
	if _, err := mcp.HandleBaselinePrune(mcp.BaselinePruneRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatalf("prune: %v", err)
	}
	after := scanAll(t, root)
	if after.Baseline.Gone != 0 {
		t.Errorf("prune must remove gone items, still gone: %d", after.Baseline.Gone)
	}
}

// Moving code (adding imports above) must NOT change item states — the fingerprint
// is content-based, not line-based.
func TestBaselineRobustToLineMoves(t *testing.T) {
	root := copyFixture(t)
	scanAll(t, root)
	// Prepend comment lines to shift every line down.
	p := filepath.Join(root, "app/users/route.ts")
	orig, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append([]byte("// added import\n// another line\n"), orig...), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := scanAll(t, root)
	if resp.Baseline.New != 0 || resp.Baseline.Changed != 0 {
		t.Errorf("line moves must not create new/changed items, got %+v", resp.Baseline)
	}
}
