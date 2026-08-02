# ADR 0046 — The audit-timestamp vocabulary is ONE measured, shared definition

**Status:** Accepted · **Date:** 2026-08-02 · **Phase:** 2 (RF-03, DB dimension)

**Extends [ADR 0017](0017-name-heuristic-db-rules-as-pure-surface.md).** DB-052 is
still pure SURFACE, still fires only when NOTHING in the table stamps a row, still exposes
`looks_like_join_table` as a fact rather than a suppressor. What changes is the
vocabulary the rule reads a name WITH, and where that vocabulary lives.

**Related to [ADR 0015](0015-db-rules-as-core-functions-over-the-neutral-schema.md) and
[ADR 0037](0037-schema-gate-stage-2-schema-decides-before-any-table-role.md).** 0015 is why the notion
lives in the neutral model next to `db.IndexLike`; 0037 is the schema gate whose
`no_audit_timestamps` signal is the vocabulary's second consumer.

Found by the same first run of codefit's DB dimension over a real project's
PostgreSQL dump that produced ADR 0045.

## Context

DB-052 fired on 3 of that project's 9 tables. Two of the three were wrong:

```sql
CREATE TABLE public.batch_log (
    id bigint NOT NULL,
    ...
    "timestamp" timestamp(6) without time zone NOT NULL,
    batch_id bigint NOT NULL
);
```

`batch_log` and `inventory_movement` are append-only event tables whose creation
time is a column literally named `timestamp`. DB-052 **listed that column in its
own `columns:` signal** and asked "should this table track when its rows were
created?" anyway. The third table, `_user`, has no time column at all — that one
was fair.

The rule compared the normalized column name by EQUALITY against exactly
`createdat` and `updatedat`. Neither of its escape hatches ("is it a link table?",
"is it a config table?") matches "it has a differently-named timestamp column".

DB-052 is also the noisiest rule in this project — measured again for this change
at **424 items over 29 corpora**, against DB-001's 308 — so the question was never
just "add one name" but "how much of that 424 is the rule being wrong about a
spelling?"

### The parallel vocabulary

`internal/core/paradigm`'s `no_audit_timestamps` warehouse signal asked the SAME
question at schema scope, from a hand-copied two-name list and its own private
normalizer, with a comment saying so and ending: *"If a third consumer appears,
the normalizer should move to a shared home rather than be copied again."* Its
declared limit named the exact defect this ADR fixes — *"Sakila and Pagila spell
it last_update, AdventureWorks spells it ModifiedDate, and this signal fires on
all of them"* — and deferred it to a measurement.

The DW-005 / `timeDimensionName` incident is what a drifting copy costs here: two
spellings of one concept grew apart, and a rule went from a silent miss to a
confident false claim on two real corpora.

## Decision

### 1. One definition, in the neutral model

`db.IsAuditTimestampName(name string) bool` (`internal/core/db/auditstamp.go`)
owns the notion and the normalization. DB-052 asks it per table; the gate signal
asks it per schema. Both packages already import `internal/core/db`, so this adds
no import edge, and it is the same "defined ONCE and never drift" placement
`db.IndexLike` already has for index coverage.

The equivalence is locked as a test rather than as a convention:
`TestSignalNoAuditTimestamps_MatchesTheSharedVocabularyExactly` asserts, name by
name, that the signal fires exactly when the shared vocabulary does NOT recognize
the column — so widening either side alone fails.

### 2. A name earns its place by MEASUREMENT, not by plausibility

An entry is admitted when a real corpus shows it on a table DB-052 was firing on
— i.e. a case where the rule's sentence was false. Distinct tables, measured over
29 corpora (26 external + 3 vendored excerpts) plus the reporting project:

| name | tables | corpora |
| --- | --- | --- |
| `created_at` / `updated_at` | 107 / 83 | dub, formbricks, dw-salesmart, dw-barousse |
| `last_update` | 35 | sakila-full 15, pagila 14, pagila excerpt 4, sakila excerpt 2 |
| `create_date` | 4 | pagila, sakila-full, dw-ngthao |
| `creation_date` | 3 | dw-p4pa |
| `update_date` | 3 | dw-p4pa |
| `timestamp` | 2 (+2) | synapse (+ the reporting project) |
| `created_ts` | 2 | synapse |
| `creation_time`, `creation_ts`, `inserted_ts`, `added_ts`, `added_at` | 1 each | synapse |
| `ModifiedDate` | 1 | AdventureWorks (vendored T-SQL excerpt) |

