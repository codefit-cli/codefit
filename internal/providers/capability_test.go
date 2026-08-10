package providers_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
)

// TestCapability_SurfaceMustBeSubsetOfProviderCategories is C2: every category
// a Capability declares in Surface must be a member of
// surface.ProviderCategories — the vocabulary D1b just locked to the const
// block. This is what makes a provider's "N of 4" claim DERIVED
// (len(surface.ProviderCategories) is the denominator), never a literal a
// provider could drift from.
func TestCapability_SurfaceMustBeSubsetOfProviderCategories(t *testing.T) {
	valid := providers.Capability{
		Security:  providers.RuleSet{Declared: []string{"SEC-001"}, Enumerable: false},
		Practices: providers.RuleSet{Declared: []string{"PRAC-001"}, Enumerable: false},
		Surface:   []surface.Category{surface.CategoryAuthz},
	}
	if !valid.ValidSurface() {
		t.Error("a Capability whose Surface is a real subset of ProviderCategories must be ValidSurface() == true")
	}

	invalid := providers.Capability{Surface: []surface.Category{surface.Category("not-a-real-category")}}
	if invalid.ValidSurface() {
		t.Error("a Capability whose Surface contains a category outside ProviderCategories must be ValidSurface() == false (C2)")
	}
}
