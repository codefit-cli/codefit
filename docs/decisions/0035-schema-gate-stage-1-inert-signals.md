# ADR 0035 — Schema gate, stage 1: five schema-wide warehouse signals, built inert

**Status:** Accepted · **Date:** 2026-07-31 · **Phase:** 2 (RF-03 OLAP closure)

**Extended by [ADR 0036](0036-schema-gate-sixth-signal-column-type-profile.md):**
a SIXTH signal (`type_profile_split`, over column types) and a 26-corpus
measurement of all six. Everything below still holds as written; 0036 changes
what the numbers in "The measurement" section mean, since that section reads
only the corpora this repository vendors.

**One sentence below is superseded in scope (2026-08-01) by
[ADR 0040](0040-delimited-type-names-resolve-at-the-canonical-form.md),** and this
ADR is not rewritten: the re-measurement paragraph ends "`type_profile_split` still
abstains on the separate bracketed-type gap, exactly as ADR 0036 predicted it
would." That gap is now CLOSED. On the vendored AdventureWorksDW excerpt the signal
no longer abstains — it is evaluated and returns false on the arithmetic (one
text-dominated table against a floor of two). **The corpus's fired set, the gate's
verdict and every counting argument in this ADR are unchanged.**

## Context

ADR 0033 built paradigm detection BOTTOM-UP: `paradigm.Detect` assigns a role
to each table from its name plus local structural corroboration, then folds the
schema-level `Paradigm` out of those roles (`cls.Paradigm = fold(cls.Roles)`).

The sensor's 3NF suppression (`internal/sensors/db`, `suppress3NF`) then
consults the PER-TABLE role and never reads `cls.Paradigm` at all. The
consequence is a real hole: **one table named `dim_status` with fan-in >= 1,
sitting in an otherwise purely transactional schema, decides its own silencing
of the DB-002/DB-003 1NF surface.** The schema gets no vote.

The reason the question cannot be answered where it is currently asked is that
the evidence does not exist at the table level.
`order_items(order_id, product_id, quantity, price)` is structurally
INDISTINGUISHABLE from TPC-DS's
`store_sales(ss_item_sk, ss_customer_sk, ss_quantity)`. The table cannot be told
apart; the schema can.

## Decision

### 1. Invert the question — but stage the change, and measure before wiring

The target design is: decide from SCHEMA-WIDE evidence whether this is a
warehouse AT ALL, and only then assign roles inside it.

Stage 1 (this ADR) computes and NAMES that evidence and wires it to **nothing**.
Stage 2 does the wiring, from the measurement rather than from a hunch.

Building it inert is the decision, not an implementation detail. The threshold
question — "how many signals, and which, constitute a warehouse" — is exactly
the question a hunch answers badly, and stage 1 exists to put numbers in front
of it first. `WarehouseEvidence` therefore carries **no verdict and no
threshold**, only the named signals that fired.

Inertness is test-locked two ways, not asserted in prose
(`internal/core/paradigm/schemagate_inertness_test.go`): an AST scan of every
non-test Go file in the repository proves no production code references any gate
symbol, and a behavioral test proves `Detect` does not move over a schema on
which the gate fires. Stage 2 must retire the first lock deliberately.

### 2. Five signals, each a pure function of `*db.Schema`, each NAMED

No fuzzy score. A consumer must always be able to be told WHY.

| Signal | Fires when | Threshold rationale |
|---|---|---|
| `calendar_table` | a table name, after an OPTIONAL role token is stripped, equals date/time/calendar | a dedicated calendar is near-exclusive to warehouses; OLTP stores timestamps on rows |
| `surrogate_key_names` | >= 3 `_sk`-suffixed columns across >= 2 distinct tables | 1 column is an accident, 2 could be a composite key; the 2-table half is what makes it a SCHEMA fact rather than one developer's habit |
| `bulk_load_shape` | zero declared FKs anywhere, >= 4 key-like columns in one table AND >= 8 schema-wide | a fact row is a tuple of dimension references; without the per-table concentration, every CRUD schema with one `*_id` per table qualifies |
| `no_audit_timestamps` | no table carries created_at/updated_at | row-level audit stamps are an OLTP habit; a warehouse timestamps its LOAD |
| `star_topology` | some table's FKs reach >= 2 DISTINCT tables that themselves reference nothing | depth is the discriminator: an OLTP mesh runs 3+ deep (order -> customer -> address -> country) |

Signals 3 and 5 are near mutually exclusive by construction (3 requires no FKs,
5 requires FKs). That is expected: a real warehouse rarely shows both, and the
gate reports what it sees rather than reconciling them.

### 3. NO VACUOUS TRUTHS — the guard that outranks the signals

Two signals conclude from ABSENCE, and absence is trivially true of an empty
schema. A gate that fires on an empty schema classifies anything as a
warehouse.

- A schema-wide floor of **3 tables** (`minJudgeableTables`) gates all five: 3
  is the smallest schema in which any of the shapes can exist (a hub plus two
  spokes). Below it the absence-based signals are vacuous and the structural
  ones unreachable.
