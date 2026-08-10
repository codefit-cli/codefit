# ADR 0063 — Materialized-view refresh (DW-022 / DB-022) is reframed as surface, not confirmed as a permanent exclusion

**Status:** Accepted · **Date:** 2026-08-10 · **Phase:** 3 (roadmap P1-4a, P4-3)

**This ADR is the one DW-022 owed.** `VERSIONING.md`'s `v0.2.5` entry says, in its own
words, that DW-022 was "permanently dropped" and that its ADR was "still owed, unlike the
structurally identical DB-012 exclusion (ADR 0024)." This document pays that debt — but it
pays it by **reversing** the exclusion it was expected to confirm, per the architect's
decision recorded in `docs/roadmap.md` P4-3 (2026-08-04).

## Context

`DW-022` (a materialized view with no refresh mechanism, in an OLAP/data-warehouse schema)
and `DB-022` (the same question on the OLTP side) were both evaluated during Phase 2.5 and
dropped on the same reasoning as `DB-012` (`never-used index`, [ADR 0024](0024-db-012-never-used-index-permanently-not-covered.md)):
refresh cadence lives in scheduler state — a cron entry, a CI pipeline, an application job —
that static DDL does not carry, so codefit cannot **affirm** that a materialized view is
stale. That reasoning was recorded as a **permanent** exclusion, the same footing as
`DB-012`: not deferred, not schedulable, structurally incompatible with a parser that only
ever reads DDL text.

The architect re-examined that call (roadmap P4-3, 2026-08-04) and found it **half right and
half wrong**.

## Decision

### The reasoning is right about affirmations and wrong about surface

`DB-012`'s unreachability is total: nothing about "is this index ever queried" is knowable
from schema text, full stop — there is no smaller claim codefit could make instead. A
materialized view's refresh question is different in shape. codefit genuinely cannot
**affirm** "this view is stale" — that conclusion needs the scheduler, and codefit never
connects to one. But codefit **can** establish a smaller, true fact from the DDL alone: *this
schema declares N materialized views, and their freshness depends on a scheduler outside
what codefit reads.* That is not an affirmation about staleness — it is an honest,
proven-from-what-was-read enumeration, exactly the shape the PRD's division of labour
assigns to **surface** (§10): codefit maps what it can prove and hands the judgment to the
agent, which can read the cron, the migrations, the CI pipeline and the application code —
none of which codefit ever sees.

`DB-012` has no equivalent smaller claim to fall back to (an index either gets read by the
query planner over live traffic or it does not; there is no static-DDL-provable subset of
that question), which is why this ADR reverses DW-022/DB-022 and leaves DB-012 exactly as
[ADR 0024](0024-db-012-never-used-index-permanently-not-covered.md) left it — the two are
no longer on the same footing, and the "same DB-012 lineage" language in `VERSIONING.md` and
`dbcoverage.go` is superseded by this document, not deleted (append-only, per this project's
documentation doctrine).

### DB-022 (OLTP) takes the identical reversal

The reasoning above does not depend on OLAP classification — a materialized view's refresh
cadence lives outside the DDL whether the schema is transactional or analytic. `DB-022` (the
OLTP-side rule id for the same question) is reversed on the same grounds, in the same ADR,
rather than carrying a second, later reversal — there is no argument that distinguishes the
two paradigms here.

### The future rule is a schema-level census, not a per-view affirmation

