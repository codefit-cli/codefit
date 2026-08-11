# ADR 0068 — A negative claim needs positive evidence: the statement census, and a budget note derived from the measured size

**Status:** Accepted · **Date:** 2026-08-11 · **Phase:** 3 (regression fix, unreleased)

## Context

Two response-layer defects, found by dogfooding codefit against a real 45-migration
Flyway project. They look unrelated and are the same law broken in the same place:

> **A response may assert a negative claim only when that claim was ESTABLISHED by
> direct measurement — never inferred from a proxy that also has innocent causes.**

### D2 — blindness inferred from an absence with innocent causes

ADR 0044's unread-schema floor asks the neutral model exactly one question:
*does any `Pos` name this file?* For a file codefit could not read, the answer is no,
which is why the floor works at all. But the answer is **also** no for two files codefit
read perfectly:

- a migration whose every statement is `ALTER TABLE … ADD COLUMN IF NOT EXISTS <col>`
  on a column an earlier migration already declares — reduced correctly, and the correct
  reduction is to add nothing, so it **cannot** leave a `Pos`, by definition;
- a migration that is pure `INSERT`/`UPDATE`/`GRANT` — read, and there was no structure
  in it to read.

Both were reported to the agent as:

> codefit read NOTHING from N of the M configured schema file(s) … Not one statement in
> them reached any rule … whatever they declare, codefit did not see it.

Measured on the real project: **13 of 45 migrations** named under that sentence. Nine of
the thirteen were seed/permission files and one was a resolved no-op. codefit was telling
an agent it was blind over files it had read and resolved completely — the mirror of
invariant I1 (*nothing is declared UNREAD that was in fact read and resolved*), and worse
than noise: it teaches an agent to discount the sentence that matters.

