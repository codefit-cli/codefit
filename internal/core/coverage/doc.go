// Package coverage models codefit's coverage manifest (PRD section 10, RF-07):
// the explicit, per-language declaration of what codefit detects and what it
// does not. It turns the blind spot from "invisible and dangerous" into
// "declared and known", and is the single source for both the human-facing
// COVERAGE.md and the agent-facing codefit-coverage tool, and for the report's
// coverage_note.
//
// Status: SKELETON. This declares the [Manifest] type. Each LanguageProvider
// supplies its manifest and the report derives its coverage_note from it in
// Fase 1; at that point the report's coverage_note stops being omitempty and is
// always populated (there is always something to declare about coverage).
package coverage
