package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

func mentions(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(strings.ToLower(s), strings.ToLower(substr)) {
			return true
		}
	}
	return false
}

func TestCoverageManifest(t *testing.T) {
	m := typescript.New().CoverageManifest()
	if m.Language != "typescript" {
		t.Errorf("Language = %q", m.Language)
	}
	if len(m.Deterministic) == 0 || len(m.Reasoning) == 0 || len(m.NotCovered) == 0 {
		t.Fatalf("manifest lists must be populated: %+v", m)
	}

	// The two split categories (ADR 0004) must appear on BOTH sides — the local
	// case under Deterministic, the follow-the-data case under Reasoning — so the
	// division of labor is explicit, not a gap.
	for _, cat := range []string{"sql", "dangerouslySetInnerHTML"} {
		if !mentions(m.Deterministic, cat) {
			t.Errorf("%q split category missing from Deterministic", cat)
		}
		if !mentions(m.Reasoning, cat) {
			t.Errorf("%q split category missing from Reasoning", cat)
		}
	}

	// Prose, not "partial": each split entry must read as a human sentence, not a
	// one-word status. Guard against a lazy "SQL injection: partial".
	for _, s := range append(append([]string{}, m.Deterministic...), m.Reasoning...) {
		if strings.Contains(strings.ToLower(s), "partial") {
			t.Errorf("manifest uses the word 'partial' instead of explaining the split: %q", s)
		}
	}
	// A split entry should be a real sentence (long enough to explain the limit).
	foundProse := false
	for _, s := range m.Reasoning {
		if strings.Contains(strings.ToLower(s), "sql") && len(s) > 60 {
			foundProse = true
		}
	}
	if !foundProse {
		t.Error("the SQL reasoning entry should explain, in prose, what is mapped as surface and why")
	}
}
