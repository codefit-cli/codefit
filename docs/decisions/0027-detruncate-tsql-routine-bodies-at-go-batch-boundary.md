# ADR 0027 — De-truncate T-SQL routine bodies at the GO batch boundary

**Status:** Accepted · **Date:** 2026-07-19 · **Phase:** 2.3

## Context

ADR 0025 shipped `db.Body` with a tokenizer-derived `Complete` flag so a rule
can never affirm over truncated text. It also documented a real limit: T-SQL
has **neither** PostgreSQL's dollar-quoting **nor** MySQL's `DELIMITER`
directive to protect a routine body's internal `;`s — split() (ADR 0018) cut a
multi-statement T-SQL `BEGIN … END` body at its **first internal `;`**,
captured `Complete: false` with a Note, and silently dropped everything past
the cut. This was the correct, honest behavior GIVEN the limit, but it was
still a limit: DB-030/031/040/041-class routine-body rules cannot reason about
a body they never fully saw, no matter how honestly the truncation is
disclosed.

Separately, ADR 0022's Unit I rework removed an unsound phantom-table guard
(`inRoutineBody`) that tried to model T-SQL routine bodies by counting
`BEGIN`/`END` as raw text — it matched those keywords inside string literals,
was not depth-counted, and leaked state across files. Its removal was correct,
but it left a documented known limit (a): a `CREATE TABLE`-shaped fragment
inside a GO-batched T-SQL routine body could surface as a spurious top-level
table, because nothing kept the body's internal `;`s from being tokenized as
top-level statement terminators.

T-SQL DOES have one boundary the tokenizer already recognizes unconditionally:
the `GO` batch separator (`split.go`'s `matchGoBatchSeparator`), a
client-tool/`sqlcmd` convention, not part of the SQL grammar in any dialect.
Every T-SQL routine in practice ends its batch at `GO` (or EOF, for the last
batch in a file).

## Decision

### Capture the body to GO/EOF, expressed as dialect DATA, consumed via
### routine-head recognition — never by counting BEGIN/END

`Dialect` gains `RoutineBodyEndsAtBatchSeparator bool` (`dialect.go`), `true`
only for `SQLServer()`; `Postgres()` and `MySQL()` set it explicitly `false`
for parity with ADR 0022's per-dialect-descriptor discipline (a new dialect
value, never a branch in the shared tokenizer/reducer).

`split()` consults this datum at the existing generic `;`-terminator case: for
a dialect with `RoutineBodyEndsAtBatchSeparator == true`, the FIRST internal
`;` of a statement decides, once, whether the accumulated buffer is a
recognized `CREATE FUNCTION`/`PROCEDURE`/`TRIGGER` head — via `isRoutineHead`,
a new `reduce.go` helper that is nothing more than `reRoutine.MatchString ||
reTrigger.MatchString` (the SAME head regexes `apply()` already anchors
against). If it is a routine head, the `;` is written into the buffer instead
of flushing, and the verdict is cached (`headDecided`/`routineHead`, reset on
every `flush`) for every subsequent `;` in the same statement — so the whole
body accumulates as ONE `stmt` that flushes later at the existing `GO` branch
(`termGoBreak`) or at EOF (`termEOF`), never at an internal `;`. This is
**head-recognition, not block-counting**: it never inspects `BEGIN`/`END`,
never touches string-literal content specially beyond what `split()` already
does for every dialect, and cannot repeat ADR 0022's unsound guard — the very
regexes it reuses are the reducer's own anchored, statement-start-only
patterns, not a scan for keywords anywhere in the text.

`reduce.go`'s existing completeness formula (ADR 0025) is REUSED UNCHANGED:
`complete := st.term != termSemicolon || st.quotedBlockSeen`. A body that now
flushes at `termGoBreak` or `termEOF` is `Complete: true` with no Note — the
formula already produced the right answer for those terminator kinds; nothing
in `reduce.go` needed to change beyond the new `isRoutineHead` helper.

PostgreSQL and MySQL are untouched: `RoutineBodyEndsAtBatchSeparator: false`
means the new branch in `split()` never triggers for them, so their
dollar-quoting/`DELIMITER` paths are byte-identical to before this ADR. The
Pagila (PostgreSQL) and Sakila (MySQL) goldens passing byte-identical with
zero `-update` is the gate that proves this.

