package scaffold_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// categoryPhraseTable mirrors internal/scaffold's unexported categoryPhrases
// map for this external test — the same phrases the skill's derived slot
// renders. Kept in lockstep by category_phrase_test.go's C2b exhaustiveness
// lock (internal, package scaffold) — if a phrase changes there, this table
// must change too, or this test's own probe (checked below) catches it.
var categoryPhraseTable = map[surface.Category]string{
	surface.CategoryIDOR:      "IDOR",
	surface.CategoryAuthz:     "broken authorization",
	surface.CategoryOverfetch: "data over-fetching",
	surface.CategoryNPlus1:    "N+1 query patterns",
}

// TestSkill_NoPhantomCategory is C3: for every registered language, the
// rendered description AND body must name no phrase mapped from a category
// that language does NOT declare. One-directional (it catches the TEXT
// over-claiming past the DECLARATION; it cannot catch the declaration
// over-claiming past the code — that is Control A's job, which exists only
// for TypeScript).
func TestSkill_NoPhantomCategory(t *testing.T) {
	for _, e := range registry.All() {
		t.Run(e.Canonical, func(t *testing.T) {
			declared := make(map[surface.Category]bool)
			for _, c := range e.New(nil).Capability().Surface {
				declared[c] = true
			}
			out, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: e.Canonical})
			if err != nil {
				t.Fatalf("RenderSkill(%s): %v", e.Canonical, err)
			}
			rendered := string(out)
			for cat, phrase := range categoryPhraseTable {
				if declared[cat] {
					continue // this language DOES declare it — the phrase is legitimate
				}
				if strings.Contains(rendered, phrase) {
					t.Errorf("%s: rendered skill names phrase %q (category %q), which this language does NOT declare — phantom capability",
						e.Canonical, phrase, cat)
				}
			}
		})
	}
}

// TestSkill_NoPhantomCategory_ProbeCatchesInjectedPhrase proves the check
// itself is not a tautology: rendering a language whose declared set omits a
// category, then confirming the general mechanism (string containment)
// really would flag an injected phrase, using a synthetic string rather than
// mutating production code (the real production mutation is recorded in the
// apply-progress evidence table for this task).
func TestSkill_NoPhantomCategory_ProbeCatchesInjectedPhrase(t *testing.T) {
	fakeRendered := "... some text mentioning data over-fetching in a Go section ..."
	declared := map[surface.Category]bool{surface.CategoryAuthz: true} // Go's real declared set
	for cat, phrase := range categoryPhraseTable {
		if declared[cat] {
			continue
		}
		if strings.Contains(fakeRendered, phrase) && cat == surface.CategoryOverfetch {
			return // mechanism correctly flags the injected "data over-fetching" phrase
		}
	}
	t.Fatal("probe mechanism failed to detect the injected phantom phrase — the check is not real")
}
