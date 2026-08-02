# ADR 0043 — Every CREATE-TABLE-shaped head lands somewhere, and a withheld table is not an unread one

**Status:** Accepted · **Date:** 2026-08-02 · **Phase:** 2 (RF-03 parser floor)

**Extends [ADR 0034](0034-neutral-model-completeness-contract-for-structure.md).**
0034's invariant, its per-table carriers and its measurement/diagnostics boundary
are untouched. What this ADR adds is (1) the TABLE-family analogue of the
abstention floor 0034 gave the INDEX family, and (2) a THIRD disposition its
vocabulary did not have a word for: a declaration the parser reads perfectly and
deliberately declines to model.

**Related to [ADR 0041](0041-run-on-statement-separation-at-the-create-table-tail.md):**
0041 closed the last path that lost table structure with no trace *for a
statement the reducer had to find*. This one closes the last path that lost it
*for a statement the reducer was handed*.

## Context

`reIndexShapedHead` (`reduce.go`) has given the `CREATE INDEX` family an
honest-abstention floor since ADR 0034: a form neither `reCreateIndex` nor
`reCreateColumnstoreIndex` can reduce still announces itself as index-shaped, so
`markUnrecognizedIndexShape` records it instead of letting it fall through
`apply()`'s `default:`.

**There was no table-shaped equivalent.** `rg -c "TableShapedHead|markUnrecognizedTable"`
returned zero. A `CREATE <anything> TABLE` head no branch reduced matched
nothing, fell through `default:`, and evaporated.

**Measured through the real `Sensor.Audit`, not inferred.** A schema whose only
statement is an unlogged table:

```
=== only an UNLOGGED table in the whole schema ===
  Measured=true   Note=""   tables=0   Schema.Unreduced=0   findings=0 surface=0
```

That is the false *"audited, 0 findings"* state over a schema codefit never
read — the worst state an auditor can occupy, because it is indistinguishable
from a clean bill of health.

Twelve forms were confirmed silent this way, each under the dialect it belongs
to (the same probe, before and after, is quoted in the PR):

| dialect | form |
|---|---|
| PostgreSQL | `UNLOGGED`, `UNLOGGED … IF NOT EXISTS`, `TEMP`, `TEMPORARY`, `GLOBAL TEMPORARY`, `LOCAL TEMPORARY` |
| MySQL | `TEMPORARY` |
| T-SQL | `#Local`, `##Global` (a NAME prefix, not a keyword) |
| any | `CREATE FOREIGN TABLE`, `CREATE TABLE … AS SELECT`, a quoted name outside the reducer's identifier class |

`CREATE TABLE IF NOT EXISTS` was never affected — it is an explicit group in
`reCreateTable` and has always worked. It is named here because the two are easy
to lump together and they are not the same case.

## Decision

### 2.1 A table-shaped-head floor, written against a SHAPE not a list

`reTableShapedHead` is the LAST-RESORT net of `apply()`'s table branches, and its
whole value is that it does not enumerate: the twelve forms measured as lost are the
ones that were *known*, and the dialect keyword nobody has read yet lands here
too instead of evaporating. This closes the CLASS, not the cases.

It records the statement verbatim on `Schema.Unreduced`, reaching the agent
through the per-scan inventory (`sensors/db.Result.Note`, ADR 0034 §2.8). Schema
level rather than a table's `MarkUnproven`, for the reason ADR 0041 §2.6 already
gave: this is "recognized as table-affecting but not attributable to a specific
table", and demoting an unrelated table would be a false demotion.

### 2.2 The catcher DECLARES; it never guesses a name

`markUnrecognizedIndexShape` can attribute its drop to a table, because every
`CREATE INDEX` form carries an `ON` clause. The table catcher has no such
handle — and, more fundamentally, **the missing table IS the loss**. The forms
that land here are by definition ones whose grammar this reducer does not know,
so it does not know where their name sits either: `CREATE TABLE x AS SELECT`
puts it in one position, `CREATE FOREIGN TABLE x SERVER s` in another, and the
next dialect's form somewhere else again.

Registering a table from a guessed span would be the FABRICATION class
`db.Table.Complete` structurally cannot catch (ADR 0034 §2.6) — strictly worse
than the silence it replaces. The VERBATIM statement plus its `file:line` is what
carries the name to the agent, without codefit asserting which token it is.

### 2.3 A two-word modifier window, and why that bound is load-bearing

`^create\s+(?:\w+\s+){0,2}?table\b`. Two words admits every real one- and
two-word form (`UNLOGGED`, `FOREIGN`, `EXTERNAL`, `TRANSIENT`, `GLOBAL
TEMPORARY`, `LOCAL TEMP`, `OR REPLACE`, `SET`/`MULTISET`) while excluding the
three-word shapes that are NOT table declarations and must keep falling to
`default:`:

