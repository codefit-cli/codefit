package surfaceindex

import "github.com/codefit-cli/codefit/internal/core/findings"

// Entry is the light projection of a findings.SurfaceItem that every index
// carries, for every item, always (design D4 — nothing is withheld here).
//
// What stays, and why (spec "index entry shape"): Fingerprint stays because
// codefit-baseline-accept takes fingerprints directly — detail-only would
// force an extra round trip before a named item could be accepted.
// StructuralFacts stays because it is the only filterable axis across 18
// disjoint db surface categories with no severity field; without it,
// fetching everything is the only way to learn more than a category name.
//
// What is dropped, and why: Snippet, StructuralSignals, ReasonToReview and
// IndirectCall are prose/detail fields — exactly the ~85% of a db surface
// item's serialized weight that this projection exists to keep out of the
// default response (measurement #1679). They are served only through
// Resolve, by id.
type Entry struct {
	ID              string          `json:"id"`
	Category        string          `json:"category"`
	File            string          `json:"file"`
	Line            int             `json:"line"`
	Fingerprint     string          `json:"fingerprint,omitempty"`
	StructuralFacts map[string]bool `json:"structural_facts,omitempty"`
}

// Index projects every item into its light Entry form and returns the count
// of items indexed, taken from the input slice's own length — computed
// independently of how the entries slice below is built, so a future edit
// that truncates the returned entries can never also, silently, shrink the
// count that is supposed to catch it (the mutation this package is
// mutation-tested against: see the M3 conservation test in internal/mcp,
// anchored to the db sensor's own population, never to a response's own
// index — the trap recorded in the coverage-chain archive, obs #1664).
func Index(items []findings.SurfaceItem) ([]Entry, int) {
	n := len(items)
	entries := make([]Entry, 0, n)
	for _, it := range items {
		entries = append(entries, Entry{
			ID:              it.ID,
			Category:        it.Category,
			File:            it.File,
			Line:            it.Line,
			Fingerprint:     it.Fingerprint,
			StructuralFacts: it.StructuralFacts,
		})
	}
	return entries, n
}

// Resolve returns the FULL findings.SurfaceItem for each requested id, plus
// the ids that matched nothing. An unmatched id is NAMED, never silently
// dropped (design D3): codefit is stateless and cannot tell "this id never
// existed" from "the schema moved between calls and it is gone" — an empty
// success would hide that distinction the caller needs to reason about.
func Resolve(items []findings.SurfaceItem, ids []string) (found []findings.SurfaceItem, unrecognized []string) {
	byID := make(map[string]findings.SurfaceItem, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, id := range ids {
		if it, ok := byID[id]; ok {
			found = append(found, it)
			continue
		}
		unrecognized = append(unrecognized, id)
	}
	return found, unrecognized
}
