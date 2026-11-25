// Package ruleengine is codefit's own matcher for a subset of the Semgrep rule
// format (PRD section 17). It is the TYPESCRIPT provider's detection mechanism,
// not the cross-language architecture: each language owns its detector, and what
// is shared across languages is the neutral model and the namematch vocabulary
// (ADR 0083). The pattern syntax itself is TypeScript-shaped — a metavariable is
// an ordinary TS identifier spelled "$X" — so patterns do not port to grammars
// where that is not a legal identifier. Deterministic detection rules are written as
// declarative YAML (the de-facto standard, with a large community corpus) and
// matched in pure Go over the provider's AST — the OCaml Semgrep/OpenGrep engine
// is NOT embedded, which would break the single, CGO-free binary.
//
// Supported operators (the core subset): pattern, pattern-either, pattern-not,
// pattern-inside, metavariables ($VAR), metavariable-regex, plus two of
// codefit's own that Semgrep does not have: metavariable-name (match an
// identifier by name component against a closed vocabulary — see [metavarname]
// in matcher.go) and metavariable-value-veto (drop a match whose bound string
// literal proves, by its own shape, that it is something else — see [valueveto]
// in valueveto.go). Object patterns also accept one scoped ellipsis, spelled
// `...$REST` (see [objectsubset] in matcher.go). The YAML loader rejects any key
// it does not know, so a misspelled operator fails the load instead of being
// dropped. The `patterns` (AND)
// operator is DECLARED in the Rule shape but compiled by nothing — a rule using
// it alongside `pattern` loads and silently drops the conjunction. Listed here
// as absent rather than supported, so the doc stops promising it (found while
// gating unparsable patterns, PR #173).
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
