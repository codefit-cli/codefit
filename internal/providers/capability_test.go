package providers_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
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

// isZeroCapability reports whether c carries no declaration at all — C1's
// completeness check. A zero Capability means a provider forgot to implement
// Capability() beyond the interface's compile-time requirement (a bare
// `return providers.Capability{}` still satisfies the interface).
func isZeroCapability(c providers.Capability) bool {
	return len(c.Security.Declared) == 0 && len(c.Practices.Declared) == 0 &&
		len(c.Surface) == 0 && !c.CoverageManifest
}

// TestCapability_EveryRegisteredProviderDeclaresNonZero is C1, driven through
// the interface: every REAL registered provider (constructed the same way
// production does, never a hand-built struct) must declare a non-zero
// Capability, and that Capability's Surface must satisfy C2.
func TestCapability_EveryRegisteredProviderDeclaresNonZero(t *testing.T) {
	registered := []providers.LanguageProvider{golang.New(), typescript.New()}
	for _, p := range registered {
		t.Run(p.Language(), func(t *testing.T) {
			cap := p.Capability()
			if isZeroCapability(cap) {
				t.Errorf("%s.Capability() is the zero value — every registered provider must declare a non-zero Capability (C1)", p.Language())
			}
			if !cap.ValidSurface() {
				t.Errorf("%s.Capability().Surface is not a subset of surface.ProviderCategories (C2)", p.Language())
			}
		})
	}
}
