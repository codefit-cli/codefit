// Package db is the database-structure sensor (dimension "db"). Unlike the
// security sensor it does not walk source files: it resolves the configured
// database.schema_paths from disk (it is the filesystem-side caller, ADR 0014),
// asks the provider's SchemaParser for a neutral db.Schema, runs the core dbrules
// over it, and stamps baseline fingerprints. It is honest about NOT measuring:
// disabled, no schema_paths, or a provider without a schema parser return
// Measured=false with a note — never a false "clean, 0 findings".
package db
