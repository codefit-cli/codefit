# ADR 0022 — Per-dialect DATA descriptor for SQL-DDL parsing (MySQL + T-SQL)

**Status:** Accepted · **Date:** 2026-07-06 · **Phase:** 2.1 (SQL dialect support)

## Context

The DB audit dimension parsed PostgreSQL DDL only. Any codebase on MySQL or SQL
Server was under-audited — worst case, statements were silently mis-tokenized and
skipped, under-reporting auditable surface. Adding dialects must NOT fork the
core, the sensors, the reducer, or the MCP adapter: the layering doctrine (ADR
0016 dimension lifecycle, ADR 0014 neutral model) requires that a new dialect be
added without touching dialect-agnostic code. The parser is a hand-written
`go/ast`-style splitter + incremental reducer (ADR 0018), not tree-sitter, and
must stay CGO-free.

## Decision

### Dialect support is a per-dialect DATA descriptor, not branching code

A `sqlddl.Dialect` value is pure DATA:

```
Dialect{
  Name
  LineComments []LineComment{Prefix; RequireBoundaryAfter}
  IdentQuotes  []QuotePair{Open, Close rune; Doubling bool}
  DoubleQuoteIsString bool
  DollarQuoting bool
  TypeMap  map[string]db.Type
  Modifiers map[string]bool
}
```

It is consumed by ONE shared tokenizer (`split.go`) and ONE dialect-free reducer
(`reduce.go`). There are **no `if dialect.Name == …` branches**: every dialect
difference is expressed as descriptor data the shared code reads. `Postgres()`,
`MySQL()`, and `SQLServer()` are the three descriptors today.

### Quoting is canonicalized to ANSI `"` at tokenization

Backtick (MySQL) and `[bracket]` (T-SQL) identifiers, with their doubling-escape
rules, are re-emitted as ANSI `"…"` during tokenization. The reducer only ever
sees ANSI quoting, so it stays dialect-free — and the PostgreSQL path is
**byte-identical** to its pre-change behavior (locked by the Pagila golden).

### The dialect is bound at construction — ParseSchema's signature is unchanged

`sqlddl.New(WithDialect(d))` binds the dialect; `sqlddl.New()` with no options is
Postgres (backward-compatible default). `ParseSchema`/`SchemaParser` signatures
are **unchanged** — the dialect is not threaded through the call. This defers, and
does not force, the ADR-0014 `SchemaParser` convergence. The MCP adapter
(`internal/mcp/schemaparser.go`) is the single place that maps `database.type` →
descriptor; `sqlite` returns an explicit "not supported yet" note (never a silent
Postgres parse), and an unrecognized `database.type` returns an explicit note
rather than silently guessing Postgres.

### Two data-driven refinements

- `LineComment.RequireBoundaryAfter`: MySQL's `--` opens a comment only when
  followed by whitespace/EOL (unlike PostgreSQL's unconditional `--`). This is a
  descriptor flag, not a tokenizer branch.
- KEY/INDEX-inline vs. a column named `key`/`index` is discriminated by the
  dialect's `TypeMap` (is the token after the keyword a known type → column; else
  → inline index), not by paren presence — so `key varchar(255)` stays a column
  while `KEY idx (col)` stays an index.

### Routine bodies: retreat to a documented limit, not a speculative guard

An early phantom-table guard for T-SQL `GO`-batched stored-procedure/trigger
bodies (skip-to-`END`) proved unsound — it matched `BEGIN`/`END` in string
literals, was not depth-counted, and was not reset between files, corrupting valid
input on every dialect. It was removed. Building a *sound* T-SQL routine-body
guard now would be speculation: no dogfooded schema exercises a `CREATE TABLE`
inside a T-SQL routine body. Per the honesty doctrine, this is left as a
**documented known limit** (below), to be revisited when a real schema justifies a
sound guard. MySQL routine bodies wrapped in `DELIMITER //` … `//` are handled
correctly (the body merges into one non-reduced statement).

## Alternatives considered

- **`if`/`switch` by dialect inside the tokenizer/reducer** — rejected: forks the
  dialect-agnostic core, violating the layering doctrine (ADR 0016). Adding a
  dialect would then touch shared code, exactly what the design forbids.
- **A tree-sitter grammar per dialect** — rejected: pulls in CGO/complexity; the
  hand-written splitter + neutral reducer (ADR 0018) is retained.
- **Threading the dialect through `ParseSchema`/`SchemaParser`** — deferred: would
  force the ADR-0014 `SchemaParser` convergence prematurely; construction-time
  binding gets dialect support without a signature change.

## Consequences

- Adding a dialect = writing one `Dialect` descriptor + wiring its
  `database.type` string in the adapter. Core, sensors, reducer, and the DB rules
  (dialect-agnostic, over the neutral `db.Schema`) are untouched. No core
  enrichment: every dialect type maps onto an existing `db.Type`; unmapped
  keywords fall back to `db.TypeUnknown` (honest, not guessed).
- The PostgreSQL path is unchanged and byte-identical.

### Known limits (disclosed, not silent)

- **(a)** A T-SQL `GO`-batched stored-procedure/trigger body containing a
  `CREATE TABLE`-shaped fragment MAY surface as a spurious top-level table (routine
  bodies are not modeled). MySQL `DELIMITER //`-wrapped bodies are NOT affected.
- **(b)** MySQL `DELIMITER` directives are recognized only when the argument is
  punctuation (`//`, `$$`, …); a word-based delimiter such as `DELIMITER GO` is not
  recognized.
- **(c)** The T-SQL `GO` batch separator is recognized only when a line is exactly
  `GO`; a column literally named `go` alone on its own line would collide
  (vanishingly rare).
- **(d)** An inline index whose NAME is itself a type keyword (e.g. `KEY int (col)`
  — an index named `int`) is read as a column, because the KEY/INDEX-vs-column
  discriminator trusts a type-named token. Pathological, accepted.

### Assumptions

- MySQL parsing assumes `ANSI_QUOTES` is OFF (a bare `"` is a string literal, not
  an identifier quote).
- One dialect per project (the parser binds a single dialect at construction). A
  project mixing `.prisma` and `.sql` schema inputs remains out of scope.

## References

- ADR 0014 (neutral DB model), ADR 0016 (dimension lifecycle), ADR 0018 (SQL-DDL
  parser: declared subset + incremental reducer + input selection).
