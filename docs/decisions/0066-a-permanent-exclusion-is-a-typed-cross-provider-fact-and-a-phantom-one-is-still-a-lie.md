# 0066 — A permanent exclusion is a typed, cross-provider fact, and a phantom one is still a lie

**Status:** Accepted · **Date:** 2026-08-10 · **Phase:** 3 (roadmap P1-4b) ·
**Builds on [ADR 0056](0056-a-practices-rule-affirms-only-what-it-checked-and-prac-004-is-dropped.md),
[ADR 0057](0057-the-coverage-manifest-answers-for-every-capability-the-prd-promises.md), and
[ADR 0064](0064-language-capability-and-exposure-registry.md)**

## Context

ADR 0056 permanently dropped `PRAC-004` from the Go provider — it asserted a fact
(synchronization was absent) that `go/ast` without `go/types` cannot establish, and the
practices dimension has no probabilistic surface channel to demote an uncertain signal
into (ADR 0055). The drop was recorded in that ADR and in `CHANGELOG.md`. It was never
recorded in the one place an agent actually reads before trusting Go's practices coverage
— `codefit-coverage`'s response. ADR 0065 built the mechanism to derive an honest coverage
answer for any exposed language (`R1`) and named this exact debt as its landing site
without resolving it.

`readme-count-and-prac004-entry` (this change) paid it: `internal/providers/provider.go`
gained `ExcludedRule{ID, Reason}` and `RuleSet.Excluded []ExcludedRule`, plus
`RuleSet.ValidExclusions()` (**C6**) — an excluded id must never also appear in `Declared`.
Go's `Practices` RuleSet now declares `PRAC-004` there, with its ADR 0056 reason, and
`internal/mcp/scan.go`'s `deriveManifest` wires every provider's `Security.Excluded` /
`Practices.Excluded` into `codefit-coverage`'s `NotCovered` list. `apply` shipped this
without an ADR, reasoning that `internal/core/dbcoverage.NotCovered()`'s precedent —
a flat list of permanently-absent rule ids with reasons — already settled the shape
question. `sdd-verify` (obs #1467) judged that reasoning unsound and recommended this ADR
as a disclosed, non-blocking follow-up. This ADR is that follow-up, and it also closes the
sharper defect verify found while checking the reasoning: `ValidExclusions`/C6 has no way
to tell a real exclusion from a fabricated one.

### Why this earns an ADR when `dbcoverage.NotCovered()` did not

`dbcoverage.NotCovered()` is untyped free prose — a `[]string`, no invariant enforced
anywhere, several entries not even naming a single rule id (the PII-coverage boundary, the
dialect-assumptions paragraph). `ExcludedRule` is different in kind, not merely in scope:

- It is a **new first-class type** in the shared `providers` package, not a string.
- It carries a **new cross-provider invariant**, C6, enforced by test for every registered
  provider (`TestCapability_EveryRegisteredProviderDeclaresNonZero`), not read-only prose.
- It is **wired programmatically** into `deriveManifest`'s machine-derived
  `codefit-coverage` response for every exposed language, present and future — a mechanism
  parallel to `Declared` / `Enumerable` / `Surface` / `CoverageManifest`, the exact fields
  ADR 0064 documented as the provider-facing contract.

A mechanism with a cross-provider invariant and a permanent wiring point is the class of
decision this project already writes ADRs for at comparable or smaller size (ADR 0043's
index-form floor, ADR 0045's non-table-relation registry). Citing `NotCovered()`'s shape as
settling this one conflated "a list of strings exists elsewhere" with "this specific typed,
checked, wired mechanism needs no record" — those are not the same claim.

### The phantom-exclusion gap, found by mutation

`ValidExclusions()`/C6 checks exactly one thing: that no id is simultaneously `Declared`
*and* `Excluded`. It says nothing about whether an excluded id ever corresponded to a real
rule. Verify proved the gap by renaming the real `PRAC-004` entry to
`PRAC-999-NEVER-EXISTED` and running the full `internal/providers/...` suite: C6 passed,
every generic capability test passed, and only the one test written specifically to look
for the literal string `"PRAC-004"` failed — for an unrelated reason (it could not find the
string, not because the fabricated id was rejected). A capability declaration can name a
gap that was never real, and every existing mechanical control stayed green while it did.

This is `ADR 0057`'s Control B pointed the other way. Control B forbids the manifest
mentioning a rule id that is not registered, not declared absent, and not delivered
elsewhere — a lie that overstates coverage. A phantom exclusion is the same shape of lie in
the humble direction: it tells an agent about a hole that was never actually there,
understating coverage instead of overstating it. Both are the manifest asserting a fact
about the codebase that does not hold; the direction of the lie does not change that it is
one.

## Decision

### 1. `ExcludedRule` / C6 stand as the typed, cross-provider mechanism this ADR records

No shape change from what shipped: `ExcludedRule{ID, Reason string}`, `RuleSet.Excluded
[]ExcludedRule`, `RuleSet.ValidExclusions() bool` (C6, disjoint from `Declared`), wired into
`deriveManifest`'s `NotCovered` for every provider's `Security`/`Practices` RuleSet. This
ADR is the record ADR 0064's own field list implied but never wrote down for the `Excluded`
half of `RuleSet`.

### 2. The phantom-exclusion check is honest about what it can prove, and for whom

`RuleSet.ValidExclusionSource() (ok bool, phantom []string)` is the new mechanical control
(**C7**). It follows the exact epistemic split `dbcoverage`'s own Control A/B/C draw, keyed
off `RuleSet.Enumerable` — the field that already exists to carry this precise distinction
(ADR 0064: *"whether that list is derivable from a real rule loader... or a hand-maintained
mirror"*):

- **`Enumerable == true`** (TypeScript's YAML-backed security rules today): Control A
  (`typescript/control_a_test.go`) already proves `Declared` is the *exact*, both-directions
  match of the real rule loader's output. Every id in `Declared` therefore shares a
  `<PREFIX>-<N digits>` shape that is itself grounded in that real source. C7 requires every
  `Excluded` id to match that same shape, and names every one that does not.
- **`Enumerable == false`** (Go's hand-written `SEC-`/`PRAC-` literals — no `All()`/`ID()`
  loader exists): `Declared`'s own shape is unverified, so deriving a pattern from it and
  calling the result "checked against the real rule source" would be exactly the
  over-promise this control exists to prevent. C7 returns `(true, nil)` — not applicable, no
  claim made — for this case, the same choice `dbcoverage_test.go`'s Control C makes for
  `internal/core/paradigm/`: *"a rule↔manifest correspondence test is IMPOSSIBLE for this
  root, not merely unwritten — claiming otherwise would be exactly the kind of
  over-promising manifest this whole enforcement effort exists to kill."*

**C7 is a correspondence check, never an accuracy one** — the same limit Control A/B/C state
of themselves. It cannot prove an excluded id was ever actually built or considered by
anyone; a fabricated id that happens to share the family's shape (`SEC-999` instead of
`SEC-999-NEVER-EXISTED`) still passes. What it *does* catch, mechanically and by mutation
proof, is exactly the defect verify found: an id that does not even look like a member of
its family. `PRAC-004`, Go's one real exclusion, is `Enumerable:false`, so C7 makes no claim
about it today — the gap verify found for Go's own exclusion is **not** closed by this ADR;
only the mechanism to close it for a future `Enumerable:true` family with an exclusion is.
That is the honest boundary, stated rather than papered over.

### 3. A permanent exclusion is a coverage fact an agent must be told — this is what ADR 0056 left owed

`PRAC-004`'s drop was true the moment ADR 0056 shipped it. It became *told* — visible to the
one channel an agent actually reads before trusting Go's coverage — only with this change's
`deriveManifest` wiring. The gap between "true in an ADR" and "told in the response" is
exactly the failure class ADR 0057 exists to forbid (*"a gap the manifest does not declare
is a gap the agent will not cover for, because it was never told the gap exists"*), applied
here to a permanent rule drop instead of an unregistered PRD promise. `ExcludedRule` closes
that gap for every current and future provider that declares one.

## Consequences

- `internal/providers/provider.go` gains `RuleSet.ValidExclusionSource()` (C7), wired into
  `TestCapability_EveryRegisteredProviderDeclaresNonZero` alongside C1/C2/C6 for both
  registered providers.
- **The phantom-exclusion gap for `Enumerable:false` families (Go's real, live exclusion)
  stays open.** No `All()`/`ID()` loader exists for Go's hand-written rule lists; building
  one is separate work (and would itself need its own decision about cost versus the value
  of a mechanical check over a two-entry, hand-reviewed list). Recorded in
  `docs/roadmap.md`, not fixed here.
- `TypeScript`'s `Security` RuleSet has no `Excluded` entries today, so C7 is currently
  vacuous in production; it is proven against a fixture RuleSet built `Enumerable:true`
  specifically for the test, using the exact `PRAC-999-NEVER-EXISTED` mutation verify ran.
  The first real `Enumerable:true` exclusion this project ever declares is the first one C7
  actually guards in production.
- `docs/roadmap.md` gains a note pointing at this ADR alongside P1-4b's entry.
