# ADR 0018 — A hand-written SQL-DDL parser: declared subset, incremental reducer, input-based selection

**Status:** Accepted · **Date:** 2026-07-04 · **Phase:** 3 (DB sensor — SQL-DDL schema source)

## Context

Slice 1 (ADR 0014) gave codefit a neutral schema model and a Prisma parser behind
`providers.SchemaParser`. The db dimension now needs a second schema source:
SQL-DDL migrations (Flyway). The incremental dogfood is PlantaLinda's 45
PostgreSQL migrations (37 tables, ~90 FKs, ~96 indexes, and — verified — zero
views/procs/triggers); Pagila provides views/functions/triggers with
dollar-quoting as a test fixture.

Three facts shape the design: SQL is a far larger grammar than the Prisma DSL; a
migration set expresses a schema INCREMENTALLY (a table born in one file, altered
in several later ones), not as one declarative snapshot; and a SQL schema is
ORTHOGONAL to the app's backend language (PlantaLinda is Java, for which
`providerForLanguage` returns nil).

## Decision

### A hand-written parser over a DECLARED SUBSET

The parser is hand-written (no SQL parsing library — keeps `CGO_ENABLED=0`, no
heavy dependency) and parses only the subset the real dogfoods use; everything
outside is skipped-and-declared as a limit, each locked by a test — the same
pattern as the Prisma parser's `@@map`/`@default` limits (ADR 0014). Maintenance =
grow the subset when a real project brings something new, with a test that proves
it. It grows by dogfooded demand, never by speculative completeness. The subset is
the maintenance contract. Declared limits: CHECK constraints, `ON DELETE/UPDATE`
actions, partial-index `WHERE`, `CREATE TYPE`/enum, `ALTER COLUMN`, all DML, and
the bodies/columns of views/procs/triggers.

### A dollar-quoting-aware statement splitter

Statements are split by a state machine that respects single-quoted strings,
quoted identifiers, line/block comments, and — critically — dollar-quoted blocks
(`$$…$$` and `$tag$…$tag$`, matched on the exact tag). A naive `split(';')` breaks
inside PL/pgSQL `DO $$ … $$` blocks (nine in PlantaLinda; `$$` and `$_$` function
bodies in Pagila), whose internal semicolons must not cut. This is the technical
core of the parser.

### An incremental reducer

The parser applies statements IN ORDER over a mutable schema builder: `CREATE
TABLE` creates, `ALTER ADD/DROP COLUMN` mutates, `CREATE INDEX` indexes, `ADD
CONSTRAINT` constrains, `DROP TABLE` removes — accumulating to the final state.
Order is the Flyway version order of the migrations. This is what is genuinely new
versus the Prisma parser (a single declarative file).

**`IF NOT EXISTS` skips the WHOLE statement (first wins).** When an entity already
exists, an `IF NOT EXISTS` create is ignored in its entirety — the first
definition wins, exactly as PostgreSQL behaves. The second body is NOT merged; a
Frankenstein schema that diverges from the real one would be worse than either
version. Locked by a test (two `CREATE TABLE` of the same name with different
columns → only the first body survives).

### Flyway ordering — integer versions, dotted declared out

A directory `schema_path` is expanded (filesystem-side, ADR 0014) to its `*.sql`
files ordered by Flyway version: `V<n>__…` sorts by the INTEGER `<n>` (not lexical
— `V2` before `V10`). PlantaLinda uses only integer versions (verified). DECLARED
LIMIT: dotted versions (`V1.1`) do not match the integer pattern and fall to the
lexical bucket after all versioned files; Flyway `R`/`U` prefixes are out of scope.
Both are locked by tests, so a wrong-order reconstruction can never happen
silently. Intentional version gaps (V17/V45 skipped) are irrelevant — order does
not depend on the count.

### Parser selection by INPUT, and a narrowed sensor

The schema parser is chosen by the input's shape (`.prisma` → Prisma parser;
`.sql` or a migration directory → SQL-DDL parser), not by the app language — a
schema is orthogonal to the backend (there is a precedent: the surface tools
resolve a provider by file extension). The MCP adapter (the single place that maps
input → concrete parser) resolves it and injects it.

The db sensor's dependency is NARROWED from `LanguageProvider` to the capability it
actually uses, `providers.SchemaParser`. This makes the "no parser for this input"
case a compile-time concern of the adapter's resolver, not a runtime branch in the
sensor (one sensor test relocates to the resolver accordingly). The sensor keeps
its honest not-measured/error states (disabled, no `schema_paths`, unreadable file,
parse failure). The security path (`providerForLanguage`) is untouched — no
regression. A project mixing `.prisma` and `.sql` is a declared out-of-scope limit.

### The SQL-DDL parser is a schema-only "provider"

It lives in `internal/providers/sqlddl` and implements `providers.SchemaParser`
only — not `LanguageProvider` (SQL is not a programming language codefit audits
for security/surface). Parsing is provider territory (ADR 0014); the neutral model
and the rules stay in the core.

## Consequences

- A new `internal/providers/sqlddl` parser reconstructs table/column/index/FK/PK/
  UNIQUE state from ordered SQL-DDL; the eight existing schema-only rules run over
  the reconstructed schema unchanged. Dogfood: PlantaLinda reconstructs to 37
  tables with no crash on its DO blocks or interleaved DML.
- View/Procedure/Trigger are populated with what the model supports today
  (Name/Pos, and Table for triggers, parsed from `ON`); their bodies, view
  columns, and the materialized flag are NOT captured — a declared limit for the
  future DB-020..041 rules. (PlantaLinda has none; Pagila exercises them.)
- No `core/db` enrichment in this slice; the reducer fills the existing model.
- Column positions inside a multi-line `CREATE TABLE` are computed per line so
  distinct columns get distinct fingerprints.
- PlantaLinda becomes the second real schema for calibrating the deferred DB-052/
  DB-053 softeners; the calibration decision is taken with the evidence, with the
  owner.

## Related

- ADR 0014 — Prisma parser / neutral model (the sibling parser; enrich core not rule).
- ADR 0015 — DB rules as neutral core functions (rules unchanged; parser only parses).
- ADR 0016 — dimension lifecycle (this parser advances the db dimension toward close).
