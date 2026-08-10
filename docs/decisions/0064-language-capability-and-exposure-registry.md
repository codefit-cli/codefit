# ADR 0064 — Capability and exposure are declared, checkable facts behind one registry

**Status:** Accepted · **Date:** 2026-08-10 · **Phase:** 3 (roadmap P1-1b)

## Context

`docs/roadmap.md` P1-1b named the defect: three independent switches
(`internal/mcp/scanall.go`'s `providerForLanguage`, `internal/mcp/surface.go`'s
`providerFor`, `internal/scaffold/detect.go`'s `detectLanguage`) each decide "does
codefit support this language" by a different signal — language name, file
extension, marker file — and could silently disagree. P0-5 closed the
disagreement risk with three regression locks (Lock A/B/C) but deliberately did
not converge the three switches onto one source.

A second, related question had no answer at all: **what does a provider
actually implement**, independent of which resolvers currently expose it? codefit
had five hand-written, disagreeing answers to "what can codefit do for this
language" (the roadmap's own count) — a coverage manifest for TypeScript, a
skill description, MCP tool descriptions, the three resolver switches, and
nothing at all for Go beyond its presence in `detectLanguage`. None of them
were checked against each other or against the real rule/parser code.

## Decision

**Capability is provider-owned; exposure is table-owned; the registry answers
both underneath four preserved queries.**

- `LanguageProvider.Capability() Capability` joins the interface's identity
  block (beside `Language()`, `Frameworks()`, `FileExtensions()`). Every
  registered provider must declare a non-zero `Capability` — checked at
  compile time (the method is in the interface) and at test time (C1, driven
  through the interface with the two real providers, never a hand-built
  struct).
- `Capability` composes `Security`/`Practices` `RuleSet{Declared []string,
  Enumerable bool}` and `Surface []surface.Category` (a subset of
  `surface.ProviderCategories`, C2) and `CoverageManifest bool`.
  `RuleSet.Declared` is never a count — a count cannot be checked against
  anything; a list of IDs can. `Enumerable` distinguishes a list that is
  provably derived from a real rule loader (TypeScript's YAML-backed security
  rules, `Enumerable: true`) from a hand-maintained mirror of a provider's own
  Go source (everything else today, `Enumerable: false`).
- Where `Enumerable: true`, **Control A** asserts strict set equality, both
  directions, between `Declared` and the real loader's output
  (`ruleengine.LoadFS`) — not a copy of the rule list, the loader itself,
  driven directly.
- `internal/providers/registry` is the ONE ordered table mapping language to
  provider, answering `All`, `ByName`, `ByExtension`, `ByMarkerFile`. File
  extensions are deliberately **not** a table field — `ByExtension` reads them
  from `New(nil).FileExtensions()`, so they cannot become a sixth hand-written
  answer to the same question. Each `Entry` carries an `Exposure{SecurityScan,
  SurfaceTools, InitDetect bool}` — independent of `Capability`, and the one
  place a language becomes reachable through a given resolver. A provider
  cannot declare its own exposure (it does not know which resolvers exist);
  only the registry does.
- The three original switches now query the registry instead of building a
  concrete provider: `scanall.go`'s `providerForLanguage`/
  `SupportedLanguageNames` filter by `Exposure.SecurityScan`; `surface.go`'s
  `providerFor` filters by `Exposure.SurfaceTools`; `scaffold/detect.go`'s
  `detectLanguage` resolves by `ByMarkerFile`, table order preserving the
  existing `go.mod`-before-`package.json` priority. `scaffold/detect.go` drops
  its `golang`/`typescript` imports entirely — it no longer constructs a
  provider itself.
- `internal/mcp/schemaparser.go` stays outside the table (ADR 0018 unchanged):
  it resolves by the shape of the input (`.prisma`/`.sql`), not by language.

## Vocabulary control (D1b)

`surface.ProviderCategories` (`internal/core/surface/surface.go`) is the new
enumeration of provider-emitted categories (`idor`, `authz`, `overfetch`,
`nplus1`), excluding the `db-`/`dw-` prefixed schema-derived namespace. It is
locked against the const block it enumerates by a `go/ast` parse
(`vocabulary_test.go`), in both directions — this is deliberately **not** the
shape `dbcoverage_test.go`'s Control C was refused for (a second hand-written
list asserted against a manifest, which only moves the drift three lines): the
list here is asserted against the const block itself, read from source, so
there is no third place left to drift to. The same test locks
`framework.go`'s `defaultSeverity`/`titles` maps exhaustive over the
enumeration — two more previously-unchecked hand-maintained mirrors of the
same four categories, both of which silently fell back (medium severity, a
generic title) for anything undeclared.

