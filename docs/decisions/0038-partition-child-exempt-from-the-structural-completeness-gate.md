# ADR 0038 — A declared partition child is exempt from the structural-completeness gate

**Status:** Accepted · **Date:** 2026-08-01 · **Phase:** 2 (RF-03 OLAP closure, S4)

**Refines [ADR 0034](0034-neutral-model-completeness-contract-for-structure.md) §2.5**
on ONE point: WHICH tables a schema-level census judgment must gate on. 0034's
invariant (absence-based rules are sound only over a model proven complete; parser
silence is not evidence) is untouched and still holds, including for DW-020.

**Superseded in scope (2026-08-01) by
[ADR 0039](0039-census-membership-scopes-the-completeness-gate-for-every-census-rule.md).**
Two statements below are no longer true of the code, and this ADR is not rewritten —
read them together with 0039:

- **Decision 4's DW-020-only scope, and the DW-005 false negative it left open, are
  CLOSED.** DW-005 and DW-011 now scope their gates to their own census members
  through the same shared predicate, so the measured "adding one fact-role partition
  child makes DW-005 vanish" no longer happens.
- **Decision 4's DW-011 claim was WRONG.** "DW-011's gate reads dimension-role tables
  only, so a fact-role child never reaches it" is true and incomplete: a DIMENSION can
  be partitioned, and when a fact references a specific partition (which PostgreSQL
  before 12 required) the child earns a dimension role and DOES reach DW-011's gate.
  Measured through the real parser; `dw-mixed-scd-strategies` vanished the same way
  DW-005's item did.

Everything else below still holds as written, including the third declared limit —
which 0039 gives the test it never had.

This ADR is deliberately narrow. DW-020 itself — a schema-level census of fact-table
partitioning, at most one item per schema — follows the idiom DW-005 and DW-011
already shipped under, and needs no ADR of its own. What needs one is the single
place where DW-020 does something the contract as written did not anticipate.

## Context

ADR 0034 §2.2 gave `db.Table` a completeness signal whose ONLY documented cause was
a parser failure: a statement the reducer's dispatch has no branch for
(`ReasonUnreducedTableStatement`), a body it could not parse
(`ReasonMalformedTableBody`), a table materialized by a reference before any
`CREATE TABLE` was seen (`ReasonTableNeverDeclared`). Every one of those means
"codefit could not read this DDL". §2.5 then made census rules abstain as a WHOLE
when any census member is unproven — because a shrunken census that still emits
looks authoritative over an undercounted schema, which is a worse lie than silence.

The `feat/partition-capture` slice introduced a fourth reason that is NOT a parser
failure:

- `db.ReasonPartitionChildInheritsStructure` (`internal/core/db/db.go`), set by
  `applyCreateTablePartitionOf` (`internal/providers/sqlddl/reduce.go`) for
  PostgreSQL's `CREATE TABLE c PARTITION OF p FOR VALUES ...`.
- That statement declares the child's partition BOUNDS and nothing else — its
  columns, primary key and constraints all live on the parent and appear nowhere in
  it. Marking the child unproven is exactly right, and it is what stops DB-050
  affirming "no primary key" over a key the parent declares.
- It is therefore unproven **by construction**: on a schema codefit read perfectly,
  with zero dropped statements, every declared partition child is unproven, always.

A census rule that gates on `StructureProven()` across the schema inherits that. For
DW-020 the consequence is not a lost item, it is a lost RULE: the schemas that
declare partition children are precisely the schemas that partition their fact
tables, so a child-gating DW-020 would abstain on every declaratively partitioned
PostgreSQL warehouse — structurally incapable of ever observing its own positive
case, while still looking like a working rule.

### The trap is live, and it was measured rather than reasoned

Three facts, each produced by the REAL sqlddl parser and the REAL ADR-0037
classifier over real PostgreSQL DDL — never a hand-built `db.Table`, the fixture
shape this project names as its most recurring test defect. Two of them corrected a
statement made earlier in the DW-020 slice.

1. **A partition child can earn a fact role.** A child that declares its OWN foreign
   keys — which PostgreSQL 10 and earlier REQUIRED, since a partitioned parent could
   not carry them — comes out `paradigm.RoleFact` with `StructureProven() == false`.
   It would have been censused. Locked in
   `TestDW020_RealParser_PartitionChildren_ExcludedFromCensusAndFromTheGate`
   (`internal/providers/sqlddl/dw020_partitioning_integration_test.go`), which fails
   its own fixture assertions if the parser or the classifier stops producing that
   shape.
2. **Such a child inflates the PARTITIONED side of the census, not the unpartitioned
   one.** Its `Partitioning.Declaration` is non-empty — measured verbatim as
   `PARTITION OF fact_sales FOR VALUES FROM ('2024-01-01') TO ('2024-02-01')` — so
   without the exclusion, ONE partitioned fact table with 60 monthly children is
   reported as 61 partitioned fact tables: the census drowns in restatements of a
   single fact. The opposite error (a correctly partitioned warehouse reported as N
   unpartitioned facts) belongs to the child form this rule cannot see at all, in
   "Declared limits" below.
3. **A partitioned parent whose foreign keys live on its CHILDREN loses its fact
   role.** The PostgreSQL ≤10 pattern leaves the parent with FK fan-out 0, below
   `factFanOutMin`, so ADR 0033's corroboration gate demotes it to
   `RoleUnclassified` — with the schema gate OPEN and nothing withheld. Measured:
   over such a schema DW-020 emits ZERO items, and the schema's partitioning is
   invisible to it.

## Decision

### 1. A table excluded from the census cannot abstain the census

DW-020's completeness gate is scoped to its census MEMBERS. A declared partition
child is not a member, so its unprovenness — by construction or otherwise — does not
reach the gate.

