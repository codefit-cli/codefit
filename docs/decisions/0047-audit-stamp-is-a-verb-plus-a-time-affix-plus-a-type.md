# ADR 0047 — An audit stamp is a VERB plus a TIME AFFIX plus a TYPE

**Status:** Accepted · **Date:** 2026-08-02 · **Phase:** 2 (RF-03, DB dimension)

**Partially supersedes [ADR 0046](0046-audit-timestamp-vocabulary-is-one-measured-shared-definition.md).**
0046's first decision — ONE shared definition in the neutral model, asked by
DB-052 per table and by the schema gate's `no_audit_timestamps` per schema, with
the equivalence locked as a test — stands and is kept. This ADR replaces its
second decision (a fixed list of sixteen names, admitted only by measurement) and
the "never a TYPE" half of its third.

**Extends [ADR 0017](0017-name-heuristic-db-rules-as-pure-surface.md).** DB-052 is
still pure SURFACE, still fires only when NOTHING in the table stamps a row, still
exposes `looks_like_join_table` as a fact rather than a suppressor.

**Related to [ADR 0015](0015-db-rules-as-core-functions-over-the-neutral-schema.md),
[ADR 0037](0037-schema-gate-stage-2-schema-decides-before-any-table-role.md) and
[ADR 0040](0040-delimited-type-names-resolve-at-the-canonical-form.md).** 0015 is
why the notion lives in the neutral model next to `db.IndexLike`; 0037 is the gate
whose `no_audit_timestamps` signal is the second consumer; 0040 is why the type
gate below is trustworthy at all.

## Context

ADR 0046 fixed a real false positive — an append-only event table whose creation
time is a column literally named `timestamp` — by widening DB-052's vocabulary
from two names to sixteen, and every one of the sixteen was read off a real
corpus, on a table the rule was firing on. The admission rule was deliberately
strict: *a name earns its place by measurement, not by plausibility*, because
admitting a name **silences** a table and a false negative is the error nobody
sees.

That reasoning is sound about *guesses*. It does not survive contact with the
fact that a list can only ever know the spellings that happened to appear in the
corpora that happened to be cloned. 0046 explicitly rejected `created_on`,
`date_created`, `inserted_at`, `modified_at`, `last_modified` and `updated_ts`
for lack of measured support. Not one of those is ambiguous to a human reader.
Any project that spells its stamp one of those ways gets a table reported as
having no audit trail while the audit trail is listed in the item's own
`columns:` signal — the exact defect 0046 was opened to fix, just for a
different spelling.

### The obvious fix is the wrong one

"Admit any column whose name ends in `_at`" was measured before being rejected.
Across the 29 corpora plus the reporting project, **80 distinct column names end
in `At` / `_at`**, and only six of them are audit stamps:

| verdict | names |
| --- | --- |
| audit stamp (6) | `created_at`, `createdAt`, `updated_at`, `updatedAt`, `modifiedAt`, `added_at` |
| business event time (74) | `expiresAt`, `startedAt`, `finishedAt`, `lastSyncAt`, `paidAt`, `clickedAt`, `bannedAt`, `publishedAt`, `sentAt`, `deliveredAt`, `bouncedAt`, `read_at`, `validated_at`, `trialEndsAt`, `lastLoginAt`, … |

The six were not eyeballed: the counts here and in the type table below come from
running the shipped `db.IsAuditTimestampColumn` over the parser's own column dump
for every corpus, not from a grep or a hand tally.

A table whose only time column is `expires_at` **genuinely does not record when
its row came into being**. `dub`'s `jackson_ttl(key, expiresAt BigInt)`,
`formbricks`' `DataMigration(id, started_at, finished_at, status, name)` and
`WorkflowRunLog(…, startedAt, finishedAt)` all fire DB-052 today and should keep
firing. Admitting the suffix alone would go quiet over every one of them — the
error that hides, arrived at from the other direction.

What makes a name an audit stamp is the **verb**, not the suffix.

## Decision

`db.IsAuditTimestampColumn(c db.Column) bool` (`internal/core/db/auditstamp.go`)
answers the question, and it takes a **Column, not a name**, so no caller can
reach the vocabulary while skipping the type gate. There is deliberately no
name-only export.

A column is an audit stamp when all three hold.

