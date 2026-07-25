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
package paradigm
