// Package dwrules is the paradigm-aware DW (data-warehouse) rule family: a
// SEPARATE two-argument Rule family from dbrules.Rule (schema-only),
// mirroring how internal/core/crossrules solved "a rule needs a fact beyond
// the bare schema" (ADR 0029) without mutating dbrules.Rule. A dwrules.Rule
// reasons over db.Schema PLUS the paradigm.Classification computed by
// internal/core/paradigm — both neutral inputs, so this package imports
// ONLY internal/core/db, internal/core/findings, internal/core/paradigm and
// internal/core/surface, never a provider, never a sensor (ADR 0033).
//
// S1 shipped this package as an inert skeleton (All() empty) so RunWith could
// prove the merge mechanism before any real rule existed. S2 fills it with the
// star-schema + SCD family — DW-001 (fact without a dimension FK), DW-002
// (dimension without a surrogate key), DW-005 (facts present, no time
// dimension), DW-010 (SCD-2 without a currency index) and DW-011 (mixed SCD
// strategies) — without touching that seam. DW-021 (columnar index, S3) and
// DW-020 (partitioning, S4) are NOT here yet and remain declared-not-covered.
//
// Every rule in this package is SURFACE-only (ADR 0017). A warehouse-modelling
// choice is a design judgment, not a structurally undeniable defect: an
// intentional degenerate fact, a natural-key dimension carried over from a
// source system, or a deliberately mixed SCD policy are all legitimate. So a
// DW rule states the observed shape and the agent decides — it never affirms.
//
// Every rule also reads table ROLES from the Classification and never
// re-derives them. A rule that re-guessed roles would fork the S1 heuristic
// and drift from it (locked decision A5).
package dwrules