| statement | interstitial words |
|---|---|
| `CREATE TYPE IdList AS TABLE (…)` — a T-SQL table TYPE | `type x as` |
| `CREATE STATISTICS s ON t (…)` | `statistics s on` |
| `CREATE SCHEMA s CREATE TABLE a (…)` — the SQL-standard element list | `schema s create` |

`TABLESPACE` is excluded by the word boundary alone, the same guard that does
most of the work in `reRunOnStatementHead`. Both guards are mutation-proven
separately: widening the window to three words makes the type and element-list
cases fail; removing `\b` makes the `TABLESPACE` case fail.

The `CREATE SCHEMA` element list is the open gap ADR 0041 recorded, and it stays
open **deliberately**: admitting it would start declaring statements on
AdventureWorks-for-PostgreSQL, a corpus that is read correctly today (measured —
the three-word window gives it 5 new `Schema.Unreduced` entries).

### 2.4 UNLOGGED is ADMITTED to the model

An unlogged table only skips the write-ahead log. It is ordinary persistent
storage, its columns and keys are as real as any other table's, and it belongs in
the schema — on the floor it would merely be honest about a table codefit is
perfectly able to read.

`reCreateTable` and `reCreateTablePartitionOf` both take the widening (PostgreSQL
admits `CREATE UNLOGGED TABLE c PARTITION OF p`), and in both the prefix is
NON-capturing: `reduceCreateTable` reads `loc[2]` for IF NOT EXISTS and
`loc[4]:loc[5]` for the name, so a capturing group shifts every later span. That
is not a hypothetical — the mutation that makes it capturing panics the reducer
outright on real corpora.

### 2.5 A temporary table is WITHHELD, and that is not the same fact as unreduced

This is the load-bearing half of the decision, and the reason it needed its own
carrier rather than a reuse of `Schema.Unreduced`.

- A form the parser **cannot read** is unreduced. `Schema.Unreduced` and
  `Table.Note`'s `Reason*` vocabulary both mean exactly that, and every consumer
  reads them as "codefit was blind here".
- A temporary table the parser **can read and deliberately declines to model** is
  a different fact entirely. Nothing about it is unproven. Reporting it through
  `Unreduced` would misdescribe a SCOPING DECISION as a PARSER FAILURE — a lie in
  the opposite direction from the silence this ADR removes, and one that would
  make an agent distrust a parser that is in fact working.

Nor can the table simply be admitted. A temporary table is dropped with the
session that created it, so it is not part of the persistent schema the DB
dimension audits, and admitting it would have DB-050 affirm "table without a
primary key" over session scratch space at confidence 1.0 — precisely the false
affirmation ADR 0034 exists to prevent, arrived at from a new direction.

So: `db.Schema.Withheld []Withheld{Name, Text, Pos, Reason}`, with
`WithheldReason` a CLOSED vocabulary and a DISTINCT TYPE from `Reason`. The two
must not be assignable to each other. `Reason` answers "why could this table's
structure not be proven complete", and an answer to that question is grounds for
an absence-based rule to abstain; a withheld declaration raises no such question.
Sharing one type would invite a future reader to route one into the other's
carrier, which is exactly the misdescription this section rejects.

### 2.6 Withholding is announced, bounded, and aggregated by reason

