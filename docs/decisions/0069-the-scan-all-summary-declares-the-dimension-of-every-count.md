# 0069 — The `scan-all` summary declares the dimension of every count

Date: 2026-08-11
Status: accepted
Supersedes: nothing. Extends the honesty contract of
[ADR 0054](0054-actionable-endpoints-are-named-and-the-response-declares-its-budget.md)
(a prefix of the truth must say which prefix) to the summary block.

## Context

`codefit-scan-all`'s `summary` carried four unqualified counts:

```json
"summary": { "endpoints": 0, "deterministic_findings": 0, "surface_items": 0, "certain_concerns": 0 }
```

All four were computed from `secRes`, the **security** sensor's result. `dbRes`
was never read by the summary. The block presented itself, unlabelled, as the
response's summary.

Two facts turn that into a false all-clear rather than an incomplete one:

1. **A DB-only project produces an all-zero summary.** No security provider
   resolves for the language, so `secRes` stays zero while the DB dimension runs
   independently — its parser is chosen by the shape of the schema file, not by
   the language ([ADR 0018](0018-sql-ddl-parser-declared-subset-incremental-reducer-input-selection.md)).
2. **Every rule in the DW-0xx family is surface-only** — verified against
   `internal/core/dwrules/dwrules.go`, where all seven entries of `All()` are
   annotated surface — and the score counts deterministic affirmations only
   (the principle [ADR 0017](0017-name-heuristic-db-rules-as-pure-surface.md)
   establishes for name-heuristic rules). So a warehouse schema
   with dozens of mapped surface items yields `by_dimension.db` high **and**
   `summary.surface_items: 0` — two independent all-clears over the same schema.

Observed in dogfood on a Java project: `summary.surface_items: 0` while
`db.surface` held 62 items. The 62 existed in exactly one place in the response,
and it was not the block an agent skims first.

This is **I4** of `docs/specs/audit-protocol.md` (a partial result declares
itself partial) — the summary was a security-only prefix presented as the whole.
The harm is **I2**: a zero that means nobody looked.

It was a CLASS, not a field. All four counts had the same defect.

Committed proof it predates the discovery: `internal/mcp/testdata/scanall_ts_withdb_prechange.json`,
a real response captured at `337f158`, reports every summary count as zero over a
`db` section holding one finding (DB-050) and one surface item — with
`baseline.new: 2` in the same bytes, codefit's own record that it observed two
items that pass.

## Decision

`summary` carries one sub-block per audit dimension plus a derived roll-up. Each
count declares the dimension it counted.

```go
type ScanAllSummary struct {
    Security *SecuritySummary `json:"security"` // nil = not measured
    DB       *DBSummary       `json:"db"`       // nil = not measured
    Totals   SummaryTotals    `json:"totals"`
    Note     string           `json:"note"`
}
```

Breaking, no flag, no dual shape. `summary.security.*` carries the old values
verbatim; `summary.totals.*` is the new cross-dimension number.

### D1 — Pointer sub-blocks, keys always present, never `omitempty`

`null` is the statement "this dimension was not measured". `score.by_dimension`
already ships exactly this — `"db": null` beside `"db": 95` — so the response
speaks one language about the same fact. An **absent** key would be a third state
the reader has to guess at.

`ScanAllResponse.DB` *is* `omitempty`, but only for ADR 0020's byte-identity
guarantee for projects without a database. That reason does not apply to a
summary sub-block, so the inconsistency is deliberate and confined.

`summary.db` is null in **both** unmeasured cases: the dimension did not run
(nothing configured, or narrowed out of scope) and it ran without measuring
(`DBSection.Measured == false`: no parser, unreadable schema). Rejected: a zeroed
block for the second case. A zeroed block over a schema codefit could not read is
an affirmation it never earned.

### D2 — `summarize()`'s signature is the enforcement, not a convention

```go
func summarize(secRan bool, secRes findings.SensorResult, endpoints []report.EndpointReport,
               dbRan bool, dbRes findings.SensorResult, dbSources []string) ScanAllSummary
```

