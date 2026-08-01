# ADR 0039 — Census membership scopes the completeness gate for EVERY census rule, not just DW-020

**Status:** Accepted · **Date:** 2026-08-01 · **Phase:** 2 (RF-03 OLAP closure, follow-up to S4)

**Generalizes [ADR 0038](0038-partition-child-exempt-from-the-structural-completeness-gate.md)**
from DW-020 to all three schema-level census rules, and **closes the false negative
ADR 0038 §4 declared open**. [ADR 0034](0034-neutral-model-completeness-contract-for-structure.md)
§2.5's invariant is untouched: absence-based rules are sound only over a model proven
complete, and parser silence is still not evidence.

## Context

ADR 0038 scoped DW-020's whole-rule completeness gate to its census MEMBERS, so a
declared PostgreSQL partition child — unproven BY CONSTRUCTION under
`db.ReasonPartitionChildInheritsStructure`, on a schema codefit read perfectly —
could not abstain a rule it was not part of. It deliberately did not generalize,
and it stated the price in the same paragraph:

> adding one fact-role partition child makes **DW-005 vanish** … That is a real,
> measured false negative, left open deliberately.

That price was paid on every declaratively partitioned PostgreSQL warehouse, which
is exactly the class of schema the `feat/partition-capture` parser slice had just
made visible. Before that slice a `PARTITION OF` child matched no dispatch branch
and vanished from the model entirely; after it, the child is read — and its arrival
silenced a rule.

### Reproduced first, through the real parser, before anything was changed

Over `dw020Star(mixedPartitioningFacts)` — a two-dimension PostgreSQL star with a
partitioned `fact_sales`, an unpartitioned `fact_returns`, and one `PARTITION OF`
child — with the child REMOVED and then ADDED, nothing else changed:

```
BEFORE (no child):  DW surface = [dw-fact-no-columnar-index ×2, dw-facts-not-partitioned, dw-no-time-dimension]
AFTER  (one child): DW surface = [dw-fact-no-columnar-index ×2, dw-facts-not-partitioned]
```

`dw-no-time-dimension` is gone. The child is `role=fact proven=false Of="fact_sales"`.

### ADR 0038's DW-011 exemption claim was wrong, and that was measured too

ADR 0038 §4 stated: *"DW-011's gate reads dimension-role tables only, so a fact-role
child never reaches it."* True, and incomplete. **A dimension can be partitioned
too.** Before PostgreSQL 12 a foreign key could not target a partitioned parent at
all, so DDL of that era references a specific PARTITION — the fan-in then lands on
the child, which earns it a dimension role of its own. Measured through the real
parser on such a schema:

```
CONTROL (fact -> partitioned PARENT): dim_region     role=dimension    proven=true  Of=""
                                      DW surface includes dw-mixed-scd-strategies
CHILD   (fact -> the CHILD):          dim_region     role=unclassified proven=true  Of=""
                                      dim_region_us  role=dimension    proven=false Of="dim_region"
                                      DW surface does NOT include dw-mixed-scd-strategies
```

Two rules, not one, went silent on partitioned warehouses.

## Decision

### 1. The scoping rule is the idiom, not a DW-020 special case

Every schema-level census judgment scopes its whole-rule completeness gate to its own
census MEMBERS. ADR 0038 decision 1 is restated once, generally: *a table excluded
from the census cannot abstain the census.* A rule concludes only over its members, so
only a member's unprovenness can make its conclusion unsound.

`censusAbstains(s, member)` (`internal/core/dwrules/census.go`) is the single
implementation, and each rule passes the ONE membership predicate its census loop
uses — `inTimeDimensionCensus` (DW-005), `inSCDCensus` (DW-011),
`inPartitioningCensus` (DW-020). ADR 0038 decision 2's anti-drift property is
therefore now structural for all three: a table can never be gated without being
censused. ADR 0038 recorded the factored form as "the shape a census rule should
take" while leaving DW-005 and DW-011 byte-identical; leaving them alone is what
produced the defect above, so they are retrofitted here.

### 2. The census EXCLUSION is not optional bookkeeping — it is what prevents a NEW false affirmation

Exempting the gate without excluding the child from the census would trade a false
negative for a false affirmation, which is the worse trade. A child declares no
columns of its own — they live on the parent — so as parsed it carries no SCD
marker at all and joins DW-011's **SCD-1** group. On a warehouse whose dimensions
are uniformly SCD-2, one partition child is then enough to report a mixed strategy
that does not exist: an SCD-1 dimension fabricated out of a partition. Measured by
mutation, `scd1_dimensions` reads `dim_product, dim_region_us` under exactly that
half-fix.

