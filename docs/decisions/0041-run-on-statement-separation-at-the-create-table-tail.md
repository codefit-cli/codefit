# ADR 0041 — A run-on statement is separated at the CREATE TABLE tail, and its residue is never silent

**Status:** Accepted · **Date:** 2026-08-02 · **Phase:** 2 (RF-03 OLAP closure)

**Closes known limit (9) of `internal/core/dbcoverage/dbcoverage.go` and
extends [ADR 0034](0034-neutral-model-completeness-contract-for-structure.md)
§2.4.** 0034's invariant, dispositions and carriers are untouched. What this ADR
adds is the disposition for a statement the reducer had to *find* rather than
receive from a delimiter — a case 0034's declared-skip rule did not distinguish,
and the one remaining path through this parser that lost table structure with no
trace at all.

## Context

`split()` is a tokenizer: it ends a statement at the dialect's active terminator,
at a `DELIMITER` directive, at a standalone `GO` batch line, or at EOF. A
statement boundary that is expressed by none of those is invisible to it — and
T-SQL makes the terminator optional, so two `CREATE TABLE` statements written
back to back with no `;` and no `GO` between them are valid input, not malformed
DDL.

Before this change the reducer read the FIRST table of such a run and discarded
everything after it, with no trace of any kind: no `Schema.Unreduced` entry, no
`Table.Note`, and the host still `StructureProven()`. That is blindness with no
trace — precisely the outcome ADR 0034 exists to prevent, and the one place in
this parser where it still happened. Every other known limit either routes to the
abstention floor or is declared.

**Measured, not inferred.** A public warehouse script (`kenapDW/CREATE_DW.sql`,
not vendored here) declares 7 `CREATE TABLE` statements, contains zero `;`, and
has one lowercase `go` on line 2 that separates `use KENAP` from the rest and
nothing else. Through the real parser it reduced to:

```
tables=1  schemaUnreduced=0
  Dim_Price_Table  cols=2  StructureProven=true  Note=""
```

Six declared tables, 39 declared columns and 7 declared foreign keys, gone
without a record.

**The lowercase `go` was not the cause.** `reGoBatchSeparator` has been
case-insensitive (`(?i)`) since it was written, so `go` was already accepted and
already flushed `use KENAP`. Accepting lowercase `go` is not a fix for anything
here, and widening `GO` recognition further would be a *risk*, not a fix: the
regex requires the whole trimmed line to be the word, which is what keeps a
column or an identifier named `go` from cutting a statement in half.

## Decision

### 2.1 The boundary is DERIVED at the tail, never guessed inside the statement

`applyCreateTable` no longer reduces the statement it is handed. It first calls
`cutRunOn`, which:

1. locates the table's body with `balancedParen` — the same primitive the column
   loop and the partitioning reader already trust, which computes where the body
   ends rather than estimating it;
2. scans only the TAIL (everything after that matching `)`) for a statement head;
3. cuts there, returning the host truncated at the cut plus the remainder as a
   statement in its own right.

Nothing inside the body is ever examined for a boundary. A "widened regex" that
scans a whole statement for the next `CREATE` is exactly the shape that once
truncated a table at its first `)` and invented a phantom column in this parser;
the tail restriction makes that class unreachable here.

### 2.2 THREE keywords, because only three cannot legally appear in a tail

`reRunOnStatementHead` is `\b(?:create|alter|drop)\b`. These are the only
statement kinds that can affect table structure, and none of them is legal at the
top level of a `CREATE TABLE` tail in PostgreSQL, MySQL or T-SQL. Every tail
clause those dialects DO admit starts with something else, measured over 399
`CREATE TABLE` statements in 26 external corpora:

| tail | count |
|---|---|
| (empty) | 347 |
| `ON "PRIMARY"` / `ON "PRIMARY" TEXTIMAGE_ON "PRIMARY"` | 34 |
| `ENGINE=InnoDB DEFAULT CHARSET=utf8` | 16 |
| `PARTITION BY RANGE (payment_date)` | 1 |
| **a top-level statement keyword** | **1** (the run-on) |