It takes `findings.SensorResult` values and **no `*DBSection`**. `DBSection.Findings`
and `.Surface` are baseline-FILTERED in place; counting them would mix populations
with the score, which is computed over the raw result. Because
`[]findings.Finding` does not type-check as a `findings.SensorResult`, the mistake
is a **compile error**, not a comment nobody reads.

Verified under mutation: reproducing the post-filter defect required moving the
call below `filterDBByBaseline` **and** rebuilding a `SensorResult` from the
filtered slices. The obvious mistake could not be written.

`dbRan` is the measured predicate. `runDBForScanAll` returns `ran=true` only on
the same path that returns `Measured=true`, so `dbRan` **is**
`dbSection != nil && dbSection.Measured` without handing this function the
section it must not read.

### D3 — Computed before the baseline filter, not at the return literal

The call sits immediately after `dbRes` is populated, above the
`filterDBByBaseline` mutation. D2's braces are the belt; lexical order removes
even the window in which a future edit could reintroduce the mistake.

### D4 — `Totals` is derived, never a literal

Summed inside `summarize` from the non-nil sub-blocks. A hand-written total is
right exactly once and then drifts, silently, the first time a dimension is
added.

### D5 — One census, two readers

`dbSources := distinctCanon(dbRes.AuditedFiles)` is computed once and shared
between `summary.db.schema_sources` and the DB-only `scope.auditable_total`
denominator. Two call sites computing one census is precisely how the two numbers
would come to disagree about how much schema a pass read.

### D6 — `endpoints` and `schema_sources` are never summed

Each is its own dimension's SCALE UNIT. A table has no route; adding them
produces a number with no referent. `endpoints` stays under `security`,
`schema_sources` under `db`, and `totals` carries only `deterministic_findings`
and `surface_items` — the two units that mean the same thing in both dimensions.

`schema_sources` is confirmed as the DB scale unit because it is the only one
reachable **without a new measurement**: a table or entity count would need the
parsed `db.Schema` plumbed out of `runDBForScanAll`, which returns only
`(*DBSection, SensorResult, bool)`. Caveat recorded: under a narrowed scope it
shrinks with what was read — correct, and consistent with `scope`.

## The `certain_concerns` asymmetry (the one deliberate deviation from the spec)

The spec proposed `certain_concerns` per dimension, on the premise that
"certainty 1.0 means the same thing in both". **That premise does not survive
contact with the code.**

`countCertain` (`internal/core/report/aggregate.go`) counts `Deterministic`
**plus** `SurfaceConfirmed`, and `concernFromSurface` sets `SurfaceConfirmed`
unless the structural fact `local_access_detected` is false — a **security**-surface
fact DB items never carry. So the security field is not a certainty-1.0 count at
all.

A DB sibling under the same name would therefore be a
same-name-different-definition count: the exact I4 defect this change removes,
reintroduced one level down. And `totals` would sum incommensurables while
claiming the opposite.

The asymmetry that decides it: adding `db.certain_concerns` later is **additive**
(a new key). Shipping a differently-defined one now is **breaking**. So it is
omitted from `db` and from `totals`, and the omission is locked by a test that
fails if the key ever appears.

## Sequencing (load-bearing, not procedure)

The renest alone breaks the golden regression test **on shape**. Recording that
failure would have produced evidence of *renaming*, not evidence that the fix
reached the summary. So the work was split:

- **Step A** — renest, migrate the call sites, `db` left nil, totals derived.
  Provably behaviour-preserving: `summary.security.*` is verbatim the old
  `summary.*`.
- **Step B** — wire the DB dimension, then run the regression test UNMODIFIED and
  record the output. The recorded diff shows `summary.db: {1,1,1}` and
  `totals.surface_items: 1` against the golden's zeros. It could not have been
  produced by Step A.
- **Step C** — only then does `summary` join the strip-set, with dedicated tests
  taking over its coverage.

