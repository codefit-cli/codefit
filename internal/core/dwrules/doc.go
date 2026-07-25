// Package dwrules is the paradigm-aware DW (data-warehouse) rule family: a
// SEPARATE two-argument Rule family from dbrules.Rule (schema-only),
// mirroring how internal/core/crossrules solved "a rule needs a fact beyond
// the bare schema" (ADR 0029) without mutating dbrules.Rule. A dwrules.Rule
// reasons over db.Schema PLUS the paradigm.Classification computed by
// internal/core/paradigm — both neutral inputs, so this package imports
// ONLY internal/core/db, internal/core/findings, internal/core/paradigm and
// internal/core/surface, never a provider, never a sensor (ADR 0033).
//
// S1 ships this package as an inert skeleton: All() is empty, so RunWith
// proves the merge mechanism before any real DW rule exists. S2 adds
// DW-001/002/005/010/011 as entries in All() without touching the seam.
package dwrules
