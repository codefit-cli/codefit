// Package ruleengine is codefit's own matcher for a subset of the Semgrep rule
// format (PRD section 17). Deterministic detection rules are written as
// declarative YAML (the de-facto standard, with a large community corpus) and
// matched in pure Go over the provider's AST — the OCaml Semgrep/OpenGrep engine
// is NOT embedded, which would break the single, CGO-free binary.
//
// Supported operators (the core subset): pattern, pattern-either, patterns,
// pattern-not, pattern-inside, metavariables ($VAR), metavariable-regex.
// Deliberately NOT supported: taint mode and pattern-sources/sinks/sanitizers —
// their role is covered by the agent reasoning over mapped surface.
//
// Status: BUILT (Fase 1). The rule shape ([Rule]), the YAML loader, and the
// matcher itself ([Compile], [Match], [Matches]) are implemented, and the
// TypeScript provider runs its deterministic security rules through them.
// Note the shape: the matcher is package-level FUNCTIONS over
// [syntax.Node], not the [Engine] interface — [Engine] is a declared contract
// that nothing implements today, kept for the per-language adapter it was drawn
// for. Do not read it as the entry point.
package ruleengine
