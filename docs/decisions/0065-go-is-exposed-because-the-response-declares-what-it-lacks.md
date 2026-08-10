# ADR 0065 — Go is exposed for security scanning because the response declares what it lacks

**Status:** Accepted · **Date:** 2026-08-10 · **Phase:** 3 (roadmap P4-1)

## Context

Roadmap **P4-1** asked the question ADR 0064 deliberately left open: does `"go"` ever
gain an entry in the registry's exposed set, so `codefit-scan-security`,
`codefit-scan-endpoint`, `codefit-scan-all`'s security bucket, and the
`codefit-surface-*` family become real for Go code? ADR 0064 built the mechanism —
`Capability` (what a provider implements) and `Exposure` (which resolvers admit it) as
two independent, checkable facts — and left Go's `Exposure.SecurityScan`/`SurfaceTools`
both `false`, `InitDetect` the only `true`, precisely so this remained a decision, not an
accident.

The measurement that forced the decision: Go's exposure was flipped locally and the real
binary was driven over stdio against a Go project containing a SQL injection.

```
scan-all on a Go project
  score      90         security: {"measured": true}
  findings   1 deterministic  (it found the SQL injection)
  surface    1 item
  endpoint   handler.go   categories: [security, authz]

codefit-coverage for "go"
  ERROR: no coverage manifest for language "go"
```

It works — it found a real defect in real Go code. That is the argument *for* exposing
it. And it is exactly the problem: `surface_items: 1` means *authz was mapped and
nothing else was*. IDOR, over-fetching and N+1 were never looked for, and nothing in
that response said so. An agent reading a 90 with one surface item sees an audited
project. The one tool built to answer "what do you cover for this language?" — the tool
an agent would use to find out — returned an error instead of an answer.

## Decision

**Go is exposed** — `Exposure{SecurityScan: true, SurfaceTools: true, InitDetect: true}`
in `internal/providers/registry` — **and every response that carries mapped surface for
an exposed language now declares what it does not cover.** Exposure without declaration
is a partial audit that reads as a complete one; that is the failure class this decision
exists to forbid, not merely to accept as a known limitation.

This is the concrete form of `docs/specs/audit-protocol.md`'s **I2** (not measured is
never clean) and **I5** (what is not covered is declared), applied to language reach, and
it closes roadmap **P4-1**. The full spec is `docs/specs/declared-partial-language-exposure.md`.

### The rule this establishes

> A language may be exposed only if the response declares what it does not cover for
> that language.

### What was built

- **`surface.DeriveCoverage`** (`internal/core/surface/coverage.go`): computes, for a
  provider's declared `Capability.Surface`, exactly which of the locked
  `surface.ProviderCategories` vocabulary it mapped and which it did not — walking the
  vocabulary slice, never a hardcoded literal. `DeriveCoverageFrom` exposes the
  vocabulary as an explicit parameter so the derivation is provable against a synthetic
  vocabulary in tests, without touching the real, AST-locked const block.
- **`codefit-coverage` answers for every registered, exposed language** (R1):
  `HandleCoverage` now falls back to a manifest DERIVED from `Capability()` —
  security/practices rule ids, prose per mapped category, prose per not-mapped category —
  when no hand-written `CoverageManifest()` exists. `CoverageResponse.Derived` states
  which kind of answer this is. TypeScript's hand-written prose manifest is unchanged and
  stays authoritative; the derived answer is the floor, never a replacement.
