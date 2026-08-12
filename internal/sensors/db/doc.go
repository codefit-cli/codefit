// Package db is the database-structure sensor (dimension "db"). Unlike the
// security sensor it does not walk source files: it resolves the configured
// database.schema_paths from disk (it is the filesystem-side caller, ADR 0014),
// asks the provider's SchemaParser for a neutral db.Schema, runs the core dbrules
// over it, and stamps baseline fingerprints. It is honest about NOT measuring:
// disabled, no schema_paths, or a provider without a schema parser return
// Measured=false with a note — never a false "clean, 0 findings".
//
// DECLARED GAP — this sensor does NOT apply path criticality. RF-10 weights a
// finding's severity by its path class (production/test/example) and its
// wording says "cada finding", but config.PathCriticalityFor has exactly one
// caller and it is the security sensor. A db finding is schema-scoped: it is
// located by table and column inside a schema source, so a project-relative
// glob would classify the schema FILE, not the object the finding is about, and
// "a test table" is not a question RF-10 answers. The gap is deliberate and
// open, not an oversight: see
// docs/decisions/0070-path-criticality-is-configurable-and-reaches-only-the-security-sensor.md
// before adding criticality here.
package db
