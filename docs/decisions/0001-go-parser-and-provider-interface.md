# ADR 0001 — Go parser: go/ast; provider interface: parser-agnostic

**Status:** Accepted · **Date:** 2026-06-20 · **Phase:** 1 (Go provider + security sensor)

## Context

codefit needs to parse Go source to run static security and best-practice
checks, under the non-negotiable constraint `CGO_ENABLED=0` (single binary,
clean cross-compile). The PRD (§13, §14) names "tree-sitter pure Go, no CGO" as
the parsing strategy and sketches a `LanguageProvider` interface that exposes
`Grammar() *sitter.Language` and `SecurityQueries() []Query`.

Two problems surfaced when implementing the Go provider:

1. **No mature pure-Go tree-sitter exists today.** The popular bindings
   (`smacker/go-tree-sitter`) require CGO, which breaks the cross-compile
   guarantee. Pure-Go ports (`alexaandru/go-tree-sitter-bare`) are immature.
2. **For Go specifically, the stdlib already ships the canonical parser**
   (`go/parser` + `go/ast` + `go/token`): zero dependencies, zero CGO, exact
   semantics, position info for free.

## Decision

1. **Use `go/ast` (stdlib) for the Go provider.** It is strictly better than any
   tree-sitter option *for Go*: official, dependency-free, CGO-free. tree-sitter
   remains the plan for TypeScript / Java / Python in later phases (where the
   stdlib does not parse the language).

2. **Make `LanguageProvider` parser-agnostic.** Instead of leaking a parser type
   (`Grammar()`, `SecurityQueries() []Query`), the provider *owns its parser* and
   exposes analysis that returns findings:

   ```go
   AnalyzeSecurity(src SourceFile) ([]findings.Finding, error)
   AnalyzePractices(src SourceFile) ([]findings.Finding, error)
   ```

   The universal sensors stay parser-agnostic: they orchestrate the pyramid and
   ask the active provider for findings, never knowing whether it used go/ast or
   tree-sitter.

This supersedes the placeholder `SecurityQueryPatterns() []string` methods from
the scaffolding (Prompt 1), which were explicitly provisional, and deliberately
diverges from the PRD's tree-sitter-shaped interface sketch (§14).

## Consequences

- The Go provider has no external parsing dependency; `CGO_ENABLED=0` holds.
- A future TypeScript provider implements the same `AnalyzeSecurity/Practices`
  contract backed by tree-sitter, with no change to the core or the sensors.
- The provider, not the core, decides how to parse — the right place for that
  knowledge to live.
