# ADR 0003 — AST boundary: a neutral `core/syntax.Node`, provider interface convergence deferred

**Status:** Accepted · **Date:** 2026-06-22 · **Phase:** 1 (TypeScript provider, parsing)

## Context

codefit now has two real parsers: the Go provider (`go/ast`) and the new
TypeScript provider (`gotreesitter`). The core's `ruleengine.Engine.Match(rules,
ast any)` left the AST argument deliberately abstract "until the per-language AST
adapter is designed in Fase 1" — this is that design.

Two questions had to be answered:

1. How does a parsed AST reach the core's rule engine **without the core
   depending on any concrete parser** (`go/ast`, gotreesitter)?
2. Should the `LanguageProvider` interface converge now to the PRD §16 shape
   (`Parse(*AST)` + declarative `SecurityRules()`/`SurfaceQueries()`, core runs
   the rules), replacing the current provider-owns-analysis shape (ADR 0001
   addendum, `AnalyzeSecurity/Practices/Surface`)?

## Decision

### A neutral AST boundary in the core: `internal/core/syntax`

Introduce `syntax.Node`, a minimal, parser-agnostic tree interface that lives in
the core. The core (rule engine, sensors) navigates a parsed file **only**
through `syntax.Node`; it never imports `go/ast` or `gotreesitter`. Each provider
adapts its parser's nodes to `syntax.Node` (the TypeScript provider's `tsNode`
wraps a gotreesitter node, hiding the `*Language` and source bytes the parser
threads through its calls).

`syntax.Node` is intentionally **minimal** — only what tree navigation needs
today: `Type`, `Text`, `NamedChildCount`/`NamedChild`, `ChildByField`,
`StartLine`, `HasError`. Notably **no `Parent()`**: the `pattern-inside` operator
that would need it is implemented in Prompt 1.2, and the interface should not be
shaped for a caller that does not yet exist (YAGNI). It is extended then, with a
real consumer and evidence of how a parent is exposed by both parsers.

### `Parse` stays out of the shared `LanguageProvider` interface — convergence deferred

The TypeScript provider's `Parse(SourceFile) (syntax.Node, error)` is a method on
the provider, **not** added to the shared `LanguageProvider` interface yet. The
provider still satisfies the current interface with `AnalyzeSecurity/Practices/
Surface` as stubs (filled in Prompts 1.2/1.3).

Convergence to the PRD §16 shape — moving `Parse` + declarative `Rules` into the
interface, migrating the Go provider, deprecating the `Analyze*` methods — is
**deferred to Prompt 1.2+**.

## Why defer the convergence

Two reasons, the second stronger than the first:

1. **Scope / blast radius.** Adding `Parse` to the shared interface now forces the
   Go provider to implement it too, which would touch the Go provider and break
   this phase's "self-audit green, Go untouched" boundary, mixing parsing work
   with a provider migration.

2. **Not enough evidence to freeze the shared interface.** The PRD §16 interface
   is the *vision*. But a shared abstraction must be validated by **two real
   parsers exercising it**, not designed against one. `syntax.Node` is brand new;
   only the TypeScript provider exercises it today. Designing the converged
   `LanguageProvider` now — with a single parser behind the abstraction — would be
   designing in a vacuum, the same mistake avoided with `go/ast` (ADR 0001) and
   the rule-engine shape. When Prompt 1.2 builds the real rule engine and **both**
   the Go and TypeScript providers drive the same `syntax.Node`, we will know
   whether the abstraction holds without leaks. If `go/ast` and gotreesitter do
   not fit the same boundary cleanly, it is far better to discover that against
   real code than against a frozen interface.

The neutral `syntax.Node` lives in the core from day one (it *is* the boundary);
the *provider interface convergence* waits for two parsers to validate it.

## Consequences

- The core gains a parser-agnostic AST boundary; `ruleengine.Engine.Match`'s
  `ast any` will be a `syntax.Node`.
- The Go provider is untouched this phase (self-audit stays green). Two provider
  shapes coexist temporarily (Go: analysis-internal; TS: `Parse` → `syntax.Node`
  + stubs) — an accepted, documented transient until 1.2+ unifies them.
- `syntax.Node` will grow (e.g. `Parent()`) driven by real operators, not
  speculation.
