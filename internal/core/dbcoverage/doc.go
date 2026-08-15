// Package dbcoverage holds the DB dimension's coverage prose — what the rules in
// core/dbrules actually detect, in the same plain-prose form the coverage
// manifest uses (PRD §10, RF-07). It lives beside dbrules, not inside a
// language provider: the DB dimension is schema-driven and language-independent
// (ADR 0018 resolves the schema parser by the input's shape, not the app
// language), so its coverage prose belongs beside the rules that produce it,
// not duplicated into every ORM provider that happens to consume it.
//
// dbcoverage imports exactly one codefit package, core/coverage, for the Entry
// type its four functions return. That is leaf to leaf: coverage imports nothing
// of codefit's, so there is no cycle in either direction, and neither package
// imports a provider. A provider's own CoverageManifest composes these entries
// into its own Deterministic/Reasoning/NotCovered buckets by append, exactly as
// before (ADR 0014 layering: no provider knowledge here, no DB knowledge
// duplicated there).
//
// dbcoverage does not know that detail resolution exists. It returns entries and
// nothing else; whether a caller serves a claim, the prose, or both is entirely
// the caller's business, so the DB dimension never grows a second code path to
// keep in step with the first.
package dbcoverage