### 1. A VERB of creation or modification

| stem | forms | evidence |
| --- | --- | --- |
| create | `create`, `created`, `creation` | MEASURED — `create_date`, `created_at`, `creation_date`, `creation_time`, `creation_ts` |
| insert | `insert`, `inserted`, `insertion` | `inserted` MEASURED (synapse `inserted_ts`); the other two are morphological closure |
| add | `add`, `added` | `added` MEASURED (synapse `added_at`, `added_ts`) |
| update | `update`, `updated` | MEASURED — `last_update` (36 columns), `update_date`, `updated_at` |
| modify | `modify`, `modified` | `modified` MEASURED (AdventureWorks `ModifiedDate`, dub `modifiedAt`) |
| change | `change`, `changed` | morphological closure |

**Rejected, with reasons rather than omission** — and each rejection is locked as
a test, not left as a comment:

- **`register` / `registered`.** A `registered_at` is when the USER registered: a
  business event that merely tends to coincide with row creation, the same
  category as `started_at`. dub declares `registeredDomains`, a business column.
- **`new`.** Not a verb here — an audit-log table's payload columns are
  `old_value` / `new_value`, and `new_date` is a business date.
- **`renew` / `renewed`.** synapse declares `last_renewed_ts`, dub declares
  `autoRenewalDisabledAt`; neither is about the row's own lifecycle.

### 2. A TIME AFFIX attached to the verb

- **Suffixes:** `at`, `on`, `ts`, `time`, `date`, `datetime`, `timestamp`.
  `at`, `ts`, `time`, `date` are MEASURED. `on` is the sibling of `at` — the same
  English temporal slot — and `created_on` is precisely the spelling 0046
  rejected and warned falsely about. `datetime` / `timestamp` are unambiguous
  time words.
- **Prefixes:** `last`, `date`. `last` is MEASURED (`last_update`, 36 columns, the
  single largest source of DB-052 false positives). `date` covers the inverted
  word order (`date_created`, `date_modified`). `ts` and `time` are **not**
  prefixes: synapse declares `ts_added_ms`, a real column of a firing table.

**At least one affix is required.** A bare `created` says *whether*, not *when*,
and no corpus shows one used as a stamp. `last_update` reaches the answer through
its prefix, which is itself a time word.

**`_by` is not a time affix, and that is load-bearing.** `created_by` is a
creation verb on a genuine audit field and it names a **person**. DB-052 asks
about time tracking, so it must not match. formbricks declares `created_by TEXT`
beside `created_at` and `last_sync_at` in one migration, and `createdBy String`
on six models.

**The affixes must consume the WHOLE remainder**, which is what 0046's equality
test bought and this rule keeps. `dv_create_date` and `dv_create_time` (tpcds),
`cst_create_date` (dw-p4pa), `dwh_create_date` (dw-ngthao),
`wp_creation_date_sk` (dw-p4pa), `ts_added_ms` and `last_federation_update_ts`
(synapse), `addedToMarketplaceAt` (dub) each leave a non-verb remainder and are
each a real column of a table DB-052 fires on.

### 3. A TYPE that can hold a time

Now trustworthy, because ADR 0040 fixed the delimited-type-name defect that had
AdventureWorksDW parsing at 359-of-359 `TypeUnknown`.

**Admitted: `TypeDateTime` and `TypeInt`, and nothing else.** The measurement is
narrow and unanimous — across the 29 corpora plus the reporting project, EVERY
column whose name passes the verb rule is typed one of those two:

| type | occurrences | which |
| --- | --- | --- |
| `datetime` | 258 | `createdAt` 105, `updatedAt` 82, `last_update` 36, `created_at` 14, `updated_at` 5, `create_date` 4, `creation_date` 3, `update_date` 3, `timestamp` 4, `ModifiedDate` 1, `modifiedAt` 1 |
| `int` | 9 | synapse epoch stamps, every one declared `bigint`: `created_ts` (×2), `creation_ts`, `creation_time`, `inserted_ts`, `added_ts`, `added_at`, `timestamp` (×2) |

`int` is the reason this is **not** "must be a date type". synapse stores epoch
milliseconds in `BIGINT` columns, and a date-only gate would reject all nine and
reinstate exactly the false positives this rule exists to remove.

