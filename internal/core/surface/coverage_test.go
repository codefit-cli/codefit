package surface_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
)

// TestDeriveCoverage_SplitsMappedFromNotMapped is R2/R1's core mechanism: given
// what a provider mapped, DeriveCoverage must report exactly which of
// surface.ProviderCategories were mapped and which were not — the "1 of 4"
// claim the coverage manifest and the scan response both need, computed rather
// than asserted.
func TestDeriveCoverage_SplitsMappedFromNotMapped(t *testing.T) {
	got := surface.DeriveCoverage([]surface.Category{surface.CategoryAuthz})

	if len(got.Mapped) != 1 || got.Mapped[0] != surface.CategoryAuthz {
		t.Errorf("Mapped = %v, want [authz]", got.Mapped)
	}
	wantNotMapped := []surface.Category{surface.CategoryIDOR, surface.CategoryOverfetch, surface.CategoryNPlus1}
	if len(got.NotMapped) != len(wantNotMapped) {
		t.Fatalf("NotMapped = %v, want 3 categories (idor, overfetch, nplus1)", got.NotMapped)
	}
	for _, want := range wantNotMapped {
		found := false
		for _, c := range got.NotMapped {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("NotMapped missing %q: %v", want, got.NotMapped)
		}
	}
	if got.Note == "" {
		t.Error("Note must be non-empty — the prose statement is what survives into an agent's reasoning")
	}
}

// TestDeriveCoverage_AllMappedYieldsEmptyNotMapped: a provider mapping every
// category in the vocabulary (TypeScript today) must produce an EMPTY
// NotMapped, not a nil-vs-empty ambiguity that could be misread.
func TestDeriveCoverage_AllMappedYieldsEmptyNotMapped(t *testing.T) {
	got := surface.DeriveCoverage(surface.ProviderCategories)
	if len(got.NotMapped) != 0 {
		t.Errorf("NotMapped = %v, want empty when every category is mapped", got.NotMapped)
	}
	if len(got.Mapped) != len(surface.ProviderCategories) {
		t.Errorf("Mapped = %v, want all %d categories", got.Mapped, len(surface.ProviderCategories))
	}
}

// TestDeriveCoverageFrom_DerivedNotLiteral is test-contract item 4: the
// not-mapped set must be DERIVED from the vocabulary passed in, never a
// hardcoded literal. A synthetic vocabulary with a fifth, unmapped category
// must produce that category in NotMapped — proving the computation walks the
// vocabulary slice rather than a fixed list, with no other edit to production
// code. (Mutation: replace the walk with a hardcoded 3-element literal — this
// test would then miss the synthetic fifth category and fail.)
func TestDeriveCoverageFrom_DerivedNotLiteral(t *testing.T) {
	syntheticVocabulary := append(append([]surface.Category{}, surface.ProviderCategories...), surface.Category("hypothetical-fifth"))
	mapped := []surface.Category{surface.CategoryAuthz}

	got := surface.DeriveCoverageFrom(mapped, syntheticVocabulary)

	found := false
	for _, c := range got.NotMapped {
		if c == surface.Category("hypothetical-fifth") {
			found = true
		}
	}
	if !found {
		t.Errorf("NotMapped = %v, want it to include the synthetic fifth category — the not-mapped set must be "+
			"derived from the vocabulary parameter, not a fixed literal", got.NotMapped)
	}
	if len(got.NotMapped) != len(syntheticVocabulary)-1 {
		t.Errorf("NotMapped has %d entries, want %d (vocabulary size minus the one mapped category)",
			len(got.NotMapped), len(syntheticVocabulary)-1)
	}
}

// TestDeriveCoverage_UsesRealProviderCategoriesVocabulary proves DeriveCoverage
// (the production entry point) is DeriveCoverageFrom bound to the real, locked
// surface.ProviderCategories — not an independent copy that could drift from
// it.
func TestDeriveCoverage_UsesRealProviderCategoriesVocabulary(t *testing.T) {
	mapped := []surface.Category{surface.CategoryAuthz}
	direct := surface.DeriveCoverageFrom(mapped, surface.ProviderCategories)
	viaDeriveCoverage := surface.DeriveCoverage(mapped)

	if len(direct.NotMapped) != len(viaDeriveCoverage.NotMapped) {
		t.Fatalf("DeriveCoverage must equal DeriveCoverageFrom(mapped, ProviderCategories): NotMapped %v vs %v",
			viaDeriveCoverage.NotMapped, direct.NotMapped)
	}
}