### Side effect: closes ADR 0022 known-limit (a)

Because the whole routine body — including any in-body `CREATE TABLE`-shaped
fragment — is now accumulated as text inside ONE statement rather than being
tokenized as separate top-level statements, the fragment is absorbed into
`Procedure.Body`/`Trigger.Body` and can no longer surface as a spurious
top-level table. This closes ADR 0022's known limit (a) as a side effect of
de-truncation, without resurrecting a `BEGIN`/`END` counter.

## Declared limits

1. **No trailing `GO`.** A T-SQL routine with no trailing `GO` followed by
   another statement will absorb that statement into its body — this is
   invalid T-SQL batching in practice (`sqlcmd`/SSMS require `GO` between
   batches), and it is the intentional trade of using the `GO`/EOF boundary: a
   boundary this cheap and this reliable in real T-SQL DDL is worth a
   pathological-input edge case. A `;` appearing BEFORE a recognizable routine
   head (e.g., a statement `isRoutineHead` does not match) degrades to
   today's pre-ADR-0027 truncation behavior — no worse than status quo.
2. **The split→reduce seam is intentional.** `isRoutineHead` in `reduce.go`
   is deliberately consulted by `split.go` — a layering exception to
   `split()` being the tokenizer and `reduce.go` being the reducer. This seam
   is REQUIRED for T-SQL de-truncation to work at all: `split()` cannot know
   whether accumulated text is a routine head without the same regexes
   `reduce.go`'s `apply()` anchors against. A future refactor must NOT
   separate `reRoutine`/`reTrigger`/`isRoutineHead` from this shared reach, or
   T-SQL de-truncation silently regresses back to first-`;` truncation.

## Alternatives considered

- **Count `BEGIN`/`END` depth to find the real end of the body** — rejected:
  this is exactly the unsound guard ADR 0022 removed (string-literal false
  matches, no depth counting done safely, cross-file state leaks). Reusing
  head-regex recognition instead of block-counting was a binding constraint
  from the start.
- **Add a T-SQL-specific branch in `split()`** — rejected: ADR 0018/0022
  established one shared, dialect-free tokenizer and reducer, driven by
  per-dialect DATA. `RoutineBodyEndsAtBatchSeparator` is that data; the
  `isRoutineHead` seam is the minimal code needed to consume it without a
  `dialect.Name == "sqlserver"` branch.
- **Leave the T-SQL truncation as a permanent documented limit** — rejected:
  ADR 0025 accepted it as an honest interim state, but a real, cheap boundary
  (`GO`) already existed in the tokenizer; not using it left routine-body
  rules (DB-030/031/040/041) permanently blocked on T-SQL for no necessary
  reason.

## Consequences

- T-SQL multi-statement procedure and trigger bodies are now captured WHOLE
  to the GO batch separator (or EOF), `Complete: true`, matching the
  trustworthiness PostgreSQL (dollar-quoted) and MySQL (`DELIMITER`-wrapped)
  bodies already had.
- ADR 0022 known-limit (a) — the in-body `CREATE TABLE`-shaped phantom-table
  leak — is closed as a side effect; the `TestTSQLGoBatches_
  RoutineBodyAbsorbsInBodyCreateTable` fixture locks this.
- PostgreSQL and MySQL parsing is provably unaffected: golden fixtures
  (Pagila, Sakila) pass byte-identical with zero `-update`.
- Routine-body rules (DB-030/031/040/041, deferred by ADR 0025) are now
  unblocked on T-SQL specifically for the common, well-formed-batch case; the
  two declared limits above remain.

## Related

- ADR 0018 — SQL-DDL splitter + incremental reducer (the `;`-oriented cut
  this ADR partially de-truncates for T-SQL).
- ADR 0022 — per-dialect DATA descriptor discipline; the removed unsound
  `BEGIN`/`END` guard this ADR must not repeat; known-limit (a), closed here.
- ADR 0025 — routine-body capture + tokenizer-derived `Complete` flag (the
  formula reused unchanged by this ADR).