- **The scan response declares the surface gap** (R2): `ScanResponse.SurfaceCoverage`
  (scan-security, always present) and `SecuritySection.SurfaceCoverage` (scan-all, present
  whenever security is measured) carry the same `surface.CoverageStatement` — mapped and
  not-mapped categories, machine-readable, plus a prose `Note` (the form that survives
  into an agent's reasoning).
- **The generated skill and `codefit init`'s printed capability line** state the same
  N-of-M reach before a user installs anything, matching README's existing per-dimension
  reach statement shape (`docs/roadmap.md` P1-1c).

### The locks change meaning; they are not deleted

Flipping Go's exposure turned seven pre-existing tests red — three regression locks
(`internal/mcp/language_source_test.go`'s Locks A/B/C) plus four `scan-all` tests in
`internal/mcp/scanall_dbonly_test.go`/`scanall_writegate_test.go` that depended on Go
having no resolvable security provider. That is them working: the boundary they guarded
was crossed on purpose.

- **Locks A/B/C** moved from asserting *"the resolvable set is exactly TypeScript"* to
  asserting it is exactly `{typescript, ts, tsx, go}` — the exposed set, stated
  explicitly, not merely allowed to drift into whatever the table says.
- **The DB-only scan-all tests** (which exist to prove the DB dimension runs
  independently of security provider resolution — the P0-5 invariant) moved their
  fixture language from `"go"` to `"python"` (unregistered), preserving the exact
  "no resolvable provider" scenario they always tested; the guard's own behaviour is
  unchanged.
- **A new lock replaces the guarantee Locks A/B/C used to hold**:
  `TestExposedLanguageDeclaresNonEmptyCapability`
  (`internal/providers/registry/exposure_test.go`) — any language exposed to an analysis
  resolver (`SecurityScan` or `SurfaceTools`) must declare a non-empty `Capability`.
  `TestReplacementLock_ExposedLanguageDeclaresCompleteGap`
  (`internal/mcp/golang_exposure_test.go`) is its response-level half, generic over
  `registry.ExposedForSecurity()`: every exposed language's scan response must state the
  exact complement of its declared `Capability.Surface` against the full vocabulary.

The guarantee these locks held was never "Go stays out" — it was **"nothing is exposed
without being declared."** That guarantee still holds; only the set it is checked against
grew.

### What Go actually declares (not a parity claim)

Measured at `main` @ `810b816`: **6** security rules (`SEC-001, 010, 013, 040, 050,
052`), **1 of 4** surface categories (`authz` only), **4** practices rules, **no** prose
coverage manifest. Exposing Go is not a claim that it matches TypeScript's reach — the
capability statement, the skill, and the coverage answer all say so in the same sentence
that says it is now audited at all.

## What this deliberately does not do

- **No new Go rules or surface categories.** This exposes what exists; growing it is
  separate work, and each addition is a `Capability` declaration change the existing
  controls (C1, C2) already cover.
- **No `internal/providers/golang/coverage.go`.** R1 makes a hand-written prose manifest
  unnecessary for correctness — the derived answer is truthful without it. If one is
  written later, it becomes authoritative over the derived floor, the same relationship
  TypeScript's manifest already has. **Roadmap P1-4b** (`PRAC-004`'s owed manifest entry)
  now has a landing site in the declared rule lists this change ships, and may be taken
  there or left open — this change does not resolve it either way.
- **The `db-`/`dw-` prefix partition residual**, declared in ADR 0064, is untouched.

## Consequences

- `codefit-scan-security`, `codefit-scan-endpoint`, `codefit-scan-all`, and the
  `codefit-surface-*` family now accept `"go"` — a user-visible behaviour change, recorded
  in `CHANGELOG.md`'s `[Unreleased]` with ⚠️.
- Every scan response for an exposed language carries a new `surface_coverage` key
  (`ScanResponse`, `SecuritySection`) it did not carry before. TypeScript's existing
  responses gain the same key; nothing else about them changes (locked field-for-field
  against pre-change goldens captured via `git worktree add --detach 810b816`).
- `codefit-coverage` for `"go"` returns a derived manifest instead of an error; any caller
  that branched on that specific error message for Go must now branch on
  `CoverageResponse.Derived` instead, or on the manifest content directly.
- The registry's `Exposure` field remains the only legal path for a future language to
  widen its reach — this decision demonstrates the mechanism ADR 0064 built working
  exactly as designed: flip a bool, and the capability/response-level controls catch
  whether the declaration kept pace.