This does not weaken ADR 0034. The invariant is about the tables a rule CONCLUDES
over: "I did not see partitioning on table T, therefore T is unpartitioned" is
sound only over a proven T. A child is never a T here. The refinement is a
clarification of scope, not an exception to the rule.

### 2. ONE shared predicate, because two copies of a membership test drift

`inPartitioningCensus` (`internal/core/dwrules/dw020.go`) is consulted by BOTH the
gate loop and the census loop:

```go
func inPartitioningCensus(t db.Table, cls *paradigm.Classification) bool {
	return cls.Roles[t.Name] == paradigm.RoleFact && !isPartitionChild(t)
}
```

A table can therefore never be censused without being gated, nor gated without being
censused. That is the load-bearing half of this decision: the divergence it forbids
is exactly the one that would let a child's by-construction unprovenness abstain a
rule the child is not part of.

The precedent rules do NOT do this — DW-005 and DW-011 each spell their membership
condition inline in both loops (`dw005.go`, `dw011.go`). Neither is wrong today; both
carry the drift risk that DW-020 removes by construction. This ADR records the
factored form as the shape a census rule should take, and does not retrofit the two
existing rules, whose conditions are being left byte-identical rather than churned.

### 3. The exemption keys on the MODEL, never on the note text

`isPartitionChild` reads `db.Table.Partitioning.Of != ""` — the model's own
back-reference — and NOT `Note == ReasonPartitionChildInheritsStructure`. Two
reasons, both structural:

- `Note` is a human-facing inventory string, deduplicated by reason and
  concatenated (`db.Table.MarkUnproven`). A rule that branched on its CONTENT would
  turn ADR 0034 §2.8's measurement channel into a control channel, and would break
  the moment a child accumulated a second reason.
- `Of` answers the question the rule actually asks — "does the SOURCE declare this
  table as a partition of another?" — for a child that is proven or unproven alike.

### 4. Scope: DW-020 only, and the limit of that is stated rather than implied

Nothing here is generalized to the other census rules. It is not free: measured over
the mixed fixture above, adding one fact-role partition child makes **DW-005 vanish**
(`dw-no-time-dimension` is emitted over the same schema without the child, and is
not emitted with it). DW-005's gate covers fact- AND dimension-role tables, so a
fact-role child abstains it, on the same declaratively partitioned warehouses. That
is a real, measured false negative, left open deliberately: DW-005's census
membership is a different question from DW-020's, and widening it here — without the
per-rule reasoning and the per-rule fixtures — would be exactly the copy-a-predicate
move decision 2 exists to prevent. DW-011's gate reads dimension-role tables only, so
a fact-role child never reaches it.

DW-021 needs none of this: its gate is PER TABLE, so it simply abstains on the child
and keeps evaluating the rest of the schema — measured on the same fixture, where it
emits for both real fact tables and not for the child.

## Consequences

- Both halves of DW-020's gate are locked through the real parser rather than
  asserted: `TestDW020_RealParser_PartitionChildren_ExcludedFromCensusAndFromTheGate`
  (the census exclusion AND the gate exemption, each with a distinct mutation that
  turns it red) and `TestDW020_RealParser_UnprovenCensusMember_AbstainsTheWholeRule`
  (a genuinely unrecognized `CREATE INDEX ... ON ONLY` still abstains the whole rule
  — the exemption did not open a hole in ADR 0034 §2.5).
- `internal/core/dwrules/completeness_gate_test.go` deliberately does NOT lock
  DW-020's gate, and says so: a hand-built `db.Table` carrying
  `ReasonPartitionChildInheritsStructure` would be a fixture holding a value only the
  real parser produces.
- The exemption is already declared in the coverage chain — `dbcoverage.go` states
  it, and `COVERAGE.md` mirrors it — so this ADR adds the REASONING, not a new
  capability claim. Nothing codefit detects changes because of it.
- Any future reason added to `db.Reason*` that is TRUE BY CONSTRUCTION rather than
  caused by a parser failure inherits this question: a census rule must decide
  whether the affected tables are members, before it decides whether they abstain it.

## Declared limits

- **An empty `Of` is NOT proof that a table is not a partition.** A child attached by
  `ALTER TABLE ... ATTACH PARTITION`, or dumped as a standalone `CREATE TABLE` with
  no partition grammar of its own — which is what pg_dump actually emits — is
  indistinguishable here from an ordinary table. Such a child is fully read and
  PROVEN (it never reaches `MarkUnproven`), so it does not interact with the gate at
  all; but if it earns a fact role it IS censused, on the UNPARTITIONED side,
  inverting the truth about a warehouse that does partition. The limit lives in the
  model (`db.Partitioning`'s type doc) and cannot be closed by this rule.
- **An empty `Declaration` reports the SOURCE, not the database.** A table
  partitioned by a form this parser does not read, or by a statement in a file the
  scan never saw, reads as unpartitioned.
- **A partitioned parent whose FKs live on its children is invisible to DW-020**
  (context fact 3): fan-out 0 costs it its fact role, so it never enters the census.
  That is ADR 0033's corroboration gate doing its job, and widening it inside DW-020
  would fork the role heuristic. This one is recorded in prose (`dw020.go`,
  `dbcoverage.go`) and verified by direct probe; unlike the other two it carries no
  test of its own.
- **The DW-005 false negative in decision 4 is open**, measured and unfixed.

## Related

ADR 0014 (enrich the core, not the provider), 0015 (rules as core functions),
0016 (dimension lifecycle), 0017 (a counter-signal is exposed, never used to
suppress), 0018 (the declared subset), 0033 (role corroboration — the fan-out gate
that hides a partitioned parent), 0034 (directly refined, §2.5 and §2.8), 0037 (the
schema gate: a closed gate leaves the census empty and DW-020 silent).