Following `DW-005`/`DW-011`/`DW-020` (and the reasoning that already governs them, §
`internal/core/dwrules`): the eventual rule emits **at most one item per schema**, carrying
the list of materialized views the schema declares, never one item per view. A schema with
forty materialized views must not produce forty items — the point of a census is to let the
agent see the whole surface in one place, not to flood the response with N near-identical
findings that all resolve to the same underlying question ("does this schema's refresh
tooling exist, and does it cover everything it declares as materialized").

### This is decided, not built

Nothing in `internal/core/dwrules` or `internal/core/dbrules` changes in this ADR, and
nothing should until the parser floor below lands:

- **`db.View` (`internal/core/db/db.go`) cannot say a view is materialized.** It carries
  exactly `Name`, `Pos` and `Body` — verified directly against the struct definition, not
  assumed. There is no boolean, no flag, no separate `MaterializedView` type. Both PostgreSQL
  (`CREATE MATERIALIZED VIEW`) and T-SQL (indexed views, a related but distinct mechanism)
  express "materialized" as a property the current model has nowhere to put.
- This is the same shape of prerequisite `DW-021` (`Index.Method`) and `DW-020`
  (`Table.Partitioning`) each needed before their own rules could exist — a parser floor
  first, a rule second. Building a rule against a model that cannot express the concept it
  needs to reason about would either silently misclassify every materialized view as
  ordinary, or require the rule to re-derive "materialized" from `Body` text itself, outside
  the neutral model, which this project's layering (`core` owns the model, `dbrules` reasons
  over it) forbids.
- No rule stub is added. Per [ADR 0024](0024-db-012-never-used-index-permanently-not-covered.md)'s
  own precedent, a stubbed rule that never fires would imply coverage that does not exist.

## Alternatives considered

- **Leave DW-022/DB-022 as permanent exclusions and only pay the "owed ADR" debt by writing
  one that confirms the original call.** Rejected: it is the call the architect explicitly
  revisited and found incomplete (right about affirmations, silent about surface), and
  writing an ADR that re-confirms a call already known to be half-wrong would just relocate
  the same gap into a newly-dated document.
- **Build the parser floor and the census rule in this same change.** Rejected as scope
  creep: this ADR's job is the decision record the roadmap already named as owed (P1-4a); the
  parser floor and the rule are their own slice of work, sized like what `DW-020`/`DW-021`
  each took on their own.
- **Route this through the routine-body-style "surface candidate" bullets already in
  `dbcoverage.go` (`DB-021`, `DB-032`, `DB-101`, `DB-102`) without a formal ADR.** Rejected:
  those bullets describe rules that were *always* surface candidates from the start. DW-022/
  DB-022 were recorded as a **permanent exclusion**, and reversing a recorded permanent
  exclusion is exactly the class of change `VERSIONING.md` itself said needed its own ADR.

## Consequences

- `VERSIONING.md`'s `v0.2.5` entry and `internal/core/dbcoverage/dbcoverage.go`'s DW-0xx
  `Reasoning()` paragraph both carry an append-only superseding note pointing here, rather
  than being rewritten to erase the original call — the original reasoning stays legible as
  the record of what was decided in Phase 2.5 and why it changed.
- `COVERAGE.md`'s `DB-022` bullet, which already anticipated this reframing (`decided but not
  built`, pointing at roadmap P4-3) before this ADR existed, now also cites this ADR by
  number; its DW-0xx paragraph gets the same append-only note as `dbcoverage.go`.
- **No rule, finding, surface item or baseline fingerprint changes.** `dwrules.All()` stays
  seven rules; `dbrules.All()` stays fourteen. This is a decision record, not an
  implementation — the parser floor (`db.View`'s materialized flag) and the census rule
  itself remain open work, tracked in `docs/roadmap.md`.
- `internal/providers/golang/coverage.go` (P1-3/P1-4b, a separate and still-open decision)
  is untouched by this ADR — the OLTP/OLAP reversal here is orthogonal to whether Go ever
  gets a coverage manifest.

## Related

- [ADR 0016](0016-dimension-lifecycle-standalone-then-wired-to-scan-all.md) — dimension
  lifecycle and the honesty bar for coverage prose.
- [ADR 0024](0024-db-012-never-used-index-permanently-not-covered.md) — the genuinely
  permanent exclusion this ADR distinguishes DW-022/DB-022 from.
- [ADR 0017](0017-name-heuristic-db-rules-as-pure-surface.md) — the affirmation/surface
  boundary this decision applies to a new rule pair.
- `docs/roadmap.md` P4-3 — the architect's original decision this ADR formally records.