For DW-005 the same exclusion keeps the census honest rather than correct-or-not: a
fact partitioned into 60 monthly children would put 61 names in `fact_tables`.

### 3. DW-005 pays for its own exclusion, where excluding costs something

There is exactly one shape where the exemption would cost accuracy instead of buying
it, and it was found by construction rather than by luck: a warehouse that partitions
its **calendar** and has its fact reference a specific partition. ADR 0033's fan-in
corroboration then demotes the parent `dim_date` to unclassified, the child is
excluded as a partition, and DW-005 would report "this schema declares no time
dimension" over DDL that declares `dim_date` on its face. Before the exemption that
outcome was masked by accident, by the child's by-construction unprovenness.

So DW-005 reads ONE thing from a child it excluded: `partitionedCalendarName` —
`timeDimensionName(t.Partitioning.Of)`. A child RESTATES its parent, the parent's
name is already in the model, and it is checked with the SAME name signal the rule
applies to every dimension, never a second vocabulary that could drift from it (the
drift that once made DW-005 blind to `D_DATE`). The child's OWN name is deliberately
not consulted: `dim_date_2024` strips to `date2024`, and accepting it would mean
matching by containment — the widening `timeDimensionName` rejects because it would
swallow `dim_update` and `dim_candidate`.

It is checked for ANY partition child regardless of role, because the shape that needs
it is precisely the one where role classification lost the parent.

### 4. Scope: the three census rules, and NOTHING else — each non-exemption reasoned, not assumed

Every other consumer of `db.Table.StructureProven()` was measured against a real
partition child (fact-role and dimension-role) and left alone, for a stated reason:

| Consumer | Gate shape | Affected by a partition child today | Changed | Why |
|---|---|---|---|---|
| DW-005 | whole-rule census | YES — rule vanished | Exempt + excluded | Measured false negative; a child is a restatement, not a fact/dimension |
| DW-011 | whole-rule census | YES — rule vanished (dimension-role child) | Exempt + excluded | Same, and excluding also prevents a fabricated SCD-1 |
| DW-020 | whole-rule census | No (already exempt, ADR 0038) | Refactored onto the shared helper | Behaviour unchanged |
| DW-001, DW-002, DW-010, DW-021 | per-table | Skips the child only | **No** | The child's columns/keys/indexes are the PARENT's. Evaluating one would ask a per-table question of a table whose structure the model does not hold — a false affirmation, not a recovered finding. The rest of the schema keeps being evaluated, so nothing is lost. |
| DB-050 | per-table, ROUTES | Routes the child to `db-table-structure-unproven` | **No** | Exempting it would make DB-050 AFFIRM "no primary key" over a key the parent declares — the exact false affirmation `ReasonPartitionChildInheritsStructure` was introduced to stop. The routed item is the honest inventory (ADR 0034 §2.8). |
| DB-001, DB-052 | per-table | Skips the child only | **No** | Same as the per-table DW rules: a table with zero columns in the model cannot testify to an absence. |
| `crossrules` (DB-010/DB-013) | none | No gate to exempt | **No** | It never consults the signal — a pre-existing declared limit stated in `sensors/db.completenessNote`, out of scope here. |
| `paradigm` schema gate + `unprovableDemotions` | evidence collection | Excludes the child from distributional/absence signals | **No** | Not a rule gate. Requiring proven structure before a table testifies is the correct polarity, and changing it would move table ROLES — a far larger blast radius than this fix warrants. |

### 5. The predicate stays in `dwrules`, NOT on `db.Table`

`isPartitionChild` reads `t.Partitioning.Of != ""`, and it lives in
`internal/core/dwrules/census.go` rather than as a method next to
`db.Table.StructureProven()`. Weighed, not defaulted:

- **What is shared is a POLICY, not a fact.** The fact (`Of`) is already a public
  field of the neutral model; a method restating it would add no information. What
  DW-005/011/020 share is "a declared partition child is not a census member and
  cannot abstain a census" — a rule-layer judgment, and ADR 0015 puts rule logic in
  rule functions over the neutral model, not in the model.
