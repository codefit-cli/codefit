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
// Status: SKELETON. This declares the rule shape ([Rule]) and the matcher
// contract ([Engine]). The matcher itself is implemented in Fase 1; the AST
// argument is abstract (any) until the per-language AST adapter is designed.
package ruleengine
