# ADR 0025 — Routine-body capture with a tokenizer-derived `Complete` flag

**Status:** Accepted · **Date:** 2026-07-17 · **Phase:** 2.2 (DB debt Slice A — Unit A body capture)

## Context

Slice A adds body text to the neutral model for views, procedures, and triggers,
so a future routine-body rule (DB-030/031/040/041, deferred to the
`routine-body-rules` slice) can read what a routine actually does. But the SQL-DDL
splitter (ADR 0018) is statement-oriented and cuts on `;`. A multi-statement T-SQL
routine body (`BEGIN … stmt; stmt; … END`) is therefore captured **truncated at
its first internal `;`** — everything past the cut is unseen.

A rule reading a truncated body would lie with confidence: DB-031 ("is exception
handling present?") over a body cut before its `TRY/CATCH` would FALSELY AFFIRM an
absence that is really just unread text. ADR 0022 already learned the adjacent
lesson the hard way — an early phantom-table guard that re-scanned for `BEGIN`/`END`
matched those keywords inside string literals, was not depth-counted, and was
never reset between files, corrupting valid input on every dialect. It was removed.

So capturing the body is not enough; the model must carry **how much of it is
trustworthy**, and that signal must not repeat the removed guard's mistake.

## Decision

### The neutral `Body` carries a `Complete` flag, and truncated ⇒ never affirm

`db.Body` (`internal/core/db/db.go:162`) is `{Text string; Complete bool; Note
string}`, carried by `View`, `Procedure`, and `Trigger`. The contract is
**mechanical, not aspirational** (`db.go:153`): a `dbrules` rule reading
`Body.Text` MUST treat `Complete == false` as grounds to abstain or downgrade to a
surface item — **never** to emit a deterministic finding. This makes ADR 0004's "a
mutilated rule is worse than an absent one" enforceable at the data level: a rule
physically cannot honestly affirm over text it was told is incomplete.

### `Complete` is derived from tokenizer STATE, never from re-scanning the body

The split pass already knows, for free while it scans, the two facts that decide
completeness — they are fields on `stmt` (`split.go:14`): `term termKind` (how the
statement terminated) and `quotedBlockSeen bool` (whether a dollar-quoted /
delimited block was seen). `routineBody` (`reduce.go:143`) derives:

```go
complete := st.term != termSemicolon || st.quotedBlockSeen
```

Re-deriving completeness by re-scanning the captured text for `BEGIN`/`END` is
**deliberately forbidden** (`reduce.go:119`) — that is precisely the unsound guard
ADR 0022 removed (string-literal false matches, no depth counting, no per-file
reset). Nothing about `Complete` is discovered by re-reading the statement text
afterward (`split.go:9`); it falls out of state the tokenizer already holds.

### The limit is disclosed, not silent

The T-SQL truncation is documented in the coverage source
(`dbcoverage.go`): a multi-statement T-SQL routine body is captured truncated at
its first internal `;` and marked incomplete, and a rule like DB-031 over it would
falsely affirm. PostgreSQL and MySQL (dollar-quoted / `DELIMITER`-wrapped) bodies
set `quotedBlockSeen`, so they are captured complete. The routine-body RULES are
deferred to `routine-body-rules` precisely because this parser limit blocks them,
not because the rules themselves are hard.

## Alternatives considered

- **A sound multi-statement T-SQL body parser now** — deferred: no dogfooded
  schema yet exercises the failing case at rule level, and building a depth-counted
  string-aware routine parser is its own slice (`routine-body-rules`). Shipping
  body capture with an honest `Complete` flag unblocks the neutral model without
  betting on a parser rewrite.
- **Re-scan the captured text for `BEGIN`/`END` to judge completeness** — rejected:
  this is the exact guard ADR 0022 removed for being unsound. Reintroducing it
  one layer up repeats the bug.
- **Capture bodies with no completeness signal** — rejected: a downstream rule
  could not tell trustworthy text from truncated text and would affirm over a cut
  body — a false green (ADR 0005).

## Consequences

- The neutral model gains `Body` on `View`/`Procedure`/`Trigger` with an
  enforceable trust signal; no rule reads it yet (rules deferred), but the contract
  is locked before any rule can violate it.
- The completeness derivation adds zero re-scanning cost — it reads state the
  splitter already computes.
- The T-SQL truncation is a disclosed known limit; when `routine-body-rules` lands,
  it either builds a sound multi-statement parser or ships rules that abstain on
  `Complete == false`.

## Related

- ADR 0004 — a mutilated rule is worse than an absent one (made mechanical here).
- ADR 0005 — an honest red over a false green.
- ADR 0018 — SQL-DDL splitter + incremental reducer (the `;`-oriented cut).
- ADR 0022 — the removed unsound `BEGIN`/`END` re-scan guard this ADR must not repeat.
