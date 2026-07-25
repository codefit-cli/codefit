// Package dwrules is the paradigm-aware DW (data-warehouse) rule family: a
// SEPARATE two-argument Rule family from dbrules.Rule (schema-only),
// mirroring how internal/core/crossrules solved "a rule needs a fact beyond
// the bare schema" (ADR 0029) without mutating dbrules.Rule. A dwrules.Rule
// reasons over db.Schema PLUS the paradigm.Classification computed by
// internal/core/paradigm — both neutral inputs, so this package imports
// ONLY internal/core/db, internal/core/findings and internal/core/paradigm,
// never a provider, never a sensor (ADR 0033). It does NOT import
// internal/core/surface in S1 — OwnedCategories() returns nil because no DW
// rule owns a surface category yet; that import lands with the first real
// rule's category, in S2 (readability fix, S1 review ledger: this doc
// comment previously claimed a surface import that did not exist).
//
// S1 ships this package as an inert skeleton: All() is empty, so RunWith
// proves the merge mechanism before any real DW rule exists. S2 adds
// DW-001/002/005/010/011 as entries in All() without touching the seam.
package dwrules
