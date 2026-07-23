# ADR 0031 — DB-013 (multi-column filter without a composite index), and its precedence with DB-010

**Status:** Accepted · **Date:** 2026-07-22 · **Phase:** 2 (index-vs-query — RF-03 DB-013)

## Context

DB-013 is the second rule of the code↔schema cross (ADR 0029): a query that filters
by MULTIPLE columns of one table together (`WHERE a=? AND b=?`) that no composite
index covers. It is the multi-column sibling of DB-010 (single filtered column
without an index, ADR 0030), and it forces two decisions DB-010 did not: how it
divides work with DB-010, and what "covered by a composite index" means.

## Decision

### Precedence with DB-010: partition by filter arity, no cross-rule suppression

A single query filtering `a AND b` where only `a` is indexed sits between the two
rules: DB-010, evaluating per column, would emit on the uncovered `b`; DB-013 would
emit on the pair `(a,b)`. The dedup is per column for DB-010 and per set for DB-013,
so they would NOT merge — two surface items for one fix (`@@index([a,b])`).

**DB-010 is narrowed to single-column filters; a multi-column filter is deferred
WHOLE to DB-013** (relevamiento option b). Each reconciled filter routes to exactly
ONE rule by its real (post-reconcile) column count: one column → DB-010, two or more
→ DB-013.

Why this over letting DB-013 suppress DB-010's items (option a): for a multi-column
WHERE the right fix is a COMPOSITE index, not a standalone index per column — so
DB-010's per-column emission would suggest the wrong fix (`@@index([b])`) for an
`(a,b)` query. Routing at the source gives each rule only the filters it can advise
correctly, and keeps the rules INDEPENDENT: neither inspects the other's output, so
there is no ordering dependency in `All()` (option a would break rule independence).
A column the code also filters on its own arrives as its own single-column filter
and is still caught by DB-010.

Locked by `TestDB010_DefersMultiColumnFiltersToDB013`,
`TestDB010_OverlapCaseGoesWholeToDB013`, `TestDB010_SingleColumnFilterStillCaught`,
and `TestDB013_SingleColumnFilterIsDB010s`.

### Coverage is ORDER-INSENSITIVE — the leading SET, not the leading sequence

A `WHERE a=? AND b=?` is order-insensitive: a composite index on `(a,b)` OR `(b,a)`
serves it — both have `{a,b}` as their leading columns. So DB-013 uses
`db.CoveredBySetPrefix` (the first *n* columns of an index, as a SET, equal the
filtered set), NOT the ordered `db.CoveredByPrefix` DB-010/DB-001 use. The four trap
cases:

| index | filter | covered? | why |
|---|---|---|---|
| `[a,b,c]` | `(a,b)` | yes | filtered set is the leading 2 |
| `[a,b,c]` | `(a,c)` | **no** | `c` is 3rd — `b` breaks the leading run |
| `[b,a]` | `(a,b)` | yes | same leading set, order-insensitive |
| `[a]` | `(a,b)` | **no** | index shorter than the pair |

This deliberately differs from DB-001/DB-010's ordered prefix: an equality set does
not respect declared column order, a FK / single-column lookup does. codefit does
NOT capture whether a filter is equality or range (a declared limit) — so it takes
the columns as an unordered SET and states, in the signal, that column order in the
index matters for range filters and is the agent's judgment. Locked by
`TestDB013_OrderInsensitiveIsTheHeart` and `db.TestCoveredBySetPrefix`.

### DB-013 is SURFACE, schema-anchored, deduped by column set

Same as DB-010 (ADR 0030): SURFACE not a deterministic finding — whether a missing
composite index matters depends on cardinality/size/write-load, the agent's
judgment. It abstains (emits nothing) when reconcile does not match exactly — the
same non-negotiable reconciliation floor: surface does not license a softer match.
The item anchors to the SCHEMA table (the fix, `@@index([a,b])`, is a table-level
change) and is deduplicated by (table, column SET) so `(a,b)` and `(b,a)` are one
concern. Reuses `reconcile` and `core/db` coverage — no reimplementation.

**Superseded for grouped items (ADR 0032, FIX 4):** the same column set recurring
across many models is grouped into ONE item, so the anchor is no longer "the table"
but the ALPHABETICALLY-FIRST affected model, with a per-set snippet driving a stable
baseline fingerprint. The single-table anchor above still holds for a set that
occurs in exactly one model. See ADR 0032 for the determinism guarantee.

## Declared limits (carried from ADR 0029, still in force)

- OR / NOT and nested relation filters are skipped at extraction.
- Cross-naming-space (a logical Prisma field vs a physical SQL-DDL column) abstains.
- **Equality-vs-range is not captured** (locked by
  `TestDB013_RangePredicateIsAKnownLimit`): set-cover is correct only when every
  predicate is equality. With `WHERE b>2 AND a=1`, the range on `b` cuts the index
  there — `[b,a]` no longer seeks `a`, so it does NOT actually cover the pair — yet
  set-cover reports "covered". The root cause is the neutral model:
  `prismaWhereColumns` extracts column NAMES, not operators, so DB-013 receives
  `where:{b:x}` and `where:{b:{gt:x}}` identically. **Resolving this requires adding
  the operator (equality vs range) to the neutral `QueryFilter` — a separate
  slice**, not a fix here. The direction of the error is the SAFE one: set-cover is
  more permissive than ordered-cover, so the failure mode is a false NEGATIVE (a
  missed composite-index suggestion), never a false positive (a wrong one) — the
  Complete==false floor holds. codefit names the column set and states, in the
  signal, that index column order matters for range filters and is the agent's
  judgment.
- **Cross-table (join) filters are out of scope**: a QueryFilter names one model;
  a filter spanning tables is not modeled this slice.

## Related

- ADR 0029 — cross infrastructure (neutral QueryFilter, reconcile, the seam).
- ADR 0030 — DB-010 (single-column), surface-not-finding, the reconciliation floor.
- ADR 0015 — coverage logic lives in the core (`core/db`), shared, never drifting.
- ADR 0004 / 0005 — deterministic fact vs mapped surface (why "does it matter" is
  the agent's).