ADR 0044 §2.5 anticipated the DML half and **declared it as an accepted over-report**
("a migration whose only content is `GRANT`, `INSERT` or `SET` statements contributes no
position and IS reported"). That was the right call while there was no evidence channel to
do better. There is one now. This ADR **supersedes that declared over-report for the DML
and permission family specifically**; ADR 0044 is append-only and is not edited, and the
rest of §2.5 — the empty/comment-only case and the union-of-comment-syntaxes reasoning —
stands unchanged.

### D3 — a fit claim derived from the wrong quantity

`budgetNote` asserted *"the complete endpoint list fit within this response's N-byte
budget: NOTHING was withheld"* whenever the withheld count was zero. It never consulted
the response's size. `fitToBudget` had **already measured** it — by serializing the
response — and handed the result over as the `stillOver` argument, which the
`withheld == 0` branch returned before reading.

The reachable shape is ordinary, not contrived: a project with a database and no security
provider has **zero endpoints** and a large `db.surface`. Its response exceeds 40 000 bytes
with nothing that can be withheld, and it said it fit. Invariant I4 (*a result that exceeds
its declared budget must say so*) violated with the measurement already in hand.

## Decision

### 1. The evidence channel: a per-source statement census in the neutral model

`internal/core/db` gains a carrier that is a sibling of `Schema.Unreduced` and
`Schema.Withheld`, not a widening of either:

```go
type StatementOutcome string          // CLOSED, core-owned vocabulary
const (
    OutcomeAlreadySatisfied StatementOutcome = "already-satisfied"
    OutcomeDeclaresNoSchema StatementOutcome = "declares-no-schema"
)
type SourceStatements struct { Total int; Accounted map[StatementOutcome]int }
// Schema.Sources maps source path -> census.
```

`Total - ΣAccounted > 0` is the blindness quantity: at least one statement in this file is
neither in the schema nor explained. That is a **direct measurement**, not a proxy.

It is **not** `Withheld` with a new reason. `Withheld` means "codefit read this declaration
and it does not belong in the persistent schema". A resolved no-op *does* belong and *is*
in the schema; folding it into `Withheld` would also give it a `Pos`, silencing the file
instead of reporting it, and would make the withholding note ("codefit read N table
declaration(s) and deliberately did NOT model them") false.

It is **not** a `SchemaParser` method. Adding one would be an interface change (ADR 0015)
and, worse, would **fail open**: a parser that forgot to implement it would report benign.

### 2. The reducer records what it already knew and threw away

Three sites in `internal/providers/sqlddl/reduce.go`, all statement-granular:

| site | before | now records |
|---|---|---|
| `applyAlterAdd`'s `if hasColumn(…) { return } // idempotent add` | silent return | `already-satisfied` |
| `reduceCreateTable`'s existing-table + `IF NOT EXISTS` skip | silent return | `already-satisfied` |
| a **new** DML/permission head branch in `apply()` | fell to `default:` | `declares-no-schema` |

`Total` is incremented once per statement at the top of `apply()` — the single funnel — so
the two counters are always commensurate. The run-on residual path re-enters `apply()`,
which is correct: a statement the reducer had to *find* is still a statement.

**Accounting is per STATEMENT, never per item.** `applyAlterAdd` and `applyAlterAction`
run once per comma-separated part, so they now return `satisfied bool` and
`applyAlterTable` ANDs across the parts. Without this,
`ADD COLUMN IF NOT EXISTS a, DROP COLUMN c` would account 1 of 1 and report a file that
really dropped a column as a benign no-op.

### 3. `apply()`'s `default:` branch is NEVER evidence of "declares no schema"

This is the binding prohibition of the whole change, and the reason state (c) is reached
through a **new positive head-shape branch** for `INSERT`/`UPDATE`/`DELETE`/`MERGE`/
`TRUNCATE`/`GRANT`/`REVOKE` rather than by reinterpreting the residual bucket.

`default:` is the "everything else" bucket. It also swallows `CREATE DOMAIN`, `COMMENT ON`,
`PRAGMA`, a truncated file and a dialect nobody has written a branch for. Reading it as
"declares no schema" would recreate ADR 0044's original silence with a *reassuring sentence
on top* — strictly worse than the noise it replaced.

Because the recognition is positive, the enumeration is safe in a way an enumeration
usually is not in this package: **anything unenumerated stays unaccounted and lands on
blindness**, so the list can only ever reduce noise, never create silence. A CTE-prefixed
DML (`WITH x AS (…) INSERT INTO …`) is deliberately not matched — and the dogfood
measurement below confirms one such file in the wild still reports as blindness, exactly as
declared.

### 4. The classifier, and its fail-closed hinge

`reasonFor(content, census)`, for a traceless file, in order:

1. NUL bytes survived BOM decoding → `reasonNotText` *(unchanged, defect)*
2. nothing outside whitespace and comments → `reasonNoDeclarations` *(unchanged, benign)*
3. **`census.Total == 0`** → `reasonNothingRecognized` *(defect)*
4. `census.Unaccounted() > 0` → `reasonNothingRecognized` *(defect, wording unchanged)*
5. `already-satisfied > 0` → `reasonAlreadySatisfied` *(new, benign)*
6. otherwise → `reasonDeclaresNoSchema` *(new, benign)*

**Step 3 is the fail-closed hinge.** A parser that never fills `Schema.Sources` — the
Prisma parser today — leaves every census zero, and a zero census must degrade to exactly
what codefit reported before the census existed: noisy, never a false all-clear. Removing
step 3 would send every traceless file under such a parser to step 6 and report it benign.
`defect()` likewise enumerates the DEFECT reasons, not the benign ones, so a reason nobody
classifies is treated as blindness.

**Declared residual, and it is deliberate.** A traceless file whose only statements are
`DROP TABLE`, `CREATE SEQUENCE`, or a recognized-but-unmodelled alter action
(`ALTER COLUMN … TYPE`, `SET DEFAULT`, `DROP NOT NULL`, `RENAME`, `OWNER TO`) stays under
blindness. For the type/nullability forms that is **correct** — the neutral model does not
carry what they declare, so codefit really did not see it. For `OWNER TO`/`ENABLE` it is an
over-report, accepted, and closable later by giving `StatementOutcome` one more member —
which is why that vocabulary is a closed enum rather than a bool.

### 5. `wholeScanBlind` → `wholeScanUnproductive`: mandatory, not a bonus

`Measured=false` when **every** configured source is traceless, whatever the reason. The
predicate no longer consults `defect()`.

This is not an improvement bolted onto the change; it is what keeps the change from
introducing the defect it exists to remove. The moment states (b) and (c) stop being
defects, a `database.schema_paths` glob that happens to match only seed files would report
`Measured=true`, score 100 and zero tables — byte-for-byte the shape of a clean audit, the
single failure this whole floor exists to make impossible (invariant I2: *not measured is
never clean*). It also closes the same pre-existing hole for a comment-only source set.

**Declared cost**, of the same class as ADR 0044's: a project whose configured schema paths
genuinely resolve to nothing structural loses its db score instead of scoring 100. Losing a
score for a schema nobody read is the correct direction, and the note names the files.

### 6. The blind list is enumerated in FULL; the benign lists stay capped

`completenessInventoryTableCap` no longer applies to defect-reason parts of the unread note.

The two lists are bounded by different things. A benign list can be a 200-file seed
directory, and naming all 200 buries the sentence that matters. The blind list is bounded
by the **configured** source list (ADR 0044 §2.3), every entry is a file the agent must go
and check, and a truncated blind list is an instruction the agent cannot follow.

**The interlock with §7 is deliberate.** Uncapping this list is only safe because a response
pushed over its budget by a long note now *says* it is over instead of claiming it fit.

### 7. The budget note's fit claim comes from the measured size

`budgetNote`'s `withheld == 0` case splits in two. `withheld > 0` is untouched — it already
appended the `stillOver` warning. The new branch also names **which** of the two reasons
nothing could be withheld: there are no endpoints at all, or every endpoint carries a
deterministic finding the budget is forbidden to hide (ADR 0054 / `dropActionableTail`).
Those lead an agent to different next steps and are not conflated.

No behaviour change to the baseline write-gate: `stillOver` was already true and already
blocked the write (ADR 0061). Only the prose lied.

**Deliberately NOT stated: the measured byte size.** A note that states its own response's
size changes that size; `fitToBudget`'s forward walk exists precisely because note digits
move. The claim stays qualitative and still satisfies I4.

**The larger question is declared, not solved.** The budget governs the endpoint lists only,
so a DB-heavy response exceeds it with nothing to withhold. Extending withholding to
`db.surface` needs a stable ranking for db surface items (endpoints have
`AggregateEndpoints`; db surface has none), a "fetch the rest" tool for a named db item
(`codefit-scan-db` re-runs everything), and a per-bucket count/withheld contract. That is
the **structural per-bucket cap**, a design of its own, and it stays open on the roadmap.
Cost, stated bluntly: such a response can still be rejected wholesale by the client, and an
honest note inside an undelivered response helps nobody. What the fix buys: the baseline
never advances on it (ADR 0061), so the residual is a failed tool call rather than corrupted
memory; and 40 000 is 62 % of the measured 64 097-byte acceptance (ADR 0062), so responses
in the 45 KB range **do** arrive — and for those the note is the whole difference between an
agent that knows it read a prefix and one that does not.

## Consequences

- Benign traceless files stop being reported as blindness. **User-visible.**
- A project whose every configured source is traceless loses its db score instead of
  scoring 100. **User-visible**, and the point.
- The blind-file list can now be arbitrarily long in principle; bounded in practice by the
  configured source list, and the budget note tells the truth when it costs bytes.
- A response with zero endpoints that exceeds its budget now says so.
- `db.Schema` gained a field, so the three SQL-DDL corpus goldens gained a `Sources` block.
  Regenerated through the repo's `-update` path and re-run without it; the diff is purely
  additive and no existing element moved.

### Measured, not asserted

Both numbers below were produced by running the **real** db sensor over a copy of the real
45-migration project, and the "before" number by running the same probe on a detached
`git worktree` at the pre-change commit — not by reasoning about the diff.

| | blind files | table count | list truncated? |
|---|---|---|---|
| before | **13** of 45 | 37 | yes — 5 shown, `(+8 more)` |
| after | **3** of 45 | 37 | no — all 3 named |

The identical table count is the control: the change reclassifies, it does not alter the
model. The three that remain were read and confirmed to be genuinely unseen content —
`ALTER COLUMN … TYPE`, `SET DEFAULT` + `DROP NOT NULL`, and a CTE-prefixed statement, the
last being live confirmation that the deliberate CTE exclusion of §3 behaves as declared.

### Locks

- Both mutation gates were run, broken and restored, with both outputs recorded in the
  commits that carry them: deleting the no-op/DML distinction fails the classification
  tests; making `budgetNote` skip `stillOver` when `withheld == 0` fails the budget test.
- `internal/sensors/db/testdata/migrations_traceless/` is a committed corpus with one file
  per state, verified by **content** (the real parser was run over it and its census
  printed) before any assertion was written against it.
- `V5__unknown_form.sql` (`CREATE DOMAIN`) is the standing control on §3: it reaches the
  same residual branch DML used to reach, and must keep reporting as blindness.
- The sqlddl negative controls (`ALTER COLUMN … TYPE`, `CREATE DOMAIN`, `COMMENT ON`,
  `PRAGMA`, `DROP TABLE` → `Accounted` empty) lock that the census never over-accounts.
- `TestSensorDB_TextThatYieldsNoStatement_IsDeclaredUnread` (PRAGMA/VACUUM stay blindness)
  and `TestSQLDDL_OutOfSubsetStatement_RecordsNothing` were re-run explicitly and are green.

### Not touched

No rule, category, tool or detection capability changed, so `internal/core/dbcoverage`,
`COVERAGE.md`, `internal/scaffold/skill.go` and `internal/mcp/server.go`'s tool descriptions
are deliberately unmodified.
