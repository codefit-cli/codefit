# ADR 0037 — Schema gate, stage 2: the schema decides before any table gets a warehouse role

**Status:** Accepted · **Date:** 2026-08-01 · **Phase:** 2 (RF-03 OLAP closure)

**Revises [ADR 0033](0033-paradigm-role-second-neutral-input.md)** on ONE point: the
DIRECTION of classification. 0033's decision 2 (structure corroborates a name, it
never substitutes for one) and its whole role vocabulary are untouched and still
hold, INSIDE a schema that qualifies. What changes is that a schema now has to
qualify at all.

**Completes [ADR 0035](0035-schema-gate-stage-1-inert-signals.md) and
[ADR 0036](0036-schema-gate-sixth-signal-column-type-profile.md)**, which built the
six signals inert and measured them over 26 public corpora precisely so this
decision could be made from numbers.

**Re-measurement (2026-08-02): the numbers this ADR inherits from 0036 have
moved, and the verdict is better supported for it.** This ADR is not rewritten;
read the following together with §Decision 2 and §Consequences. The full
correction — method, positive control, per-row attribution and the labelling
critique — is recorded in
[ADR 0036](0036-schema-gate-sixth-signal-column-type-profile.md)
§"Re-measurement (2026-08-02)"; only what this ADR states in its own voice is
restated here. Re-run on `main` at `4f81e85`, same corpora, same pinned commits,
`auto` config, positive control first.

- **§Decision 2's verdict table.** `>= 1 of the three selected` identifies
  **10 of 13** warehouses, not 9, still with **0** false positives.
  `>= 3 of any six` identifies **6 of 13**, not 5, also at 0. `>= 2 of any six`
  identifies **10 of 13** with **3** false positives. The per-signal column
  reads `calendar_table` **9/0**, `surrogate_key_names` 3/0,
  `type_profile_split` **4/0**, `no_audit_timestamps` **9/5**, `star_topology`
  **7/5**, `bulk_load_shape` 0/0. **Selecting still beats counting on recall at
  identical precision, by a wider margin than the table below records** — the
  decision needs no revisiting.
- **§Consequences, "Recall traded, deliberately": the list of four is now a list
  of three, and one of the four never belonged on it.** `dw-kenap` qualifies —
  [ADR 0041](0041-run-on-statement-separation-at-the-create-table-tail.md)'s
  run-on separation took it from 1 parsed table to 7/7 proven, and it now fires
  `calendar_table`, opens the gate and classifies `olap` with 6 dimensions and 1
  fact. `tpch` is mislabelled analytic: its schema is TPC-H's deliberately
  normalized order-entry model, with no date dimension, no `_sk` vocabulary and
  no numeric-dominated table, so the gate is **right** to stay shut on it. The
  real misses are `dw-barousse` and `dw-ngthao`, and `dw-barousse` remains the
  entire realised loss. **`dw-ngthao`'s miss is not a parser limit**, contrary to
  what its 9/3 parse row suggests: its gold layer alone parses 3/3 proven at 100%
  profiled coverage and the gate still stays shut, on three independent counts
  (see 0036 §Re-measurement point 7).
- **§Consequences, "transactional corpora affected at all — 0 of 13": the zero
  holds, its denominator does not.** Four of those 13 corpora parse to zero
  tables and sit below `minJudgeableTables = 3`, so they cannot produce a false
  positive at all. The honest statement is **0 across the 9 transactional
  corpora that have parseable tables**. This does not weaken the
  conclusion this ADR draws from that row — it makes the claim smaller and true.
- **Everything else in this ADR is unaffected.** The inversion, the override
  semantics, the three-table floor, the `Unprovable` separation, the gate trace,
  and the before/after delta whose single moved corpus is `dw-barousse` all
  stand as written.

## Context

ADR 0033 built paradigm detection BOTTOM-UP: `paradigm.Detect` assigned a role to
each table from its name plus local structural corroboration, then folded the
schema-level `Paradigm` out of those roles (`cls.Paradigm = fold(cls.Roles)`).

The sensor's 3NF suppression (`internal/sensors/db`, `suppress3NF`) consults the
PER-TABLE role and never reads `cls.Paradigm`. So **one table named `dim_status`
with fan-in >= 1, sitting in an otherwise purely transactional schema, decided
its own silencing of the DB-002/DB-003 1NF surface.** The schema got no vote.

