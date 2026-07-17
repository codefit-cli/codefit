# ADR 0024 — DB-012 (never-used index) is PERMANENTLY not covered

**Status:** Accepted · **Date:** 2026-07-17 · **Phase:** 2.2 (DB debt Slice A — coverage honesty)

## Context

The PRD's DB-rule family includes DB-012: flag an index that is never used by any
query, so it can be dropped (it costs write throughput and storage while returning
nothing). It sits next to DB-011 (duplicate/redundant indexes), which codefit DOES
cover (DB-011a exact-duplicate, DB-011b prefix-redundant). The two read as a pair,
so a reader reasonably expects DB-012 to arrive with them. It does not — and the
coverage prose must say WHY, not leave a silent gap (honesty doctrine).

## Decision

### DB-012 is declared PERMANENTLY not covered, not deferred

Detecting an unused index requires **runtime query telemetry** — which indexes
were actually consulted by real traffic (e.g. PostgreSQL's
`pg_stat_user_indexes`). That data exists only inside a live, running database
with accumulated query history. codefit's model is **static**: it reads DDL /
schema text and **never connects to a database** (RF, ADR 0014). So DB-012 is not
unscheduled work — it is **structurally incompatible** with how codefit operates.
Distinguishing "an index no query uses" from "an index no query has used *yet*"
is undecidable from schema text alone; the answer lives in a runtime codefit is
designed never to reach.

This is recorded in the DB coverage source
(`internal/core/dbcoverage/dbcoverage.go`, `NotCovered()`) as **PERMANENT, not
deferred** — deliberately distinct from DB-030/031/040/041 (routine-body rules),
which ARE deferred to a later slice because their blocker is a parser dependency,
not a model boundary. Conflating "permanent" with "deferred" would falsely promise
DB-012 for a future release it can never honestly reach.

### No rule stub, no phantom entry

DB-012 has no file in `internal/core/dbrules/` and no rule constant — the only
reference in the codebase is the coverage prose that explains its permanent
absence. A stubbed rule that never fires would be worse than an honest gap: it
would imply coverage that does not exist (COVERAGE.md must reflect what runs today
on `main`, not aspiration).

## Alternatives considered

- **Defer DB-012 to a later DB slice** — rejected as dishonest: it implies the gap
  is schedulable. No amount of static-parser work closes it; the missing input is
  runtime telemetry, outside codefit's model by design.
- **Add an optional DB-connection mode to read `pg_stat_user_indexes`** — rejected:
  codefit never connects to a database (ADR 0014, MCP-first static model). A
  live-DB mode is a different product, not a rule.
- **A heuristic ("index not referenced in any parsed query")** — rejected: codefit
  parses DDL, not the application's query workload; "not seen in the schema" is not
  "not used at runtime." It would manufacture false positives with confidence.

## Consequences

- `COVERAGE.md` and `dbcoverage.go` carry DB-012 as an explicit PERMANENT gap with
  its WHY, keeping the manifest trustworthy (it discloses what it cannot do).
- The DB-011 family (011a/011b) is complete on its own terms; DB-012's absence is
  documented, not implied by omission.
- Should codefit ever gain a runtime-telemetry dimension (a distinct product
  decision, not on any current roadmap), DB-012 would belong there — never in the
  static DDL rule set.

## Related

- ADR 0014 — neutral DB model; codefit reads schema text, never connects to a DB.
- ADR 0015 — DB rules as pure functions over the neutral schema (static input).
- ADR 0016 — dimension lifecycle and the honesty bar for coverage prose.
