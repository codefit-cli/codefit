// Package coverage models codefit's coverage manifest (PRD section 10, RF-07):
// the explicit, per-language declaration of what codefit detects and what it
// does not. It turns the blind spot from "invisible and dangerous" into
// "declared and known", and is the single source for both the human-facing
// COVERAGE.md and the agent-facing codefit-coverage tool, and for the report's
// coverage_note.
//
// Scope: this package declares the [Manifest] TYPE only — deliberately, not as
// a stub. The content lives where it is owned: each LanguageProvider supplies
// its own manifest (internal/providers/<lang>/coverage.go), and the DB
// dimension, which belongs to no language, supplies its own neutral one
// (internal/core/dbcoverage). The codefit-coverage tool serves them.
//
// Known gap, stated rather than promised: report.CoverageNote is still
// `omitempty` and is not yet derived from a Manifest. An earlier draft of this
// comment asserted that would land in Fase 1. It did not.
package coverage
