// Package dbcoverage holds the DB dimension's coverage prose — what the rules in
// core/dbrules actually detect, in the same plain-prose form the coverage
// manifest uses (PRD §10, RF-07). It lives beside dbrules, not inside a
// language provider: the DB dimension is schema-driven and language-independent
// (ADR 0018 resolves the schema parser by the input's shape, not the app
// language), so its coverage prose belongs beside the rules that produce it,
// not duplicated into every ORM provider that happens to consume it.
//
// dbcoverage is a pure leaf: it imports nothing beyond the standard library, so
// it cannot create a cycle with any provider or with core/dbrules itself. A
// provider's own CoverageManifest composes these lists into its own
// Deterministic/Reasoning/NotCovered slices (ADR 0014 layering: no provider
// knowledge here, no DB knowledge duplicated there).
package dbcoverage
