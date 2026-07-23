# ADR 0032 — Cross noise correction: unique-subset, low-cardinality, high-arity, and pattern grouping

**Status:** Accepted · **Date:** 2026-07-23 · **Phase:** 2 (index-vs-query — dogfood-driven correction)

## Context

Dogfooding the code↔schema cross (DB-010/DB-013) against real Prisma schemas
exposed what the hand-built, table-driven tests could not — signal-to-noise on
volume. On a real production schema (~40 models) the cross produced 77 of 120 DB
surface items (71 DB-013 alone), dominating a channel whose attention budget is
shared with DB-001. Reading the `.prisma`, a large fraction were clearly wrong or
redundant. Two clean projects (a RealWorld Prisma app) correctly produced ZERO —
so the rules are not broken, but their emission on real schemas was noisy.

The surface channel is only trustworthy if its items are load-bearing. Four
corrections, all either CORRECTNESS or principled ABSTENTION — none an arbitrary
threshold — restore that.

## Decision

### FIX 1 — Unique-subset short-circuit (correctness, `core/db`, DB-010 + DB-013)

A filter whose column set CONTAINS a unique key (the primary key, a `@unique`
column, or a `@@unique` composite) resolves to at most one row — so NO index is
missing, the lookup is already a single-row seek. `db.CoveredByUniqueSubset` fires
when some unique key's columns are a SUBSET of the filtered set. One rule covers all
three cases; it does not over-kill (a `@@unique([a,b])` is not a subset of `(a,c)`,
so that filter still emits).

This kills the dominant false positive: the multi-tenant `findFirst({ where: { id,
salonId } })` pattern (fetch by id, scoped to the tenant) — a legitimate security
idiom that DB-013 was flagging as "add a composite index on (id, salonId)". The PK
on `id` already makes it a single-row lookup.

It is ROBUST to the equality-vs-range limit (ADR 0031): even a range predicate on
the PK still uses the PK index for a bounded seek, so the short-circuit introduces
no new false negative.

### FIX 2 — Low cardinality from the SCHEMA type (DB-010 only)

DB-010 skips a filtered column whose DECLARED type is bounded: `Boolean` (2 values)
or an `enum` (a declared, finite value set). Indexing such a column standalone is
almost always wrong, and codefit knows it from the `.prisma` type without seeing a
row — this is schema information it was ignoring, not a heuristic on data.

Decision: skip ALL enums, not by value count, because the neutral `db.Schema` does
not carry enum VALUES (the Prisma parser collects enum names only). An enum is
low-cardinality by construction; refining by value count would require extending the
neutral model with `Schema.Enums`/values — a separate slice. DB-013 does NOT apply
this: a Boolean/enum as part of a composite filter is legitimate.

**Declared limit — String-used-as-enum** (locked by
`TestDB010_StringUsedAsEnumIsAKnownLimit`): FIX 2 keys off the DECLARED type, so a
column declared `String` yet used as a categorical (e.g. `Transaction.type` holding
only `'income'/'expense'`, the values living in a code comment) is
indistinguishable at the schema level from a high-cardinality String — DB-010 still
emits on it. This is a known blind spot, safe in direction (noise the agent refutes
by reading the schema, never a false affirmation), the same lineage as the range
predicate (ADR 0031). Resolving it requires the neutral `QueryFilter` to carry the
WHERE's LITERAL VALUES so cardinality can be inferred from usage — a separate slice,
not a fix here.

### FIX 3 — High arity is abstention, not a magic cap (DB-013)

With a filter of 4 or more columns, WHICH subset to index depends on selectivity
codefit cannot see — so it does not know, and stays silent (`arityAbstainThreshold
= 4`: emit only for 2- or 3-column filters). A `@@index([a,b,c,d])` is a guess with
an item's format, not an actionable suggestion. This is the same honesty floor as
cross-naming-space and the range limit — an abstention, phrased as one. Real
composite indexes rarely exceed three columns; the one-line justification is that
beyond three the useful subset is unknowable from structure alone.

### FIX 4 — Group a repeated pattern across models (DB-013)

The `(salonId, deletedAt)` set (tenant scoping + soft-delete) recurred across ~15
models. That is ONE architectural observation, not 15 findings; from the second it
adds nothing and burns attention budget. DB-013 now GROUPS emitted items by their
column SET across tables into one item per unique set, naming every affected model
in an `affected_models` signal. Grouped, NOT suppressed — the full model list is in
the item.

Everything about the grouped item is DETERMINISTIC, so an unchanged tree never
churns the baseline (which would make the agent see a "new" item every scan — worse
than the noise removed): the items are emitted in sorted column-set order; within a
group the affected models are sorted by name; the anchor is the ALPHABETICALLY-FIRST
model (a clickable start, never "first filter seen" — filter-collection order must
not leak in); and the snippet is set explicitly to the sorted column set. So the
baseline fingerprint (`category + schema-file + snippet`) is distinct-per-set and
stable regardless of which model anchors it — DB surface is baseline-tracked by
fingerprint, not the StableID confirm flow. Locked by
`TestDB013_GroupedOutputIsDeterministic` (two runs, filters in opposite order →
byte-identical items). This also fixed a latent collision where two sets on one
table shared the model line's snippet.

## Consequences

- `core/db`: `UniqueKeys` + `CoveredByUniqueSubset` join `IndexLike` /
  `CoveredByOrderedPrefix` / `CoveredBySetPrefix` as the shared, no-drift coverage
  vocabulary.
- No prior test encoded the noisy behavior, so none had to be loosened — the fixes
  only tighten what emits. New tests lock each fix, including negative controls
  (unique-subset must not over-kill; String columns still emit; 2–3 column filters
  still emit; distinct sets stay separate).
- The seam byte-identical gate and layering (core imports neither providers nor
  sensors) stay green.
- Re-measurement of the dogfood decides whether gating is still needed — deferred
  until the numbers are clean.

## Related

- ADR 0029 — cross infrastructure. ADR 0030 — DB-010 (surface, reconciliation
  floor). ADR 0031 — DB-013 (set coverage, precedence, range limit).
- ADR 0004 / 0005 — deterministic fact vs mapped surface; abstain over a false
  green (the floor FIX 3 extends).
- ADR 0015 — coverage logic lives in `core/db`, shared, never drifting.
