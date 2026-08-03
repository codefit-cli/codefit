package baseline_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/scope"
)

// goldenScenario is one Diff call exercising every branch at once: a known item,
// an acknowledged one, a new one, a changed one (paired at the same file+
// category), an in-scope gone one, an affirmation, and a foreign item whose
// sensor did not run. It is the input behind testdata/diff_full_golden.json.
func goldenScenario() (*baseline.Baseline, []baseline.Observed, map[string]bool) {
	prev := &baseline.Baseline{
		Version: "1",
		Items: []baseline.Item{
			{FP: "known1", Category: "overfetch", File: "src/a.ts", Snippet: "select *"},
			{FP: "acked1", Category: "idor", File: "src/b.ts", Snippet: "findUnique", Ack: &baseline.Ack{Reason: "owner check upstream", At: "2026-01-02", By: "human"}},
			{FP: "goneOld", Category: "authz", File: "src/c.ts", Snippet: "handler"},
			{FP: "replacedOld", Category: "overfetch", File: "src/d.ts", Snippet: "old snippet"},
			{FP: "foreign", Category: "db-fk-no-index", File: "prisma/schema.prisma", Snippet: "userId"},
		},
		AuthzHelpers: []baseline.AuthzHelper{
			{Name: "requirePermission", Language: "typescript", Reason: "project helper", At: "2026-01-01", By: "human"},
		},
	}
	observed := []baseline.Observed{
		{FP: "known1", Category: "overfetch", File: "src/a.ts", Snippet: "select *"},
		{FP: "acked1", Category: "idor", File: "src/b.ts", Snippet: "findUnique"},
		{FP: "new1", Category: "security", File: "src/e.ts", Snippet: "Possible hardcoded API key", Affirms: true},
		{FP: "replacedNew", Category: "overfetch", File: "src/d.ts", Snippet: "new snippet"},
	}
	scanned := map[string]bool{"security": true, "idor": true, "authz": true, "overfetch": true, "nplus1": true}
	return prev, observed, scanned
}

// Test contract item 1: a FULL scope through Diff produces a delta
// byte-identical to the one the pre-scope implementation produced. The golden
// was captured from that implementation, so it is a floor, not a snapshot of the
// new code's own opinion — which is why nothing here can rewrite it.
func TestDiff_FullScope_ByteIdenticalToPreScopeDelta(t *testing.T) {
	prev, observed, scanned := goldenScenario()
	got, err := json.MarshalIndent(baseline.Diff(prev, observed, scanned, scope.Full()), "", "  ")
	if err != nil {
		t.Fatalf("marshalling diff: %v", err)
	}
	path := filepath.Join("testdata", "diff_full_golden.json")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s: %v", path, err)
	}
	if string(got) != string(normalizeEOL(want)) {
		t.Errorf("full-scope delta drifted from the pre-scope golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// normalizeEOL makes the comparison independent of the checkout's line endings —
// a golden file is text, and this repository is checked out on both OSs.
func normalizeEOL(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' && i+1 < len(b) && b[i+1] == '\n' {
			continue
		}
		out = append(out, b[i])
	}
	return out
}