The evidence to answer the question does not exist at the table level:
`order_items(order_id, product_id, quantity, price)` is structurally
INDISTINGUISHABLE from TPC-DS's `store_sales(ss_item_sk, ss_customer_sk,
ss_quantity)`. The table cannot be told apart; the schema can.

## Decision

### 1. Invert the direction

`Detect` calls `WarehouseSignals` BEFORE it assigns any role. If the schema does
not qualify, every warehouse role (fact, dimension, staging, mart) is withheld,
`Classification.Gate.Withheld` records exactly what was taken and from which
table, and the schema folds to `oltp`. If it qualifies, roles are assigned
exactly as ADR 0033 specified — nothing about role assignment changed.

An OPEN gate is PERMISSION to classify, never a classification. The gate decides
whether roles MAY be assigned in this schema; each table still earns its own from
its name PLUS the A5 corroboration gate, so an open gate over a schema whose
tables carry no recognized warehouse name yields `oltp` and zero roles, and a
recognized name structure cannot corroborate still lands in
`Classification.Unprovable`.

The reference warehouse in this repository, `adventureworksdw_real_objects.sql`,
showed BOTH halves at once when this decision was taken: it opened the gate on
`calendar_table` and still classified `oltp`, because the T-SQL
`ALTER TABLE ... ADD CONSTRAINT` gap left it key-less and the A5 corroboration
gate demoted all three recognized names anyway. That gap has since been closed
(PR #82, merged to `main` before this change landed), so the corpus now measures
the OTHER outcome of the same rule: gate open on `calendar_table`, three tables
proven, 3 primary keys and 8 foreign keys corroborating the recognized PascalCase
names, paradigm `olap`, roles fact / dimension / dimension, `Unprovable` empty.
The design point is unchanged and is arguably better shown by the pair: the same
open gate produced no roles then and three roles now, purely because the
structural evidence changed underneath it.

### 2. The verdict: SELECT three signals, do not COUNT six

A schema is a warehouse **iff ANY ONE of `calendar_table`, `surrogate_key_names`,
`type_profile_split` fires.**

Measured over the 26 corpora pinned in ADR 0036 (13 analytic / 13 transactional):

| Signal | fires on W | fires on O | in the verdict |
|---|---:|---:|---|
| `calendar_table` | 8 | 0 | **yes** |
| `surrogate_key_names` | 3 | 0 | **yes** |
| `type_profile_split` | 3 | 0 | **yes** |
| `no_audit_timestamps` | 6 | 5 | no |
| `star_topology` | 6 | 5 | no |
| `bulk_load_shape` | 0 | 0 | no |

| Rule | identifies W | false positives on O |
|---|---:|---:|
| >= 1 of the three selected | **9 of 13** | **0** |
| >= 3 of any six | 5 of 13 | 0 |
| >= 2 of any six | 7 of 13 | 3 |

Selecting beats counting on recall at identical precision, and it is not close.
The two coin-flip signals are exactly what forces a count-based threshold up to 3;
`bulk_load_shape` fired on nothing at all and is empirically inert.

**All six stay computed and REPORTED** in `Gate.Fired`. They remain evidence a
consuming agent may want to reason over; they simply do not get a vote.
`Gate.Deciding` names the subset that did.

### 3. The three-table floor is inherited, not re-derived

`WarehouseSignals` refuses to evaluate a schema below `minJudgeableTables = 3`
(ADR 0035 decision 3, the no-vacuous-truths guard). With the gate wired, that has
a consequence worth stating plainly: **a schema of fewer than three tables can
never be classified as a warehouse.** The explicit override is the escape hatch.
The floor was deliberately NOT loosened for the name-based `calendar_table`,
because every number in this ADR was measured with it in place.

### 4. Developer autonomy: the explicit override outranks the gate, in one direction

`database.paradigm` keeps winning over detection (CLAUDE.md, innegotiable). What
that means for ROLES is now decided explicitly:

- **explicit `olap` or `mixed`** — the developer is ASSERTING that this is a
  warehouse. `paradigm.Resolve` RESTORES the withheld roles and reopens the
  verdict with `Gate.ByOverride`. Leaving it shut would not merely keep 1NF
  findings: the whole DW-0xx family reads `Classification.Roles`, so an
  all-unclassified map would silently run **zero** warehouse rules over a schema
  the developer just declared to be a warehouse. That is codefit answering "no it
  isn't", which this project does not do.
- **explicit `oltp`** — the developer is asserting the opposite, and NOTHING is
  restored. Manufacturing a warehouse role there would overrule the developer in
  the one direction that SILENCES findings. (The sensor also short-circuits
  suppression on explicit `oltp`; these are two independent locks on one promise,
  and the mutation that removes the `Resolve` half is caught only by the unit
  test — which is why that test exists.)

The EVIDENCE always survives an override unchanged. `Gate.ByOverride` keeps
"codefit judged this a warehouse" and "you told codefit this is a warehouse"
apart, because only one of them is evidence.

### 5. `Unprovable` is not the place to record a gate demotion

`Classification.Unprovable` answers "was this demotion STRUCTURAL, and might that
structure be a dropped statement" (ADR 0034). A schema demoted by a CLOSED gate
has a different cause entirely, so `Detect` records no `Unprovable` entry for it
and `Gate.Withheld` carries those instead. Conflating them would make `Unprovable`
blame the parser for a decision the verdict made.

### 6. The gate is never silent

The sensor's `Result.Note` gains a third trace, ordered between the completeness
inventory and the suppression trace:

- **closed, roles withheld** — the count, the tables (capped at 5 plus "and N
  more"), the three deciding signals it looked for, and the escape hatch by name.
  This is the state a developer cannot otherwise discover: the consequence is
  items that WOULD have been suppressed simply appearing, which looks like
  nothing happened.
- **open on evidence** — WHICH signals decided. Being able to write this sentence
  is one of the reasons the verdict names signals instead of scoring them.
- **open by config** — says so, and names the setting.

Empty when the gate changed nothing, in BOTH no-consequence directions.

## Consequences

### The measured behavior change, over the same 26 corpora

Run through the REAL DB sensor (`Sensor.Audit`) on both trees, `1d83c36`
(before) and this slice (after), with a positive control proving the harness
discriminates before any zero was believed. Config was `auto` throughout — no
override — which is the only setting where the gate can act unaided.

| | result |
|---|---|
| corpora whose ROLES changed | **1 of 26** |
| corpora that GAINED a role | **0** — structurally impossible under `auto`; `Detect` can only withhold |
| corpora whose DB-002/DB-003 1NF items changed state | **0 of 26** |
| corpora whose DW-0xx output changed | **1 of 26** |
| transactional corpora affected at all | **0 of 13** |

The single corpus that moved is **`dw-barousse` — a warehouse**, not a
transactional schema. It lost 10 roles (2 fact, 8 dimension) and 2 DW-021 items.
It fires only `star_topology`, an excluded signal, and the reason it fires no
deciding one is a limit ADR 0035 DECLARED rather than a new defect: its calendar
is spelled `dim_date_month`, and `calendar_table` recognizes only a role token
plus exactly `date`/`time`/`calendar`. Its keys are `*_id`, not `_sk`, and its
fact pole is 1-in-16 tables — the `numericPoleMinSharePct` false negative ADR 0036
already recorded by name.

**Zero 1NF items changed state, and that is a structural property, not luck.**
Under `auto` the post-gate role map is always a SUBSET of the pre-gate one, so
suppression can only ever DECREASE — no item can be newly silenced. It did not
decrease here either: the only two corpora where suppression actually fired
(`dw-salesmart`, `dw-ssis-salesmart`, 1 item each) both still qualify on
`calendar_table`, and `dw-barousse` had no 1NF items to restore.

### What the corpus set could NOT show, stated rather than implied

**Not one of the 13 transactional corpora had a table promoted to a warehouse
role before this change.** The hazard the gate closes — a lone `dim_`-named table
silencing its own 1NF findings inside an OLTP schema — is not exhibited by any
of them. So the measurement demonstrates the COST side (one real warehouse lost)
and cannot demonstrate the benefit side; the benefit is shown by construction, in
the positive control and in `TestSchemaGate_MovesDetect`, on the exact `dim_status`
shape ADR 0035 identified and that real Sakila's signal profile matches.

### Recall traded, deliberately

9 of 13 warehouses qualify. The 4 that do not (`dw-kenap`, `dw-ngthao`, `tpch`,
`dw-barousse`) get no warehouse roles under `auto` and therefore no DW-0xx
evaluation. Three of them already produced zero DW items before this change, so
`dw-barousse` is the entire realised loss. The trade is deliberate: a false
promotion silences a table's 1NF findings, and `database.paradigm: olap` costs a
developer one line.

### Follow-up, deliberately NOT in this slice

`roleFor` corroborates a fact candidate only by fan-OUT. "Nothing references this
table" (fan-in == 0) is a real additional fact-table signal and a genuine gap —
it is left for its own change so this slice's before/after delta has a single
cause.

### Not a silent doc change

This DOES change a declared capability: `internal/core/dbcoverage/dbcoverage.go`
(the neutral source `codefit-coverage` serves to the agent) and `COVERAGE.md`
both record the inversion, the verdict, the three-table floor and the override
semantics — source first, then mirror.

### The stage-1 inertness locks

All three were retired deliberately, in this change, and the reasoning for each
is written where they lived (`internal/core/paradigm/schemagate_wiring_test.go`):
two INVERTED on byte-identical fixtures, and the gate-symbol enumeration retired
because its subject no longer exists — an enumeration of "symbols referenced
nowhere" has nothing to enumerate once the gate is wired. Its real property is
now held by a lock that exercises both sides of the deciding/excluded split
through schemas that genuinely fire each signal.