A green on that regression test before the strip-set was narrowed would have been
a **red flag, not a pass**: it would have proved the fix never reached the
summary.

## The goldens are evidence, not comparison targets

`scanall_ts_withdb_prechange.json` and `scanall_prechange_invariant.json` keep
their captured bytes. Regenerating them from a fixed build would destroy the only
committed proof the defect existed, and `testdata/README.md` already forbids it.
The shape change is absorbed by a `flatSummary`/`flatten` **adapter** on the test
side. The adapter exists twice — package `mcp` and package `mcp_test` — because
Go gives the two no way to share an unexported helper. Forced by the package
split, not chosen.

The projection is itself the migration assertion: it reads `summary.security` and
compares against the golden's flat `summary`, so "consumers read
`summary.security.*` for the old values" is a checked promise rather than a
sentence in a changelog.

## Consequences

- **Breaking.** Any consumer reading `summary.deterministic_findings` now reads
  `summary.security.deterministic_findings` (identical value) or
  `summary.totals.deterministic_findings` (the cross-dimension number).
- **The response floor grew 530 bytes**, paid once per response — about 1.3% of
  the 40 000-byte budget. Measured over the budget fixture with
  `len(json.Marshal(resp))` (the length `fitsBudget` itself uses), both trees read
  at the same budget so the digits of the number embedded in `budget.note` cannot
  move the result: the withheld-everything floor went **4 016 → 4 546**, the full
  response **4 747 → 5 277**, and the serialized `summary` block **83 → 613**.
  The three deltas are the same 530 because the whole growth is the summary
  block: 440 bytes of always-present `note` prose (438 runes) plus 90 bytes of
  per-dimension nesting. Not free; declared.

  Two figures in the first drafted version of this ADR (commit `ca25809`, on the
  PR branch, never merged) were wrong. They are corrected in place, before this
  ADR lands, and the error is recorded rather than quietly dropped: the cost was
  stated as "~380 bytes" — the note alone is 440 and the total is 530 — and the
  floor was said to have moved "from under 4 000 to 4 530". It did not. On `main`
  a 4 000-byte budget FIT, at exactly 4 000 bytes with 2 endpoints rendered, and
  4 530 is the size at a 4 530-byte budget, not the floor. A measured number in an
  ADR is an assertion; these two did not match the measurement.
- **A DB-heavy response's size problem becomes more visible.** A correct
  `summary.db.surface_items` neither causes nor fixes it. The structural
  per-bucket cap for `db.surface` remains roadmap P0-4's declared remaining half.
- **An accidental survival was removed.**
  `TestScanAllBudget_DeterministicFindingIsNeverDemotedToAName` asserts an
  endpoint-anchored equality and survived only because its fixture configures no
  database. It now reads `summary.security` explicitly, and a sibling control
  over a DB-carrying project requires that same equality to FAIL against
  `totals`.
- **A control that passed for the wrong reason is named.** The R4 invariance lock
  included `Summary`, but its axis is complete-analysis vs rendered-subset: both
  runs it compares were equally blind to the DB dimension, so it could not
  detect a dimension-completeness gap. It now runs over a DB-carrying project
  too, reads through its own projection so the coverage cannot be quietly
  removed, and its comment states what it does and does not cover.

## Missing control, named not designed

**Invariant I4 has no registered control.** Nothing in this repository asserts
that a response's summary covers every dimension the same response reports as
measured. This defect existed because that assertion had no home, not because
anyone wrote the wrong line. The invariant → control registry that would give it
one is a separate, larger piece of work already on the roadmap. Named here so it
is not rediscovered a third time; not designed here.

## Not in scope

The DB-surface per-bucket structural cap (P0-4's remaining half) ·
`scoring.IsBlocked` (security-critical by definition, PRD §18, not part of
`ScanAllResponse`) · the invariant → control registry · the four unbuilt
dimensions — the shape accommodates them without another rewrite, and this change
adds no sub-block for them.