Developer autonomy is non-negotiable in this project (CLAUDE.md): codefit may
decide, but it never decides silently. `sensors/db.withheldNote` is a THIRD
independent trace on the existing `Result.Note` channel, joined between the
measurement inventory and the schema-gate verdict — each qualifies the ones after
it. It states the count, the reason from the core's closed vocabulary, up to five
names, and the CONSEQUENCE ("absent from the schema every DB and DW rule reads,
so no rule saw them at all"). It is empty when nothing was withheld, and a
migration suite that stages 200 temporary tables is ONE line with `(+195 more)`,
never 200.

A withheld form whose name the parser cannot read faithfully is identified by
`file:line` instead of a guessed name — §2.2's boundary applies here too.

### 2.7 T-SQL's `#` prefix is IN scope, through a per-dialect DATUM

T-SQL has no `TEMPORARY` keyword: it marks a temporary table by a NAME prefix
(`#` session-local, `##` global), so the keyword widening does nothing for it and
a separate recognition is needed. `Dialect.HashPrefixedTempTables` is true only
for SQLServer, following ADR 0022's data-not-code discipline.

The datum is what keeps the recognition honest in the OTHER direction: `#` opens
a line comment in MySQL and is a perfectly legal quoted-identifier character in
PostgreSQL. Reading it as "temporary" everywhere would be codefit deciding, on a
lexical accident, that a persistent table is not part of the schema — a silent
deletion, the exact failure this ADR exists to end. A PostgreSQL
`CREATE TABLE "#weird" (…)` therefore lands on the table-shaped-head floor (its
name is outside the reducer's identifier class, which IS a read failure), not in
`Withheld`.

Widening `reCreateTable`'s name class to include `#` was rejected: it would make
`#`-named tables ORDINARY tables in every dialect, the opposite of what T-SQL
means by one.

### 2.8 Nested `CREATE TEMPORARY TABLE` in a routine body stays safe — by TWO guards

ADR 0041 §"Alternatives rejected" relies on a nested temporary table inside a
procedure body being legitimate body CONTENT (real Pagila and Sakila procedures
contain one; both are vendored here). That is verified, not assumed, and the
verification produced a finding worth recording:

- removing the `^` anchor from `reSessionScopedTable` alone does **not** break
  it — the routine branch (`reRoutine`/`reTrigger`) claims the statement first,
  so the unanchored regex is never consulted;
- removing the anchor **and** hoisting the withholding branch above the routine
  branch does break it, on both fixtures.

What is locked is therefore the CONJUNCTION — dispatch order plus anchoring —
and the test says so rather than claiming the anchor is the guard.

## Alternatives rejected

- **Report temporary tables through `Schema.Unreduced`.** Rejected, §2.5: it
  reports a scoping decision as a parser failure.
- **Add a `Reason*` constant instead of a new closed vocabulary.** Rejected,
  §2.5: the two answer different questions and one of them gates rule abstention.
- **Admit temporary tables to the model.** Rejected, §2.5: DB-050 would affirm
  over scratch space at confidence 1.0.
- **Enumerate the six known keywords instead of writing a shape.** Rejected: it
  fixes six cases and leaves the class open, which is the whole defect.
- **Let the catcher extract a table name.** Rejected, §2.2 — a fabrication the
  completeness contract structurally cannot catch.
- **A three-word modifier window.** Rejected, §2.3, on measurement.
- **Widen `reCreateTable`'s name class for `#`.** Rejected, §2.7.

## Consequences

- The twelve measured-silent forms all reach the developer. The same probe after
  the change: `UNLOGGED` → one modeled table with its key; the TEMP family and
  T-SQL's `#`/`##` → `tables=0` with a withholding note naming the table;
  `FOREIGN TABLE`, CTAS and the out-of-class name → one `Schema.Unreduced` entry
  with its `file:line` in the note.
- **29 corpora, ZERO delta** (the 26-corpus external survey plus three jobs
  covering every `.sql` under this repo's `testdata`): tables, structure-proven
  counts, columns, foreign keys, indexes, views, procedures, triggers, paradigm,
  every emitted item and the scan note are identical. The three schema goldens
  gained ONE additive key (`Withheld`) and nothing else.
- A zero delta is exactly what a broken harness produces, so the measurement was
  proven sensitive by positive control: the three-word window moves
  `adventureworks-oltp-pg`; a MANDATORY `UNLOGGED` prefix moves 22 of 29 corpora
  with table counts collapsing.
- **No corpus could have caught a regression here**, which is its own finding:
  the forms have zero top-level prevalence across all 29. Two authored fixtures
  are therefore the only control —
  `internal/providers/sqlddl/testdata/pg_constructed_table_shaped_heads.sql` and
  `.../tsql/constructed_temp_table_names.sql` — and both are registered in the
  schema-gate corpus table, whose `tables` column is an ADMISSION lock
  (mutation-proven) rather than a withholding one.
- **T-SQL's `#` form is proven by constructed DDL only.** It DOES occur in the
  survey — `dw-gravity`'s `DWH Scripts/1.1_CreateDimDate.sql` declares
  `CREATE TABLE #tmpHoliday(…)` at the top level — but that occurrence is
  unreachable for an unrelated, PRE-EXISTING reason, confirmed through the real
  parser: the preceding `UPDATE` is unterminated, so the whole run is one
  statement whose head is `UPDATE`. That is the run-on class, not this one.
- **Withholding shrinks the modeled table count**, which can move a small schema
  below the schema gate's `minJudgeableTables` floor of 3 and silence every
  warehouse signal. That is the correct reading — scratch space is not evidence
  about whether a schema is a warehouse — but it is a real consequence, measured
  on the authored T-SQL fixture and recorded in its corpus row.
- **A later `ALTER TABLE` or `CREATE INDEX` naming a withheld temporary table**
  still materializes a phantom table carrying `ReasonTableNeverDeclared`, exactly
  as before. Withholding removes the `CREATE` from the model; it does not teach
  the other branches that the name is temporary.
- Two gaps stay open and stay declared: MariaDB's three-word
  `CREATE OR REPLACE TEMPORARY TABLE`, and `CREATE SCHEMA s CREATE TABLE …`.

## Related

ADR 0018 (the declared subset), 0022 (per-dialect data, not code), 0034 (the
completeness contract this extends), 0038/0039 (its later refinements), 0041 (the
found-residual disposition this one mirrors for a handed statement), 0028
(coverage honesty).