**The symmetric siblings are deliberately absent** — `created_on`, `date_created`,
`inserted_at`, `modified_at`, `last_modified`, `updated_ts`. They are plausible,
and plausible is what has bitten this repository twice. Admitting a name
**silences** a table, so a guessed entry buys a false negative — the failure mode
that hides — in exchange for nothing measured. A rule that keeps asking about a
`created_on` table is noisy in a way the agent can dismiss from the `columns:`
list; a rule that goes quiet over a table with no audit trail at all is wrong in
a way nobody sees.

### 3. Equality over the normalized name — a NAME, never a TYPE

`timestamp` is in the vocabulary as a COLUMN name and is also a SQL TYPE name.
Nothing reads a column's type: `logged_value timestamp` is not an audit stamp and
still fires. That direction is locked over real DDL through the real parser
(`internal/providers/sqlddl/db052_integration_test.go`), because only the parser
can put a type and a name on the same column, and it is mutation-proven against
both plausible wrong designs (matching `db.TypeDateTime`, matching the raw type
text).

Not reading the type is a declared limit in both directions: a `created_at` typed
String counts, and synapse's `creation_ts` typed `bigint` (epoch ms) counts too.
The second is the reason **not** to add a type gate on a hunch — epoch integers
are an ordinary way to stamp a row, and a type filter would silently reinstate
the false positives this change removes.

Matching is EQUALITY, never substring: `update_trace_id`,
`insertion_prev_event_id`, `commission_created`, `ts_added_ms` and `creator` are
all real columns of firing tables, and a substring or stem test admits every one
of them. `dv_create_date` (tpcds) is the prefixed case, also excluded — admitting
it means admitting every `<anything>_create_date`.

## Consequences

### Measured, over 29 corpora

- **DB-052: 424 → 375 items.** 49 tables stopped firing; **zero** started.
- Every other rule fires **identically**: the per-corpus, per-category item
  counts for every other category diff clean, before against after.
- On the reporting project: **3 of 9 tables → 1 of 9** (`_user`, the fair one).
- Corpora that moved: sakila-full −15, pagila −14, synapse −9,
  vendored pagila excerpt −4, dw-p4pa −3, vendored sakila excerpt −2,
  dw-ngthao −1, vendored AdventureWorks excerpt −1.

Sampled against the source DDL, every silenced table does track time under
another name: Sakila/Pagila's `last_update`, dw-p4pa's `creation_date` /
`update_date`, AdventureWorks' `ModifiedDate`, synapse's epoch `creation_ts` /
`added_at`, and synapse's `monthly_active_users(user_id, timestamp BIGINT)` —
whose own source comment reads *"Last time we saw the user … measured in ms since
epoch"*.

### The gate signal fires less, and no verdict moves

Six corpora stopped firing `no_audit_timestamps` (dw-p4pa, sakila-full, synapse
and the three vendored excerpts). Over the 26-corpus set of ADR 0036 the signal
went from **9 W / 5 O** to **8 W / 3 O**.

**Not one paradigm verdict changed**, and neither did any `Deciding` set — the
signal is EXCLUDED from the vote (ADR 0037), and 8 W / 3 O keeps it excluded,
since a deciding signal's bar is zero transactional fires. This was *verified*
across all 29 corpora rather than argued from the code.

What it costs, stated rather than dropped: the vendored-corpus table in
`internal/providers/sqlddl/schemagate_corpus_test.go` used to make the
"counting signals cannot rank a warehouse against Sakila" argument by having both
fire two signals. Sakila now fires one and the warehouse two, so that PAIR no
longer illustrates it. The stage-2 selection never rested on the pair — it rests
on ADR 0036's 26-corpus measurement, re-run above — but the illustration is
weaker and the test file says so.

### Not changed

- DB-052 stays SURFACE, and still fires only when NO stamp is present;
  "only one of the pair missing" (DB-052b) remains deferred.
- `has_created_at` / `has_updated_at` remain in `StructuralFacts` and remain
  `false` on every emitted item — an item is only emitted when nothing matched.
- The gate's abstentions (unproven structure, columnless table) are untouched.