Nothing else is admitted, and the DIRECTION is why that is safe: **rejecting a
type keeps the table FIRING** — a visible false positive the agent can dismiss
from the item's own `columns:` list — while **admitting one SILENCES** a table.
Zero corpora show a stamp typed `string`, `text`, `float`, `bool`, `json`,
`enum`, `bytes` or `unknown`, so none is admitted on a hunch, including
`TypeUnknown`: the parser's honest fallback is not evidence of a time. The zero
value is rejected for the same reason.

What the type gate rejects that a name-only rule could not: a **boolean**.
dw-barousse really declares `commission_created BOOLEAN`, and a `created_flag`
says whether the row was created, never when. (Both are also rejected by the name
rule — the type gate is the second lock, not the only one.)

### `timestamp` stays an explicit entry

It carries no verb and cannot be reached by the rule. It is MEASURED as a COLUMN
name: `"timestamp" timestamp(6) without time zone` is how the reporting project's
append-only event tables spell their creation time, and synapse's
`monthly_active_users` spells it `timestamp BIGINT` — its own source comment
reads *"Last time we saw the user … measured in ms since epoch"*.

Nothing reads a column's TYPE as a stamp: `logged_value timestamp` is not an
audit stamp and still fires. That direction is locked over real DDL through the
real parser (`internal/providers/sqlddl/db052_integration_test.go`) and
mutation-proven.

## Consequences

### Measured over 29 corpora + the reporting project, three ways

DB-052 items, driven through the real sensor (`Sensor.Audit`) on three built
binaries — `main` (4931040), the fixed list (ef909a9), and this rule:

| | main | fixed list | verb+type |
| --- | --- | --- | --- |
| 29 corpora | 424 | 375 | **375** |
| reporting project | 3 of 9 | 1 of 9 | **1 of 9** |

**The firing table sets for the fixed list and for this rule are byte-identical.**
All 49 tables the fixed list correctly silenced stay silenced; **zero** tables
start firing again. Every other rule category's per-corpus counts diff clean
against both baselines.

That identity is the honest result, and it is not a null result: **no corpus
happens to spell a stamp `created_on`, `date_created`, `inserted_at`,
`modified_at`, `last_modified` or `updated_ts`.** A positive control corpus was
built precisely because a count that does not move proves nothing on its own —
on it, all six of those tables fire under `main` and under the fixed list and go
quiet under this rule, while `expires_at`, `started_at`/`finished_at`,
`last_sync_at` and `created_by` keep firing under all three.

### One behavior change in the noisier direction, stated rather than dropped

A `created_at` typed as something no corpus produced — `VARCHAR`, or a type the
parser could not classify — now **fires** where the fixed list was silent. It
moved nothing in the measured corpora (all 267 name-matching columns are
`datetime` or `int`), and it is locked as a test so the trade stays visible. It
is the deliberate direction: a dismissible false positive rather than silence
over a table whose audit trail may not be one.

### The gate signal, and no verdict moved

`no_audit_timestamps` keeps asking the same shared function. Measured per corpus
rather than assumed: the fired-signal sets, the paradigm verdicts and the
`Deciding` sets are **byte-identical across all 29 corpora** before and after
this change. The signal's ADR 0036 split stays at **8 W / 3 O**, and it stays
EXCLUDED from the vote (ADR 0037), since a deciding signal's bar is zero
transactional fires.

### The shared seam survives, and got stronger

The anti-drift lock is still one test, renamed
`TestSignalNoAuditTimestamps_MatchesTheSharedDefinitionExactly`, and its cases
are now **COLUMNS rather than names**. That was forced by the type gate and it is
an improvement: a name-only equivalence would leave the type half un-shared —
DB-052 could start admitting a `created_at VARCHAR` while the gate kept rejecting
it, and nothing would notice. Two mutations prove it: severing the gate from the
shared function in either direction fails it (46 and 45 cases), and so does
letting DB-052 bypass the type gate.

### Not changed

- DB-052 stays SURFACE, and still fires only when NO stamp is present;
  "only one of the pair missing" (DB-052b) remains deferred.
- `has_created_at` / `has_updated_at` remain in `StructuralFacts` and remain
  `false` on every emitted item.
- The gate's abstentions (unproven structure, columnless table) are untouched.
