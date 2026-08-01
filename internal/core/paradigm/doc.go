// Package paradigm computes a database schema's analytic-vs-transactional
// paradigm and each table's warehouse role, as a pure function over the
// neutral internal/core/db model. It imports ONLY internal/core/db — never a
// provider, never internal/config, never a sensor (ADR 0015's schema-only
// simplicity, extended to this second neutral input by ADR 0033).
//
// Detect SEEDS the classification from structural evidence; Resolve applies
// an explicit developer override on top, honoring the project's
// innegociable developer-autonomy principle: an explicit config value always
// wins over detection.
//
// The package also holds the SCHEMA GATE (schemagate.go, ADRs 0035/0036/0037):
// six schema-wide warehouse signals that ask the paradigm question TOP-DOWN.
// Detect CONSULTS IT FIRST — a schema that shows none of the three deciding
// signals is not a warehouse, and no table inside it receives a warehouse role
// at all. The gate was built inert in stage 1 so its verdict could be selected
// from a 26-corpus measurement instead of a hunch; stage 2 (ADR 0037) wired it
// in, and that wiring is test-locked structurally and behaviorally.
//
// Resolve is where developer autonomy meets the gate: an explicit olap/mixed
// RESTORES roles a closed gate withheld, an explicit oltp restores nothing.
package paradigm
