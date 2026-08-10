package scaffold

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
)

// TestCategoryPhrases_ExhaustiveOverProviderCategories is C2b: the
// category->phrase map the skill's derived slot renders from must have an
// entry for EVERY category in surface.ProviderCategories, both directions —
// same lock shape as D1b's defaultSeverity/titles exhaustiveness. A category
// added to the vocabulary with no phrase here must be a hard failure, not a
// silent blank in the agent's skill.
func TestCategoryPhrases_ExhaustiveOverProviderCategories(t *testing.T) {
	for _, c := range surface.ProviderCategories {
		if _, ok := categoryPhrases[c]; !ok {
			t.Errorf("categoryPhrases has no entry for %q (declared in surface.ProviderCategories)", c)
		}
	}
	declared := make(map[surface.Category]bool, len(surface.ProviderCategories))
	for _, c := range surface.ProviderCategories {
		declared[c] = true
	}
	for c := range categoryPhrases {
		if !declared[c] {
			t.Errorf("categoryPhrases has a stale entry %q not in surface.ProviderCategories", c)
		}
	}
}