- **A model-level affordance would have no correct caller outside `dwrules` today.**
  Per the table above, `dbrules` and `crossrules` must NOT adopt it. Publishing it
  next to `StructureProven()` would invite exactly the copy-a-predicate move ADR
  0038 decision 2 exists to prevent, without the per-rule reasoning and per-rule
  fixtures that decision demands.
- **`StructureProven()`'s contract is untouched.** It still means "every statement
  affecting this table was reduced", for all of its callers across `dbrules`,
  `dwrules`, `paradigm` and `sensors/db`. Redefining it to "…or the unprovenness is
  benign" would silently change 15 call sites including the schema gate and role
  classification — a blast radius far beyond this fix.

**Cost accepted:** if `dbrules` or a future dimension ever needs the predicate, it
must be lifted to the model THEN, in a change whose per-rule reasoning is on the
record. That lift is cheap; re-deciding a wrong generalization is not.

## Consequences

- DW-005 and DW-011 emit again on declaratively partitioned PostgreSQL warehouses.
  Locked through the real parser with paired child-free controls, each with a
  distinct mutation that turns it red.
- Neither rule lost its ADR 0034 §2.5 gate: a census member unproven for a genuine
  PARSER reason (`CREATE INDEX ... ON ONLY`) still abstains the whole rule, locked
  separately for both.
- `internal/core/dwrules/completeness_gate_test.go` keeps its hand-built fixtures
  and keeps NOT locking the partition-child paths, for the reason ADR 0038 already
  gave: a hand-built `db.Table` carrying `ReasonPartitionChildInheritsStructure`
  would hold a value only the real parser produces.
- ADR 0038's third declared limit — a partitioned parent whose FKs live on its
  children loses its fact role and is invisible to DW-020 — carried no test and was
  "verified by direct probe". It now has one
  (`TestDW020_RealParser_PartitionedParentWithFKsOnChildrenOnly_EmitsNothing`),
  asserting the open schema gate, the parent's proven structure and zero fan-out,
  its demotion, the child's fan-out and fact role, and DW-020's silence. ADR 0034
  §2.7: a declared limit must be machine-visible.
- **Corpus effect: ZERO, in both directions, across all 26 corpora**, measured
  before and after on the same trees with the same harness (DW categories and
  dbrules categories/findings both counted). Not one corpus declares a partition
  child that holds a warehouse role behind an OPEN schema gate: `sakila` has 7
  children but a closed gate, and the one open-gate corpus with a child gives it no
  foreign keys, so it stays unclassified and never reached either gate. The
  measurement is positively controlled rather than assumed — a constructed
  fact-child schema gains `dw-no-time-dimension` and a constructed dimension-child
  schema gains `dw-mixed-scd-strategies` across the same two trees, while their
  child-free twin is byte-identical in both.
- Nothing codefit AFFIRMS changes: all three rules remain pure surface (ADR 0017).

## Declared limits

- **The empty-`Of` limit is inherited unchanged** (ADR 0038, `db.Partitioning`): a
  child attached by `ALTER TABLE ... ATTACH PARTITION`, or dumped as a standalone
  `CREATE TABLE` — what `pg_dump` actually emits — is indistinguishable from an
  ordinary table. It is fully read and PROVEN, so it never touches a gate; but if it
  earns a warehouse role it IS censused, as an ordinary fact or dimension.
- **`partitionedCalendarName` covers only the NAME path.** A partitioned calendar
  whose parent is named unconventionally (recognizable only by date GRAIN) is not
  recovered: the child carries no primary key of its own, so the structural signal
  has nothing to read. DW-005 then asks its question over that schema. It asks
  rather than affirms (ADR 0017), and the emitted `dimensions` list is what lets an
  agent dismiss it.
- **The DW-005/DW-011 censuses still depend on role classification.** A partitioned
  parent demoted by ADR 0033's fan-in/fan-out corroboration is absent from them, and
  widening that inside a rule would fork the role heuristic — the same limit ADR
  0038 recorded for DW-020, now test-locked there.

## Related

ADR 0014 (enrich the core, not the provider), 0015 (rules as core functions over the
neutral model — why the predicate stays in `dwrules`), 0017 (surface, never an
affirmation), 0033 (role corroboration — the fan-in/fan-out gate behind both the
DW-011 fixture and DW-005's calendar edge), 0034 (§2.5 census abstention, §2.7 a
declared limit must be machine-visible, §2.8 the measurement channel), 0037 (the
schema gate), 0038 (directly generalized; its §4 false negative closed and its DW-011
claim corrected).
