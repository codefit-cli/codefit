# ADR 0021 — Turn on by_dimension: scoring.Compute wired into scan-all

**Status:** Accepted · **Date:** 2026-07-05 · **Phase:** 2 (DB dimension — close: per-dimension scoring)

**Superseded in scope (2026-08-04) by
[ADR 0055](0055-practices-is-its-own-dimension-and-carries-the-smallest-weight.md).**
Every DECISION below is untouched and still holds — Compute in scan-all, the
always-present score, `null` for a dimension that is not audited, affirmations-only
scoring, the `measured ⊆ weights` guard, and the raw-findings (non-baseline-aware)
score. What is no longer true is one ILLUSTRATION of the guard, and this ADR is not
rewritten — read the following together with 0055:

- **"the guard protects a future dimension (e.g. practices, absent from
  DefaultWeights)" is FALSE.** `practices` now has a weight (5, taken from
  `complexity`), and every dimension `core/findings` declares is weighted. The guard
  itself is unchanged and still protects the next dimension declared without one;
  only the example is spent.
- **`by_dimension` carries SIX keys, not five.** The "review / complexity / tests
  null" sentence is now "review / complexity / practices / tests null". The shape
  rule it states is exactly the one 0055 follows: weighted-but-unmeasured is `null`,
  never a fake `100`.
- **The test that proved the guard was rewritten.**
  `TestMissingWeights_DetectsUnweightedMeasured` used `practices` as its unweighted
  fixture and inverted the moment `practices` gained a weight. It now runs against a
  dimension name that genuinely has none — see 0055 §Consequences.

The no-value-regression consequence — "security-only global equals today's flat
security score exactly (secScore*35/35)" — still holds: `security` weighs 35 before
and after, and 0055 verifies the whole re-balance moves no score.

## Context

`scoring.Compute` (per-dimension scores plus a weighted global) has existed since
Phase 0 but never ran in production — the MCP tools return a flat `DimensionScore`
int, and scan-all had no score at all. With security and db both running in
scan-all (ADR 0020), the last piece of the db dimension's close is to show each
dimension's score beside the global — the `by_dimension` mechanism of ADR 0016.
This is the final Phase-2 slice.

## Decision

### Compute runs in scan-all over the measured dimensions

`HandleScanAll` calls `scoring.Compute(measured, scored, DefaultWeights())`.
`measured` is security (always) plus db (only when it measured). `scored` is the
union of both sensors' findings — **RAW, not baseline-filtered** — so the global
matches the existing flat score exactly (no value regression). The result is a
`ScoreSummary` always present on `ScanAllResponse`.

### The score is always present; by_dimension declares what is not audited

`ByDimension` carries every weighted dimension: security and db populated (db null
when it did not run), and review / complexity / tests null — an honest statement
that those dimensions exist but are not audited yet. This is a deliberate,
informative shape, not an omission. Adding an always-present `score` object is a
response-shape change; the DB *section* (`DB *DBSection`) stays omitted when there
is no database, so a database-less project's non-score fields are unchanged.

### The score reflects affirmations, not surface

A dimension's score is 100 minus the penalties of its deterministic FINDINGS;
mapped surface (questions the agent reasons) does not penalize until confirmed. So
a project with much unresolved db surface (un-indexed FKs, …) can still show a high
db score — the score measures affirmed defects, not open questions. Consistent with
security (surface scores only after confirm-surface). Declared so the number is not
misread.

### A guard for the measured ⊆ weights contract

Compute iterates the weights map, so a dimension that is measured but has no weight
would be silently dropped (no by_dimension entry, no global contribution).
`scoring.MissingWeights` is run before Compute; a measured dimension without a
weight is a codefit wiring bug and surfaces as an explicit error from scan-all,
never a silently incomplete score. Today security and db both have weights; the
guard protects a future dimension (e.g. practices, absent from DefaultWeights).

### The score is over raw findings — baseline-aware scoring is deferred

The score does NOT respect baseline acknowledgment: a finding acknowledged in the
baseline still penalizes the score, because `scored` is the raw sensor findings.
This matches the existing flat scores exactly (the no-regression requirement pins
it) but means an accepted false-positive still lowers the number. A baseline-aware
score (subtracting acknowledged findings) is a declared scoring debt for a future
slice, not built here.

## Consequences

- `scoring.Compute` leaves the dead path; scan-all reports `global` + `by_dimension`.
- No value regression: security-only global equals today's flat security score
  exactly (secScore*35/35).
- The response gains an always-present `score` object (an intentional shape change).
- The "affirmations only" semantics, the measured⊆weights guard, and the
  raw-findings (non-baseline-aware) scoring are declared and tested.
- **This closes Phase 2**: the db dimension is built, dogfooded, wired into scan-all,
  and scored beside security.

## Related
- ADR 0016 — dimension lifecycle (by_dimension is part of the close/DoD).
- ADR 0020 — scan-all multi-dimension (security + db running, which this scores).
