# ADR 0034 — The neutral model carries structural completeness, not only body completeness

**Status:** Accepted · **Date:** 2026-07-30 · **Phase:** 2 (`db-model-completeness-contract`)

## Context

`internal/core/db.Body{Text,Complete,Note}` (`db.go:246-259`) already declares
a load-bearing doctrine for ROUTINE bodies: a reader "MUST treat
`Complete==false` as grounds to abstain or downgrade to a surface item, never
to emit a deterministic finding" (ADR 0004/0025). Four rules already honour
it (`db031.go`, `db020.go`, `db030.go`, `db040.go`), and `crossrules.go`
independently reinvented the same discipline for the code↔schema join,
calling it "the `Complete==false` discipline ... applied to the code↔schema
join". Three independent instances of one idiom is a pattern, not a
coincidence — this ADR names it and generalizes it.

The idiom was never applied to `db.Table`'s STRUCTURAL set — its columns,
primary key, foreign keys, indexes. The sqlddl reducer silently discards any
statement it cannot reduce (`reduce.go`'s several `default:` branches), and
`DB-050` ("table without a primary key") reads that silence as evidence: it
affirmed, at confidence 1.0, that three tables of Microsoft's own
`AdventureWorksDW` sample database have no primary key — DDL that plainly
declares one for each. `dbcoverage.NotCovered()` item (6) had already
documented this in PROSE ("the most serious consequence ... DB-050 ... is
WRONG on that DDL") without a test enforcing the honesty gap it described —
exactly the failure this ADR's §2.7 closes structurally.

The governing invariant (approved, `db-model-completeness-contract` proposal
§1, not re-litigated here):

| Rule kind | Claim | Sound over an incomplete model? |
|---|---|---|
| Presence-based | "I saw X, X is wrong" | Yes |
| Absence-based | "I did not see X, therefore X is missing" | Only over a model known to be complete |

**Parser silence is not evidence.**

## Decision

### 2.1 The invariant as doctrine

Presence-based rules are sound over an incomplete model; absence-based rules
are sound ONLY over a model PROVEN complete. A rule that concludes "I did not
see X, therefore X is missing" without first checking completeness is
unsound by construction, regardless of how careful its own logic is.

### 2.2 Completeness is carried per ELEMENT, never per schema

`db.Table` gains `Complete bool`, `Note string`, `Unreduced []Unreduced` —
the structural analogue of `Body{Text,Complete,Note}`, at TABLE granularity.
Schema-level was rejected: one bad statement in a 200-table schema must not
mute the other 199. Per-constraint-kind was rejected: the drop sites do not
know the kind of the thing they dropped — not knowing it is *why* they
dropped it.

`Note` is drawn from a CLOSED vocabulary of `Reason*` constants defined in
`internal/core/db`, never provider-authored prose (§2.8). `Unreduced` carries
the VERBATIM dropped statement text and its `file:line` — the user's own DDL,
never codefit's internals — so an agent reading a routed surface item can
judge the raw source directly.

`MarkUnproven(reason, text, pos)` is a CORE method on `*db.Table` (not a
reducer-local function): it sets `Complete=false`, appends the verbatim
statement to `Unreduced`, and deduplicates `Note` by reason. Placing it in
the core (ADR 0014: enrich the core, not the provider) was forced by needing
it in TWO providers — the SQL-DDL reducer and the Prisma parser — and
guarantees both dedupe identically rather than drifting.

### 2.3 Fail-closed polarity, set explicitly at every construction site

`Complete`'s zero value is `false` — "not proven", the same polarity `Body`
already uses. This is deliberately fail-closed: a new provider that forgets
to set it is silently HONEST (it reads as unproven) rather than silently
WRONG (it reads as proven). The two existing production construction sites
(`sqlddl.builder.getTable`, `typescript.parseModel`) both set `Complete: true`
explicitly, immediately mitigating the trap the polarity choice creates —
without that fix, every table of every project would read unproven and mute
the whole dimension the moment any rule started consulting the signal.
Inverting the polarity to `Incomplete bool` was rejected: it fails OPEN,
letting a new provider that never records be silently believed complete.

### 2.4 A DECLARED skip is not incompleteness

A form the parser RECOGNIZES and deliberately does not model (`CHECK`,
`EXCLUDE`, `PARTITION` constraints; `ALTER COLUMN`/`RENAME`/`OWNER`/`ENABLE`/
`DISABLE`/`CLUSTER`/`SET`/`RESET`/`VALIDATE`/`NO` alter actions) is KNOWN not
to declare a key/index/column, so recording it would be a false demotion —
and would mute `DB-050` across ordinary real PostgreSQL DDL (a live risk:
`OWNER TO`/`RENAME`/`ENABLE`/`ALTER COLUMN` appear zero times in this
project's own SQL-DDL test corpus, so a test written against the existing
corpus would have passed vacuously; the regression fixture had to be
authored). Only a GENUINELY unrecognized statement — one the parser's own
dispatch has no branch for — marks a table unproven. This makes ADR 0018's
declared subset machine-visible instead of a comment.

### 2.5 Two-way disposition; no rule-signature change

A guard clause at the top of each rule's existing loop, reading a field the
rule already receives (`db.Table.StructureProven()`). `dbrules.Rule` and
`dwrules.Rule` are untouched (ADR 0015); the core is enriched, the rule
signature is not (ADR 0014). Same shape as the pre-existing `db031.go`
`Body.Complete` guard.

- **DB-050** (the DB dimension's one AFFIRMATION) ROUTES an unproven table to
  a dedicated `db-table-structure-unproven` surface category instead of
  affirming. Silent abstention was rejected here specifically: it would trade
  a false positive for total loss of the dimension's single deterministic
  signal, which is a worse outcome than asking the agent a routed question.
- **DB-001, DB-052, DW-001, DW-002, DW-010** ABSTAIN silently, per table.
- **DW-005, DW-011** are SCHEMA-LEVEL census judgments (at most one item for
  the whole schema) and therefore abstain the WHOLE RULE when any relevant
  table is unproven. A per-table skip here was rejected: it would silently
  SHRINK the census and still emit an item that looks authoritative over an
  undercounted schema — a worse lie than abstaining entirely.

**Positive control (equal priority, not a footnote):** a genuinely PK-less
table whose structure IS proven complete still affirms DB-050 at confidence
1.0. Honesty costs nothing here — the contract narrows false positives
without trading away a single true one.

### 2.6 BOUNDARY: `Complete` covers DROPS, not FABRICATIONS

A reducer that believes it succeeded while inventing data is a DIFFERENT
failure class this flag structurally cannot catch — it never reaches the
`default:` branch that would call `MarkUnproven`. Two concrete instances,
both real:

1. Pagila's `film.fulltext` column: a `tsvector`-typed column named
   `fulltext` collides with the MySQL inline-index-shorthand discriminator
   from the opposite direction, so it is dropped and a phantom zero-column
   index is fabricated. Confirmed against real vendored DDL; not fixed by
   this change — a discriminator bug, not a silence bug.
2. `ADD  CONSTRAINT` (non-single-space): confirmed via a dedicated
   characterization test
   (`internal/providers/sqlddl/fabrication_test.go`,
   `TestSQLDDL_R1_FabricationHypothesis`) to fabricate a phantom column/key
   literally named `CONSTRAINT`, because `"PRIMARY"` is in
   `sqlserverModifiers()` so the generic `"ADD "` column branch swallows the
   constraint body as trailing modifiers. THIS instance IS closed by this
   change: `applyAlterAction` now inspects the remainder's leading keyword
   before treating it as a column, and routes a constraint-shaped remainder
   to `MarkUnproven` instead — converting the fabrication into a recorded
   drop, which the contract then covers. It adds no new supported DDL shape
   (`WITH CHECK`, a newline, and comma-chaining still all drop cleanly); it
   only stops inventing data for one specific dispatch miss.

Both the `Table.Complete` doc comment and this ADR state the boundary
explicitly rather than let the contract over-promise a guarantee it does not
provide.

### 2.7 A declared limit must be MACHINE-visible (corollary to ADR 0018/0028)

The project's own history is the argument: `dbcoverage.go` documented the
DB-050 false-affirmation defect in prose for a full release cycle with
nothing enforcing it, and separately, `internal/core/dbcoverage/` shipped
with ZERO test files while the manifest itself twice fell out of sync with
the rules it describes (denying the DW family was built after it shipped;
denying the DB-010/DB-013 cross after it shipped). "The project has a strong
written honesty doctrine but nobody tests it" is the architect's own
diagnosis. A process rule without a test enforcing it is an intention, not a
control. Every declared limit this change touches gets an executable lock:
the fabrication characterization test, the recognized-skip regression tests,
the N2 authored fixture, the AdventureWorksDW zero-false-affirmation lock,
and the Prisma `:167`/`:92` completeness/deferred-debt locks.

### 2.8 The measurement/diagnostics boundary

codefit owes the agent an INVENTORY of what it measured and what it could
not measure. It does NOT owe a DIAGNOSIS of why its parser failed. "I could
not verify these 3 tables, here is the raw DDL at file:line" is the
product's own integrity, delivered through two carriers: DB-050's routed
surface item, and the per-scan inventory (`sensors/db.Result.Note`,
aggregated by reason, bounded, reaching the agent through `scanall.go`'s
`DBSection.Note`). "The reducer does not support `WITH CHECK ADD
CONSTRAINT`" is parser telemetry — it belongs in the coverage manifest and
this ADR, not in scan output. This is not a new responsibility:
`sensors/db.Result{Measured,Note}` already encoded exactly this doctrine at
whole-scan granularity ("Measured=false with a Note is the honest 'not
audited' state ... distinct from 'audited, 0 findings'"); this change applies
the same rule one level down, to per-table granularity.

The control is TYPE-LEVEL, not a lint: `Table.Note` is authored only from
this package's closed `Reason*` constants, and `Table.Unreduced[].Text/Pos`
is the user's own verbatim source. A provider has no channel through which a
reducer function name, dispatch branch, regex, or dialect internal can reach
scan output, because it can only SELECT a reason and QUOTE the user's own
text.

### 2.9 Exactly two carriers — no third channel invented

An earlier revision of this change's design considered annotating
`StructuralFacts` (`findings.go`, `map[string]bool`) with a completeness
signal. That path was RETIRED: `StructuralFacts` cannot express "unknown" (a
bool has two states, this signal needs three), and it is a cross-dimension
contract also produced by the TypeScript provider's `authz`/`idor`/
`overfetch`/`nplus1` rules and consumed by `core/report/aggregate.go` and
`mcp/surface.go` — widening its semantics would reach into the security
dimension, an explicit non-goal. Per-rule surface items for every abstaining
rule were also rejected: a 7×N noise explosion for one fact already carried
once. The signal has exactly two carriers (§2.5's routed item, §2.8's
inventory) and no new channel is invented.

## Alternatives considered

- Schema-level completeness (one bad statement mutes 200 tables) — rejected.
- Per-constraint-kind completeness (drop sites do not know the kind) —
  rejected.
- Fixing the three known `ALTER TABLE` shapes FIRST, before the contract —
  rejected by the architect: the contract must ship honest with the parser
  still broken, and stay honest against the NEXT unknown shape, permanently.
- Silent abstention for DB-050 — rejected: trades a false positive for total
  loss of the dimension's one deterministic signal.
- A sixth `paradigm.Role` value for "unprovable" — rejected: breaks the
  documented "always one of five named constants" invariant and silently
  changes every DW role check; a parallel `Classification.Unprovable` map is
  purely additive instead.
- Inverting `Complete` to `Incomplete bool` — rejected: fails open.
- A `StructuralFacts` sentinel key for "unknown" — rejected, §2.9.
- Per-rule surface items for every abstaining rule — rejected, §2.9.

## Consequences

- AdventureWorksDW: 3 false DB-050 affirmations → 0 affirmations, 3 routed
  `db-table-structure-unproven` surface items — locked in
  `internal/providers/sqlddl/dw_integration_test.go`
  (`TestDB050_AdventureWorksDW_NoFalseAffirmation_RoutesToSurfaceInstead`).
- Schema goldens (Pagila/Sakila/AdventureWorks excerpts) gained three
  additive keys per table (`Complete`, `Note`, `Unreduced`); regenerated in
  one dedicated, hand-inspected commit.
- Any future provider that fills `db.Schema` inherits the obligation to set
  `Complete` explicitly at its construction site(s) — the Prisma provider is
  a first-class implementer of this contract, not a follow-up: without its
  own `:167` fix, the contract would affirm unproven completeness on the
  project's own most-used route (Next.js/Prisma).
- `dbcoverage.go`'s `NotCovered()` items (5) and (6) are corrected in the
  same change that introduces this ADR — source before mirror, then synced to
  `COVERAGE.md`.
- `COVERAGE.md` stays hand-mirrored from `dbcoverage.go` — a declared,
  unchanged residual (ADR 0018/0028's fixture-gap and hygiene discipline;
  not solved by this ADR).
- `internal/core/paradigm.Classification` gains `Unprovable map[string]bool`,
  additive; `roleFor` itself is byte-identical on a proven schema.

## Related

ADR 0003, 0004, 0005, 0014, 0015, 0017, 0018, 0022, 0025 (directly
generalized from routine bodies to structure), 0026, 0027, 0028, 0029, 0033.
