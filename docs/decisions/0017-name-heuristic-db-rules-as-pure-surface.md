# ADR 0017 — Name-heuristic DB rules as pure surface, engineered against noise

**Status:** Accepted · **Date:** 2026-07-02 · **Phase:** 2 (DB sensor — name-heuristic rules, slice 2b)

## Context

Slice 2 (ADR 0015) added structural DB rules over the neutral schema, split into
one affirmation (DB-050, a table with no primary key — undeniable) and three
surface rules. Slice 2b adds four rules that read MEANING from column names:
DB-051 (a foreign key typed as text instead of a numeric/uuid id), DB-052 (missing
audit timestamps), DB-053 (a sensitive-looking column stored in the clear), DB-003
(repeating groups like phone1/phone2, a 1NF smell).

Guessing meaning from a name is intrinsically noisy: a `password` may already be
hashed, a `token` may be public, `phone1/phone2` may be deliberate. So none of
these may ever be an affirmation.

## Decision

### All four are pure SURFACE — never an affirmation

Per the project rule (nothing that depends on context/intent is affirmed), every
name-heuristic rule emits SurfaceItems only, never a deterministic Finding. This
extends ADR 0004's boundary: DB-050 is conclusive over the schema (affirmation); a
name-derived guess needs judgment codefit will not fake (surface). A false green is
worse than an honest red (ADR 0005).

### Surface is not an excuse for noise — three anti-FP levers

1. **Type filter.** A suspicious name on the wrong column type is almost never the
   problem (`passwordChangedAt DateTime`, `passwordResetCount Int` are not secret
   stores). Filtering by type kills most false positives before an item is emitted.
   This is a judgment about FORM, not intent, so it is legitimate.
2. **Counter-signal EXPOSED as a fact, never suppressing.** A name carrying an
   encryption hint (`hash`/`hashed`/`encrypted`/`enc`/`digest`) is reported as the
   fact `encryption_hint_in_name`; it does **not** stop the finding from being
   emitted (DB-053). A name is not proof of encryption, so suppressing on it would
   be a silent false negative in data security — exactly the false green ADR 0005
   forbids. The agent sees the hint and dismisses instantly; codefit does not decide
   for it.
3. **Fact, not judgment.** The rule exposes "column X, type Y, token Z matched" and
   the agent concludes. The rule never states that something is sensitive/insecure.

Name matching is case-insensitive and by NAME COMPONENT (camelCase/snake_case
tokenized), never raw substring. The token/hint sets live in the core beside each
rule. They are NOT configurable now (YAGNI); a future per-project config may ADD
tokens (never replace the core set) — noted, not built.

### DB-051 is a structural type-mismatch check, not a name guess

The high-FP trap would be "fire on every String FK" (every uuid/cuid FK). DB-051
avoids it: it fires only on a TYPE MISMATCH — a String/Text FK whose referenced key
(resolved by crossing RefTable/RefColumns against the schema) is numeric — or on an
unbounded `@db.Text` key. A String FK to a String uuid PK does not fire; an
unresolvable reference does not fire. This makes DB-051 the LOWEST-FP of the four,
not the highest.

### DB-052 fires on both timestamps missing; "only one" is deferred

DB-052 fires only when a table has NEITHER `createdAt` NOR `updatedAt` (the clearest
smell). The case where exactly one of the pair is missing is a DEFERRED CANDIDATE
(DB-052b), to be decided WITH real dogfood volume in front of us — it is **not** a
permanent exclusion. The rule exposes `looks_like_join_table` so the agent can
dismiss link tables; it does not suppress them.

### StructuralFacts carries booleans; string values go in StructuralSignals

Because `SurfaceItem.StructuralFacts` is `map[string]bool`, queryable booleans
(`type_mismatch`, `has_created_at`, `encryption_hint_in_name`, `uniform_type`, …)
live there; string facts (column name, raw type, matched token) live in
`StructuralSignals`. No tautological booleans are invented.

### Designed toward the scan-all DB bucket

These rules will be wired into the DB section of scan-all at phase close (ADR 0016).
Their surface — per-rule category, boolean facts, a question — is shaped to fold
into that bucket without change now.

## Consequences

- Four surface rules join `dbrules.All()` (now eight); the DB sensor and
  `codefit-scan-db` run them unchanged (`Run` iterates `All()`). Only new rules,
  new surface categories, and coverage prose.
- No `core/db` enrichment is needed: the slice-1 neutral model already carries every
  fact these rules use (including cross-table referenced-type lookup for DB-051) —
  confirmation the neutral design held (ADR 0014).
- The name lists are a known, declared source of residual noise; each rule's firing
  condition — and especially its NEGATIVE cases (where noise is measured) — is
  locked by tests. Dogfood on a real schema is the thermometer before phase close.

## Related

- ADR 0004 — deterministic rule vs mapped surface.
- ADR 0005 — an honest red over a false green (why the counter-signal never suppresses).
- ADR 0015 — DB rules as neutral core functions.
- ADR 0016 — dimension lifecycle (these rules close into the scan-all DB bucket).
