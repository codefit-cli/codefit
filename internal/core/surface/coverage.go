package surface

import (
	"fmt"
	"slices"
	"strings"
)

// CoverageStatement declares which of a vocabulary's categories a provider
// mapped and which it did not — the concrete form of I5 ("what is not covered
// is declared") applied to language reach. Mapped and NotMapped are always
// non-nil (never a bare nil slice that could be mistaken for "not computed"):
// an EMPTY NotMapped means "every category was mapped", stated the same way
// whether the vocabulary has one entry or ten.
type CoverageStatement struct {
	Mapped    []Category `json:"mapped"`
	NotMapped []Category `json:"not_mapped"`
	// Note is the prose form of the same split — the part that survives into
	// an agent's reasoning, per docs/specs/declared-partial-language-exposure.md
	// R2 ("machine-readable, because an agent branches on it; and in prose,
	// because the prose is what survives into an agent's reasoning").
	Note string `json:"note"`
}

// DeriveCoverage computes the CoverageStatement for a provider's mapped
// surface categories against the real, locked surface.ProviderCategories
// vocabulary. This is the production entry point every caller (the coverage
// tool, the scan responses) uses.
func DeriveCoverage(mapped []Category) CoverageStatement {
	return DeriveCoverageFrom(mapped, ProviderCategories)
}

// DeriveCoverageFrom computes the CoverageStatement for mapped against an
// explicit vocabulary, rather than the package-level ProviderCategories. It
// exists so the derivation can be exercised against a synthetic vocabulary in
// tests (proving the split is DERIVED by walking whatever vocabulary is
// passed in, never a hardcoded literal) without touching the real, AST-locked
// const block. DeriveCoverage is this function bound to the real vocabulary.
func DeriveCoverageFrom(mapped, vocabulary []Category) CoverageStatement {
	mappedOut := make([]Category, 0, len(vocabulary))
	notMappedOut := make([]Category, 0, len(vocabulary))
	for _, cat := range vocabulary {
		if slices.Contains(mapped, cat) {
			mappedOut = append(mappedOut, cat)
		} else {
			notMappedOut = append(notMappedOut, cat)
		}
	}
	return CoverageStatement{
		Mapped:    mappedOut,
		NotMapped: notMappedOut,
		Note:      coverageNote(mappedOut, notMappedOut, len(vocabulary)),
	}
}

// coverageNote renders the "N of M mapped; not mapped: ..." prose. "M of M
// mapped; nothing unmapped" is stated explicitly rather than omitted, so an
// empty NotMapped list is never silently indistinguishable from "not
// computed".
func coverageNote(mapped, notMapped []Category, total int) string {
	if len(notMapped) == 0 {
		return fmt.Sprintf("%d of %d surface categories mapped (%s); nothing unmapped.",
			len(mapped), total, joinCategories(mapped))
	}
	return fmt.Sprintf("%d of %d surface categories mapped (%s); not mapped: %s — these were never searched for, not merely absent from this scan.",
		len(mapped), total, joinCategories(mapped), joinCategories(notMapped))
}

func joinCategories(cats []Category) string {
	if len(cats) == 0 {
		return "none"
	}
	strs := make([]string, len(cats))
	for i, c := range cats {
		strs[i] = string(c)
	}
	return strings.Join(strs, ", ")
}
