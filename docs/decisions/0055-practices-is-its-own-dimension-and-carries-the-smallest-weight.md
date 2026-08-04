# 0055 — `practices` is a dimension of its own, and it carries the smallest weight

**Status:** accepted · **Date:** 2026-08-04 · **Phase:** 3 (RF-05, thread H1, slice S1) ·
**Supersedes nothing; annotates [ADR 0021](0021-by-dimension-scoring-wired-into-scan-all.md)**

## Context

`scoring.DefaultWeights()` has shipped since Phase 0 with five entries — security 35,
review 20, db 20, complexity 15, tests 10 — while `core/findings` declares **six**
dimensions. `practices` was the one with no weight.

That was not an oversight, it was a pending decision, and ADR 0021 leaned on it: the
`MissingWeights` guard it introduced was illustrated with "a future dimension (e.g.
practices, absent from `DefaultWeights`)", and the test that proved the guard
(`TestMissingWeights_DetectsUnweightedMeasured`) used `practices` as its unweighted
fixture. The moment a practices sensor exists, `Compute` — which iterates the **weights**
map — would drop the whole dimension on the floor: no `by_dimension` entry, no
contribution to the global, no error. The guard exists precisely to make that loud, and
it can only fire if someone remembered to add the weight.

The practices dimension is now being built ([spec](../specs/practices-dimension.md)). Its
weight is decided here, first and alone, because it is the one part of the thread that
touches a number every existing consumer already reads.

## Decision

### 1. security 35 · review 20 · db 20 · complexity 10 · tests 10 · practices 5

Every dimension `core/findings` declares now has a weight, and they still sum to 100.

### 2. practices carries the smallest weight, by doctrine

codefit's rector principle is that it **audits what the developer never sees**. A practices
defect is the opposite of that: `any`, `console.log`, a missing `catch` — these are the
*most* visible things in normal development. A linter underlines them in the editor before
the file is saved.

So the dimension is right to exist (a codebase drowning in them is a real signal, and the
agent that generated the code will not flag its own output), but it is wrong for it to
weigh like `db`, where a missing index is invisible until production. 5 is the honest
number: present in the score, never able to dominate it.

### 3. The 5 points come from `complexity`, which is never measured

They had to come from somewhere — the map sums to 100 by contract, and
`config.Validate` rejects a user-supplied map that does not.

`complexity` is the only dimension whose weight can move with **zero effect on any score
codefit produces today**, because it is post-v1.0 and has no sensor. `Compute` accumulates
`totalWeight` over the **measured** dimensions only, so a weight that is never in the
denominator cannot change a quotient. Taking the 5 points from `security`, `db`, `review`
or `tests` would have been a silent re-scoring of every project.

This is not an argument that a re-balance is free; it is an argument that *this particular*
re-balance is verifiable as a no-op, and it is verified, not assumed:

- `TestRebalance_MovesNoScoreCodefitProducesToday` freezes the pre-re-balance map as a
  literal and asserts an identical `ScoreSummary` — global and every per-dimension value —
  over every measured set `scan-all` can produce today. Mutation-proved by adding
  `complexity` to the measured set, which makes the global move (86 → 84, 97 → 96, 88 → 87)
  and the test fail.
- End to end, the pre-change golden `scanall_prechange_invariant.json` — captured at
  `79e34b0`, before any of this existed — still matches a live `scan-all` in every field.
  The **only** difference in the whole invariant is the new key.

### 4. When complexity lands, it weighs 10, not 15

This is a decision, not a deferral. The order that matters is `complexity > practices`:
algorithmic complexity that scales badly is invisible until the data grows, which is
squarely what codefit is for; a `console.log` is not. 10 keeps that order with room to
spare and keeps the sum at 100 without reopening this file. Whoever builds the complexity
sensor inherits 10 and needs no permission to use it.

## Consequences

- **`by_dimension` grows a sixth key.** Every `scan-all` response now carries
  `"practices": null` — a user-visible response-shape change, and the reason it is in the
  CHANGELOG. `null` is the honest value: the dimension is weighted but has no sensor, and
  ADR 0021's rule stands — an unmeasured dimension is `null`, never a fake `100`.
- **No score moves.** Not the global, not any dimension's value, on any project. See §3.
- **ADR 0021 has one stale line and is NOT rewritten.** ADRs are append-only; 0021 carries
  a superseded-in-scope annotation pointing here.
- **`TestMissingWeights_DetectsUnweightedMeasured` inverted and was rewritten, not
  deleted.** It asserted `MissingWeights([practices]) == [practices]`, which this change
  makes false. It now runs against a dimension name that genuinely has no weight, mixed
  with two that do, so the guard still has to distinguish rather than answer. Mutation-
  proved in both directions: a `MissingWeights` that reports nothing fails it, and one that
  reports everything fails it *and* `TestMissingWeights_AllWeighted`.
- **A new lock replaces what the old test used to imply.**
  `TestDefaultWeights_CoversEveryDeclaredDimension` states the invariant positively: a
  dimension declared in `core/findings` without a weight fails the suite. That is now the
  thing standing between a future dimension and a silently dropped score — the role
  `practices` used to play by being absent.
- **The sum-to-100 claim is now tested.** `DefaultWeights`'s doc comment has always said
  it; `TestDefaultWeights_SumIsExactly100` is the first thing to check it.

### Declared limits, stated rather than passed over

- **`cfg.Report.ScoreWeights` does nothing.** It is a documented `.codefit.yaml` knob
  (PRD §config sketch), it is parsed into `config.Report`, and `config.Validate` rejects it
  when it does not sum to 100 — and **nothing ever reads it**. `scoring.DefaultWeights()` is
  hardcoded at both call sites in `internal/mcp/scanall.go` (the `MissingWeights` guard and
  `Compute`), and grep over the repository finds no other reference to the field than its
  declaration and its validation. A user who re-weights their audit today gets their map
  validated and then ignored. That is a pre-existing lie in the config surface; it predates
  this change and is **not fixed here** — fixing it means deciding what a partial user map
  means, whether an unweighted dimension may be dropped by hand, and how it interacts with
  the guard above. It gets a declared limit, not a silent pass.
- **The PRD still reads `complexity: 15`**, in both the defaults sentence (§RF-07) and the
  `.codefit.yaml` sketch. The PRD is explicitly exempt from the reflect-today rule
  (`CLAUDE.md` § Mapa documental), so this is **recorded, not corrected**.
- **No rule changed.** No finding, surface item or baseline fingerprint moves; `COVERAGE.md`,
  `internal/core/dbcoverage/` and the per-language `coverage.go` manifests are untouched.
  This ADR changes one map of integers.

## Related

- [ADR 0016](0016-dimension-lifecycle-standalone-then-wired-to-scan-all.md) — the dimension
  lifecycle this thread follows; `by_dimension` is part of the close.
- [ADR 0021](0021-by-dimension-scoring-wired-into-scan-all.md) — `by_dimension` wired into
  `scan-all`, the `measured ⊆ weights` guard, and the `null`-not-`100` rule.
- [spec — the practices dimension](../specs/practices-dimension.md) — R6 and slice S1.