- Every absence-based claim ABSTAINS on a table whose structure is not proven
  complete (ADR 0034's `StructureProven`), and on a table with no columns —
  which cannot testify to an absence. `star_topology` applies this to its
  SPOKES only: a dropped statement can only UNDERcount a hub's fan-out, never
  fabricate it, the same promotion/demotion asymmetry `unprovableDemotions`
  rests on.

### 4. Compose on the existing vocabularies; never add a third

`calendar_table` composes on `paradigm.StripRoleToken`. A second parallel role
vocabulary living in `dwrules` is precisely what drifted when this package's
vocabulary widened, turning DW-005 from a silent miss into a confident false
claim over two real warehouses. A third copy here would re-open that.

`no_audit_timestamps` reuses db052's `normalizeIdent` CONVENTION (lowercase,
drop separators, compare by equality) — replicated as `normalizeGateIdent`
rather than imported, because `internal/core/paradigm` imports only
`internal/core/db` and a core->core edge to `dbrules` for a four-line string
normalizer is a poor trade. If a third consumer appears, the normalizer should
move to a shared home rather than be copied again.

Suffix questions do NOT go through that normalizer: dropping the underscore
makes `risk`, `disk` and `asterisk` all end in `sk`. `trailingSegment` matches
the last underscore-delimited segment instead — the delimiter IS the word
boundary, the doctrine `segmentRole` already applies to role tokens.

## Consequences

### The measurement, which is the point of stage 1

Over every SQL corpus vendored in this repository, through the real parser, **as
measured at stage 1** (see the update below for what the live test locks today —
this table is the historical record that drove the stage-2 decision, not the
current measurement):

| Corpus | What it is | Signals fired |
|---|---|---|
| `mysql/sakila_excerpt.sql` | OLTP (rental shop) | `no_audit_timestamps`, `star_topology` |
| `pagila_excerpt.sql` | OLTP | `no_audit_timestamps` |
| `tsql/adventureworks_excerpt.sql` | OLTP | `no_audit_timestamps` |
| `tsql/adventureworksdw_real_objects.sql` | **warehouse** | `calendar_table` |
| every other corpus | — | none |

**The reference warehouse fired ONE signal; a three-table excerpt of Sakila
fired TWO.** A naive ">= 2 means warehouse" threshold applied to those numbers
would have classified Sakila as a warehouse and AdventureWorksDW as not one.
Stage 2 must not pick a threshold from these numbers as they stand.

Three causes, each independently verifiable:

1. AdventureWorksDW's structure is entirely UNPROVEN — the T-SQL reducer drops
   all three `ALTER TABLE ... ADD CONSTRAINT` shapes this corpus uses (a
   pre-existing parser gap, recorded in `dw_integration_test.go`), so its three
   real primary keys and eight real foreign keys are invisible. The gate
   ABSTAINS rather than affirming, which is correct behavior over a model it
   cannot prove complete — and it means **fixing that parser gap is a
   prerequisite for stage 2**, not an unrelated cleanup.
2. `no_audit_timestamps` fires on all three OLTP corpora because they spell
   their audit stamp `last_update` (Sakila, Pagila) or `ModifiedDate`
   (AdventureWorks). Reusing db052's vocabulary is the right seam; the
   vocabulary itself is too narrow. Measured, not guessed.
3. `star_topology` fires on Sakila because `film_actor` references `actor` and
   `film` and neither references anything back — a textbook depth-1 star that is
   a join table. This is the premise of this ADR restated: no single table, and
   no single signal, separates a warehouse from a transactional schema.

#### Update — the prerequisite in cause 1 was fixed, and the row moved

Cause 1 named the T-SQL `ALTER TABLE ... ADD CONSTRAINT` gap a **prerequisite for
stage 2**. It was closed (PR #82) and merged to `main` before the stage-2 change
landed, which is exactly the "fix that parser gap and this row should change"
outcome this ADR asked for. Re-measured through the same real parser, the
warehouse row now reads:

| Corpus | What it is | Signals fired |
|---|---|---|
| `tsql/adventureworksdw_real_objects.sql` | **warehouse** | `calendar_table`, `no_audit_timestamps` |

Its three tables are proven, its 3 primary keys and 8 foreign keys are in the
model, and `no_audit_timestamps` therefore stopped abstaining and now affirms
(none of the three declares `created_at`/`updated_at`). `bulk_load_shape` and
`star_topology` still do not fire, and no longer for want of proof: eight
declared foreign keys falsify `bulk_load_shape` outright, and six of them point
at dimension tables this three-table excerpt does not vendor, so `star_topology`
cannot show its spokes are leaves. `type_profile_split` still abstains on the
separate bracketed-type gap, exactly as ADR 0036 predicted it would.

**This does not weaken the counting argument; it changes its shape.** At stage 1
a `>= 2` threshold ranked the warehouse and Sakila *backwards*. Now they fire two
signals each, so no threshold ranks them *at all* — at `>= 2` both are
warehouses, at `>= 3` neither is. Selecting three zero-false-positive signals,
which is what stage 2 did, is the answer to both readings. The live per-corpus
numbers are test-locked in
`internal/providers/sqlddl/schemagate_corpus_test.go`.

### Not a declared capability

Nothing here changes what codefit detects or reports, so
`internal/core/dbcoverage/dbcoverage.go` and `COVERAGE.md` are deliberately
untouched. Announcing inert machinery as coverage would be the over-promising
this project's documentation rules exist to prevent.

### Declared limits carried by stage 1

- `calendar_table` does not recognize a spelled-out or qualified calendar
  (`date_dimension`, `dim_fiscal_date`, `calendar_lookup`).
- `surrogate_key_names` recognizes only the underscore-delimited `_sk` segment;
  no separator-free PascalCase spelling was observed in any corpus, and an
  unobserved spelling is a guess, not a convention. AdventureWorksDW's
  `CustomerKey` is a different convention and is deliberately not counted.
- `bulk_load_shape` fired on no vendored corpus, so its thresholds are
  reasoned-from-shape rather than measured. It remains the least evidenced of
  the five.