Widening the set to a general statement vocabulary was rejected on that same
table: `WITH` and `SET` ARE legal tail syntax (`WITH (autovacuum_enabled = off)`,
`WITH (DATA_COMPRESSION = PAGE)`), so admitting them would cut a table's own
options away from it and could demote or mis-read tables that are read correctly
today. The narrower set costs a residual that begins with `SELECT` or `INSERT` —
which stays exactly as invisible as it is today, unchanged by this ADR — and buys
a rule with no false-cut surface.

### 2.3 Three exclusions are the fabrication guard, and each is a real shape

`firstTopLevelStatementHead` ignores a match that is inside a single-quoted
string, inside a canonical `"..."` identifier, or at paren depth > 0. It is a
SEPARATE walk from `firstTopLevelMatch`, not a widening of it: that function is
consulted by partitioning over text it must keep reading byte-identically, and it
does not track identifier quoting.

Each exclusion corresponds to a shape a dialect actually writes — MySQL's
`COMMENT='…'`, T-SQL's `ON [filegroup]` (which `split()` re-emits as `ON
"filegroup"`), and both dialects' `WITH (…)` — and each is locked by a test that
was mutation-proven to FABRICATE a phantom `ghost` table when its guard is
removed. The word boundary is the fourth guard and the one that does the most
work in practice: `CREATEDFILEGROUP`, `DROP_EXISTING`, `AUTO_CREATE` and
`toast.create_table` are all defeated by it (`_` is a word character), which is
why the paren case had to be written as a deliberately adversarial input and is
labelled as such in the test.

### 2.4 The host is truncated, so it cannot absorb the residual's tail clauses

`cutRunOn` returns the host with its text cut at the boundary. This is not
cosmetic: `reduceCreateTable` reads the tail for partitioning, so a host that
kept the residual attached would read the RESIDUAL's `PARTITION BY` clause and
report it as its own — fabricating partitioning on a table that declares none.
Locked by `TestSQLDDL_RunOn_HostNeverAbsorbsTheResidualsTailClauses`, and
mutation-proven: without the truncation, `plain` reports
`{Strategy: range, Key: [ts]}` read off the NEXT statement.

### 2.5 The host is NOT demoted

A host whose body was read in full from a balanced paren, and which was cut at a
keyword that cannot belong to it, is genuinely complete. Marking it unproven
because an unreducible statement was glued to it would be the false demotion ADR
0034 §2.4 warns about — the same reasoning that keeps `CHECK` and `OWNER TO` from
muting a table.

### 2.6 A found residual that nothing recognizes is DECLARED, never skipped

`apply` now returns whether the dispatch recognized the statement. Every
delimited caller ignores it: an out-of-subset statement KIND is a declared skip
(ADR 0034 §2.4), and `TestSQLDDL_OutOfSubsetStatement_RecordsNothing` still locks
that. The run-on path does NOT ignore it. A residual the boundary rule FOUND but
no branch reduces (`CREATE TYPE`, `CREATE SEQUENCE`, `ALTER SCHEMA`, …) is
appended verbatim to `Schema.Unreduced`, reaching the agent through the per-scan
inventory (`sensors/db.Result.Note`, ADR 0034 §2.8).

This is the load-bearing half of the decision, and it is what makes recovery
admissible at all: the change never recovers some tables while losing others in
silence. Everything the boundary rule detects is either reduced into the model or
recorded on the abstention floor.

`Schema.Unreduced` rather than the host table's `MarkUnproven` is deliberate: the
schema-level channel already exists for exactly this ("recognized as
table-affecting but not attributable to a specific table"), and it declares the
loss without demoting a table that was read correctly.

### 2.7 Recursion, and why it terminates

A recovered residual is dispatched through `apply`, so a residual that is itself
a run-on re-enters `applyCreateTable` and a run of N tables recovers all N. The
boundary always lies strictly after the host's body, so the residual is strictly
shorter than its host and the recursion cannot loop. Order is preserved — the
host is reduced BEFORE its residual — because this reducer is incremental;
locked by `TestSQLDDL_RunOn_AppliesTheResidualAfterTheHost`, which was
mutation-proven (applying the residual first makes an `ALTER TABLE` materialize
its table before its own `CREATE TABLE`, which the reducer then treats as a
duplicate declaration and drops).

## Alternatives rejected

- **Detect and DECLARE only** (route the run-on to the abstention floor without
  splitting). Strictly honest and smaller, and it was the fallback if the
  boundary had turned out to be a guess. It was rejected once the measurement
  came back: the detection needed for it IS the boundary needed for splitting, so
  declaring-only pays the same false-cut risk and returns nothing — the affected
  project would learn "something is wrong here" while the whole DB dimension goes
  quiet on it (every DW rule abstains on an unproven table).
- **A general statement vocabulary** (`select`, `insert`, `with`, `set`, `if`).
  Rejected, §2.2 — `WITH` and `SET` are legal tail syntax.
- **Whitelisting the recognized tail-option clauses and declaring anything left
  over.** Rejected: the option vocabulary is open-ended and per-dialect
  (`ENGINE=`, `AUTO_INCREMENT=`, `ROW_FORMAT=`, `TABLESPACE`, `INHERITS`,
  `WITHOUT OIDS`, `TEXTIMAGE_ON`, `DATA DIRECTORY`, …), so the first unlisted
  option would demote a table that is read in full — the false-demotion trap
  again, this time at corpus scale.
- **Detecting the boundary in `split()`.** Rejected: `split()` owns quoting and
  comments and knows no grammar. The rule needs `balancedParen`, and applying it
  to every statement rather than to a `CREATE TABLE` tail would reach into
  routine bodies, where a nested `CREATE TEMPORARY TABLE` is legitimate content
  (measured: real Sakila and AdventureWorks procedure bodies contain them).
- **Marking the host unproven whenever a run-on is detected.** Rejected, §2.5.
- **Accepting lowercase `go` as the fix.** Rejected as a non-fix: it was already
  accepted, and it is not what separates the statements.

## Consequences

- **`dw-kenap`: 1 table → 7**, 0 → 7 foreign keys, 2 → 41 columns, all 7
  `StructureProven`, `Schema.Unreduced` still empty; paradigm reads `olap`
  instead of `oltp`. Line numbers, column names and foreign keys were verified
  against the source DDL statement by statement.
- **25 of the other 26 external corpora are byte-identical** on tables, proven
  count, columns, foreign keys, indexes, views, procedures, triggers, paradigm,
  every emitted item and the scan note. No golden was regenerated — the repo's
  own 18 fixtures contain no run-on tail under any of the three dialects.
- **`apply` returns `bool`.** The only production caller (`ParseSchema`) ignores
  it; the run-on path is the only consumer.
- **Two adjacent gaps were measured and deliberately NOT addressed**, recorded so
  the next reader does not have to rediscover them:
  - `CREATE SCHEMA x CREATE TABLE … CREATE TABLE …` (the SQL-standard schema
    element list, which AdventureWorks-for-PostgreSQL uses for all 68 of its
    tables) matches no dispatch branch, so those tables never enter the model.
    That loss is NOT silent — all 70 of its tables are materialized by later
    `ALTER TABLE` statements and carry
    `ReasonTableNeverDeclared` — so it is outside this ADR's scope, which is
    silence.
  - A `CREATE TABLE` body item missing its separating comma before a table-level
    `PRIMARY KEY` reads the constraint as an inline modifier of the preceding
    column, so the composite key is replaced by a single-column one while the
    table stays `Complete=true`. It is a FABRICATION of the ADR 0034 §2.6 class,
    it is PRE-EXISTING and delimiter-independent (reproduced with an ordinary
    `;`-terminated statement), and this change only made it visible on
    `dw-kenap`, whose `Fact_Reservation` reports `pk=[Profit]` where the DDL
    declares a six-column key. Characterized in
    `internal/providers/sqlddl/fabrication_test.go` and declared in the coverage
    manifest.

## Related

ADR 0018 (the declared subset), 0022 and 0027 (what `split()` does and does not
cut), 0034 (the completeness contract this extends), 0028 (coverage honesty).
