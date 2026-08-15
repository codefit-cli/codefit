package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/coverage"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// proseOf reads an entry the way a caller who asked for its detail would: the
// detail when there is one, the claim when the claim already said everything.
func proseOf(entries []coverage.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, coverage.ProseOf(e))
	}
	return out
}

// entryIDs is the set of ids a bucket declares. Every control that asks "does
// the manifest answer for X" asks it here rather than by searching joined
// prose: since the per-rule split (ADR 0077) an answer IS an entry, and an id
// spelled inside some other entry's paragraph is not one.
func entryIDs(entries []coverage.Entry) map[string]bool {
	ids := make(map[string]bool, len(entries))
	for _, e := range entries {
		ids[e.ID] = true
	}
	return ids
}

// TestCoverageManifest_EveryEntryIsNamedAndClaimed: an entry with no id cannot
// be asked for, and one with no claim is counted, named, and says nothing.
func TestCoverageManifest_EveryEntryIsNamedAndClaimed(t *testing.T) {
	idx := typescript.New().CoverageManifest().Index()
	if len(idx) == 0 {
		t.Fatal("vacuum: nothing to check")
	}
	seen := map[string]bool{}
	for _, e := range idx {
		switch {
		case strings.TrimSpace(e.ID) == "":
			t.Errorf("an entry has no id: %q", e.Claim)
		case seen[e.ID]:
			t.Errorf("duplicate id %q", e.ID)
		}
		seen[e.ID] = true
		if strings.TrimSpace(e.Claim) == "" {
			t.Errorf("entry %q has an empty claim", e.ID)
		}
		if len(e.Claim) > coverage.MaxClaimBytes {
			t.Errorf("entry %q has a %d-byte claim, over the %d-byte cap — shorten the claim, never drop the entry",
				e.ID, len(e.Claim), coverage.MaxClaimBytes)
		}
	}
}

func TestCoverageManifest(t *testing.T) {
	m := typescript.New().CoverageManifest()
	if m.Language != "typescript" {
		t.Errorf("Language = %q", m.Language)
	}
	if len(proseOf(m.Deterministic)) == 0 || len(proseOf(m.Reasoning)) == 0 || len(proseOf(m.NotCovered)) == 0 {
		t.Fatalf("manifest lists must be populated: %+v", m)
	}

	// The two split categories (ADR 0004) must appear on BOTH sides — the local
	// case under Deterministic, the follow-the-data case under Reasoning — so the
	// division of labor is explicit, not a gap.
	//
	// Asked by ID, not by searching the joined prose for the word: each half of a
	// split is its own entry, so an agent can name both and compare them. The old
	// form was satisfied by ANY entry that happened to say "sql", including one
	// describing something else entirely.
	det := entryIDs(m.Deterministic)
	rea := entryIDs(m.Reasoning)
	for _, split := range []struct{ deterministic, reasoning string }{
		{"ts.sql-injection-inline", "ts.sql-injection-via-intermediate"},
		{"ts.xss-inline-inner-html", "ts.xss-inner-html-from-variable"},
	} {
		if !det[split.deterministic] {
			t.Errorf("the local half of a split category has no Deterministic entry %q", split.deterministic)
		}
		if !rea[split.reasoning] {
			t.Errorf("the follow-the-data half of a split category has no Reasoning entry %q", split.reasoning)
		}
	}

	// Prose, not "partial": each split entry must read as a human sentence, not a
	// one-word status. Guard against a lazy "SQL injection: partial".
	for _, s := range append(append([]string{}, proseOf(m.Deterministic)...), proseOf(m.Reasoning)...) {
		if strings.Contains(strings.ToLower(s), "partial") {
			t.Errorf("manifest uses the word 'partial' instead of explaining the split: %q", s)
		}
	}
	// A split entry should be a real sentence (long enough to explain the limit).
	foundProse := false
	for _, s := range proseOf(m.Reasoning) {
		if strings.Contains(strings.ToLower(s), "sql") && len(s) > 60 {
			foundProse = true
		}
	}
	if !foundProse {
		t.Error("the SQL reasoning entry should explain, in prose, what is mapped as surface and why")
	}
}
