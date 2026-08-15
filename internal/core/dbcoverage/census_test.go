package dbcoverage_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/coverage"
	"github.com/codefit-cli/codefit/internal/core/dbcoverage"
)

// proseOf projects entries back to the prose lines they replaced, so the
// controls written against the old []string shape keep asking their own
// question instead of being rewritten alongside the shape change.
func proseOf(entries []coverage.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, coverage.ProseOf(e))
	}
	return out
}

// The census tokenizer is ruleIDToken, declared in dbcoverage_test.go — the
// very pattern Control B has used to hunt rule-ID-shaped mentions in this prose
// since before this change. Reusing it rather than declaring a second copy is
// the point: one pattern cannot disagree with itself. loadCensus additionally
// asserts the committed census was captured with that same pattern, so a change
// to either side has to reckon with the other.

type census struct {
	CapturedFrom string                    `json:"captured_from"`
	Pattern      string                    `json:"pattern"`
	Buckets      map[string]map[string]int `json:"buckets"`
	BucketTotals map[string]int            `json:"bucket_totals"`
	Total        int                       `json:"total"`
}

func loadCensus(t *testing.T) census {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "ruleid_census.json"))
	if err != nil {
		t.Fatalf("reading the rule-id census: %v", err)
	}
	var c census
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parsing the rule-id census: %v", err)
	}
	// A fixture is verified by its CONTENT, never by the fact that it opened.
	// An empty census would make every comparison below pass by saying nothing.
	if c.Total == 0 || len(c.Buckets) != 4 {
		t.Fatalf("the census fixture is not a census: %d tokens across %d buckets", c.Total, len(c.Buckets))
	}
	if c.Pattern != ruleIDToken.String() {
		t.Fatalf("the census was tokenized with %q but this test uses %q — "+
			"the two must be the same pattern or the comparison is meaningless",
			c.Pattern, ruleIDToken.String())
	}
	return c
}

func tokenize(lines []string) map[string]int {
	counts := map[string]int{}
	for _, s := range lines {
		for _, m := range ruleIDToken.FindAllString(s, -1) {
			counts[m]++
		}
	}
	return counts
}

// TestRuleIDCensus_IsConservedByTheEntryShape is the conservation control for
// this change. The DB prose was re-shaped from bare strings into named entries;
// nothing was rewritten, split or summarized. If that is true, the multiset of
// rule ids over each bucket's claim-plus-detail is EXACTLY the multiset captured
// from the tree before the first edit.
//
// This is the check that makes the re-shaping auditable rather than trusted. It
// is deliberately a multiset with counts and not a set: a rule id that appears
// three times and comes back once has lost two of its declarations, and a set
// comparison would call that equal.
//
// Claim plus detail, not detail alone, so a claim can never smuggle in a rule id
// the prose does not declare — and so a later slice cannot quietly move a
// declaration from the prose into a claim and call the census unchanged.
func TestRuleIDCensus_IsConservedByTheEntryShape(t *testing.T) {
	c := loadCensus(t)

	for name, entries := range map[string][]coverage.Entry{
		"Deterministic":      dbcoverage.Deterministic(),
		"Reasoning":          dbcoverage.Reasoning(),
		"NotCovered":         dbcoverage.NotCovered(),
		"DeliveredElsewhere": dbcoverage.DeliveredElsewhere(),
	} {
		want, ok := c.Buckets[name]
		if !ok {
			t.Errorf("the census has no bucket named %q", name)
			continue
		}
		text := make([]string, 0, len(entries)*2)
		for _, e := range entries {
			text = append(text, e.Claim, e.Detail)
		}
		got := tokenize(text)
		if reflect.DeepEqual(got, want) {
			continue
		}
		for _, id := range unionIDs(got, want) {
			if got[id] != want[id] {
				t.Errorf("%s: rule id %s appeared %d times before the change and %d times after",
					name, id, want[id], got[id])
			}
		}
	}
}

func unionIDs(a, b map[string]int) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]int{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestEntries_AreNamedClaimedAndWithinTheCap: an entry with no id cannot be
// asked for, an entry with no claim is counted and says nothing, and a claim
// over the cap is an author writing prose where a one-liner belongs.
func TestEntries_AreNamedClaimedAndWithinTheCap(t *testing.T) {
	all := append(append(append(append([]coverage.Entry{},
		dbcoverage.Deterministic()...), dbcoverage.Reasoning()...),
		dbcoverage.NotCovered()...), dbcoverage.DeliveredElsewhere()...)
	if len(all) == 0 {
		t.Fatal("vacuum: the DB dimension declared no entries at all")
	}

	seen := map[string]bool{}
	for _, e := range all {
		if e.ID == "" {
			t.Errorf("an entry has no id: %.60q", e.Claim)
		}
		if seen[e.ID] {
			t.Errorf("duplicate id %q", e.ID)
		}
		seen[e.ID] = true
		if e.Claim == "" {
			t.Errorf("entry %q has an empty claim", e.ID)
		}
		if len(e.Claim) > coverage.MaxClaimBytes {
			t.Errorf("entry %q has a %d-byte claim, over the %d-byte cap — shorten the claim, never drop the entry",
				e.ID, len(e.Claim), coverage.MaxClaimBytes)
		}
	}
}
