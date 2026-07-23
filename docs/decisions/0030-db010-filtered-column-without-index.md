# ADR 0030 — DB-010 (filtered column without a covering index) is SURFACE, not a deterministic finding

**Status:** Accepted · **Date:** 2026-07-22 · **Phase:** 2 (index-vs-query — first cross rule, on ADR 0029 infra)

## Context

DB-010 is the first rule of the code↔schema cross whose infrastructure ADR 0029
landed: it flags a column the CODE filters by (a query `where`) that the SCHEMA
does not index. It reuses `reconcile` (the exact-match certainty gate) and the
shared leftmost-prefix coverage extracted to `core/db`
(`db.IndexLike`/`db.CoveredByPrefix`, ADR 0015 — one definition, no drift with
DB-001).

Two things had to be decided, not left implicit:

1. **Deterministic finding vs surface** — what DB-010 emits on an exact reconcile
   with no covering index, and what it emits when reconcile abstains.
2. **Single-column vs composite** — how a multi-column filter is treated.

ADR 0029 §"Certainty is EARNED" left a *tentative* lean toward
"deterministic-on-resolved". Implementing DB-010 against the existing DB dimension
settled it the other way.

## Decision

### DB-010 emits SURFACE; it abstains (emits nothing) when reconcile does not match

When reconcile matches exactly and no index-like list (an index, a unique
constraint, or the primary key) covers the filtered column as a leading prefix,
DB-010 emits a **surface item** (`db-filtered-column-no-index`), never a
deterministic finding. When reconcile abstains — unknown model, column not a real
column, cross-naming-space (a logical field vs a physical SQL-DDL column) — DB-010
emits **nothing** (the Complete==false floor, ADR 0025/0029).

**Why surface, in one line:** the structural fact (filtered-and-uncovered) is
certain once reconcile matches, but whether a missing index MATTERS depends on the
column's cardinality, the table's size and the write load — the same agent judgment
DB-001 (FK without index) already defers as surface (ADR 0004/0005). This revises
ADR 0029's tentative "deterministic-on-resolved" lean: consistency with DB-001, its
direct sibling on index coverage, wins.

**Surface does not loosen the reconciliation floor.** Emitting surface rather than a
1.0 finding does NOT license a softer match: reconcile stays EXACT (single table by
name, real columns, abstain otherwise). A surface item that points at a column that
does not exist — or the wrong table — trains the agent to distrust and ignore the
channel, which is worse than emitting nothing. The honesty floor is the same for
surface as for a deterministic finding; only the "does it matter" judgment is
deferred, never the "is this real" one.

### DB-010 is the single-column rule; the item anchors to the schema

Each filtered column is evaluated on its own by the leftmost-prefix rule: a
composite index `[a,b]` covers a lookup on `a` (emit nothing) but NOT on `b` (emit)
— the same index covers one column and not the other, by position. A multi-column
`where` (a AND b) is therefore evaluated per column, surfacing each uncovered one.
The composite-INDEX case — a query filtering `a,b` together with no composite index
covering the pair — is **DB-013, the next slice**, not DB-010.

The item anchors to the SCHEMA column (file:line in the schema), consistent with
DB-001 and the whole DB dimension: the fix is a schema change (add an index), and
the filtering query is context in the signals. It is deduplicated by (table,
column): two queries filtering the same unindexed column are one concern, reported
once.

## Traps locked (tests)

- **PK / unique / composite-leftmost** — a rule looking only at explicit `@@index`
  would false-positive on every id (PK) and every unique column. DB-010 treats the
  PK as an implicit index and includes unique constraints (via `db.IndexLike`), and
  honors the leftmost-prefix rule for composites. Each is a locked test
  (`TestDB010_PrimaryKeyCovers`, `TestDB010_UniqueCovers`,
  `TestDB010_CompositeLeftmostIsTheHeart`).
- **Stamping** — the cross output is produced AFTER the schema-only sensor, so it
  carries no baseline fingerprint until the adapter stamps it (`dbsensor.StampSurface`
  over the exposed schema content). `TestDB010_SurfacesEndToEnd` proves DB-010
  surfaces in the production db result WITH a non-empty fingerprint — without it,
  `observedFrom` would silently drop the item.

## Limits (carried from ADR 0029, still in force)

- OR / NOT and nested relation filters are skipped at extraction / dropped at
  reconcile.
- Cross-naming-space (a Prisma-field query against a physical-column SQL-DDL schema)
  abstains by design — field→column resolution is deferred to a non-Prisma
  extractor in its own provider.

## Related

- ADR 0029 — code↔schema cross infrastructure (reconcile, neutral query model,
  seam); this revises its tentative deterministic lean for DB-010.
- ADR 0004 / 0005 — deterministic rule vs mapped surface (map the fact, the agent
  judges).
- ADR 0015 — DB rules over the neutral model; the leftmost-prefix coverage shared
  in `core/db` so DB-001 and DB-010 never drift.
