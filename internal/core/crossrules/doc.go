// Package crossrules is codefit's family of deterministic checks that reason over
// BOTH neutral inputs at once — the schema (db.Schema) AND the code's query
// filters (query.QueryFilter) — the code↔schema cross that index-vs-query needs
// (ADR 0029). It is DISTINCT from internal/core/dbrules (schema-only): a cross
// rule's Check takes two arguments, so it has its own Rule interface and runner.
//
// Like dbrules, it lives in the core and NEVER imports a provider: both inputs are
// neutral core types (db.Schema from a SchemaParser, query.QueryFilter from a
// QueryExtractor), so the cross reasons over them without depending on any
// language. This is the whole point of the neutral query model — it lets the core
// cross code and schema without a core→provider edge. Locked by
// TestCoreCrossrules_NeverImportProvider.
//
// This slice (0.2.x) ships the INFRASTRUCTURE only: the neutral model, the runner,
// and the reconciliation, with an EMPTY rule set — the seam proven end-to-end with
// no output change (ADR 0020). The first cross-rules, DB-010 (missing index on a
// filtered column) and DB-013 (missing composite index), are later slices built on
// this infra.
package crossrules