## Locks A/B/C, and the four new checks (C0–C2, C4, C5)

The registry rewiring was verified to keep every consumer's answer set
byte-identical to before this change, by re-running the pre-existing regression
locks after each step, not by inspection:

- **Lock A** (`providerForLanguage`/`SupportedLanguageNames` resolve exactly
  `{typescript, ts, tsx}`) needed **no edit** and stayed green throughout — it
  drives the real functions, which survive as functions over the registry.
- **Lock B** (`providerFor` agrees with `providerForLanguage`'s resolvable set)
  compile-broke exactly where the deleted `languageProviders` map was named,
  and was fixed by editing **only** its iteration source (to
  `registry.ExposedForSecurity()`); every assertion, including the `.go`/`.py`
  positive probe, is verbatim.
- **Lock C** (`scaffold.Detect` never welcomes a language `scan-all` silently
  refuses) needed no edit and stayed green.
- **C0** (`internal/providers/registry/layering_test.go`) proves the import
  graph by reading `go list -deps`: `internal/providers` (root) never imports
  `registry`/`golang`/`typescript`; `internal/core/surface` never imports
  `internal/providers`; no `internal/core/...` package imports
  `internal/providers/registry`. Mutation-proven: a blank import of `registry`
  added to the root `providers` package produced a real Go import **cycle**
  (registry imports providers; the reverse edge closes the loop), refused by
  the compiler itself before the test even needed to run.
- **C1** (every registered provider declares a non-zero `Capability`) and
  **C2** (`Capability.Surface` is a subset of `surface.ProviderCategories`)
  are driven through the interface with the two real providers.
- **C4** (`Capability().CoverageManifest` agrees with `HandleCoverage`'s type
  assertion) — `HandleCoverage`'s behavior is unchanged; C4 only adds the
  cross-check.
- **C5** (exposure snapshot) freezes the registered state: TypeScript ✓✓✓, Go
  ✗✗✓ (`InitDetect` only). Mutation-proven to move together with Lock A: a
  single field flip (Go's `SecurityScan` to `true`) turns both C5 and Lock A
  red simultaneously.

## What this deliberately does not do

- **Does not wire `"go"` into any resolver** (roadmap P4-1). Go's
  `Exposure.SecurityScan`/`SurfaceTools` stay `false`; only `InitDetect` is
  `true`, unchanged from before this change.
- **Does not build `internal/providers/golang/coverage.go`** (roadmap P1-4b).
  `golang/capability.go` is a new, tested landing site for a per-rule
  declaration — the place P1-4b was blocked on missing — but P1-4b names a
  *coverage manifest* specifically, which this change does not build. P1-4b
  stays open.
- **Does not give Go or TypeScript an enumerable `All()`/`ID()` rule-registry
  loader** beyond what already existed (TypeScript's YAML-backed security
  rules). Control A only fires where `Enumerable: true`; everywhere else it is
  an explicit skip, never a pass.

## Consequences

- Capability and exposure are now two independent, test-checked facts instead
  of five hand-written, unchecked answers to the same question. A future
  provider that forgets `Capability()` fails to compile; one that declares a
  category outside the vocabulary fails C2; one whose `Enumerable: true` rule
  list drifts from its real loader fails Control A.
- `CLAUDE.md`'s layering rule now names `internal/providers/registry` as the
  sole `language → provider` table (amended in the same change this ADR
  documents), dropping the "or the plumbing that resolves..." loophole the
  prior wording left open.
- The registry's `Exposure` field is the only legal path for a future P4-1
  decision to widen Go's reach: flip a bool, and C5 plus Lock A both catch the
  change mechanically — exactly the "one place, checked both ways" property
  this ADR exists to establish.
