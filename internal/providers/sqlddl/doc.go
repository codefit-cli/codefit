// Package sqlddl is codefit's hand-written SQL-DDL parser. It implements
// providers.SchemaParser (the second implementer after the Prisma parser), turning
// ordered SQL-DDL migrations into the neutral core/db.Schema. Today it supports
// PostgreSQL only (via the Postgres() dialect descriptor and default New()
// construction); the tokenizer and reducer are dialect-agnostic by design, but
// no other Dialect value is wired in yet.
//
// A Parser is bound to exactly one Dialect at construction time (New(opts
// ...Option), WithDialect) — see dialect.go for the descriptor shape. Two
// pieces consume it (ADR 0018, and the dialect-descriptor design): a
// statement splitter (split.go) that is aware of the dialect's comment
// styles, identifier quoting and (for PostgreSQL) dollar-quoting ($$…$$ /
// $tag$…$tag$) — so a PL/pgSQL DO block's internal semicolons never cut a
// statement — and an incremental, dialect-FREE reducer (reduce.go) that
// applies statements IN ORDER over a mutable schema (CREATE TABLE creates,
// ALTER ADD/DROP COLUMN mutates, CREATE INDEX indexes, ADD CONSTRAINT
// constrains), consulting the dialect only for type/modifier vocabulary
// (dialect.TypeMap / dialect.Modifiers). The splitter re-emits every quoted
// identifier canonicalized to ANSI "..." as it tokenizes, so the reducer's
// regexes never need to know the source dialect's quoting style.
//
// It parses a DECLARED SUBSET; everything outside (CHECK constraints, ON DELETE
// actions, partial-index WHERE, CREATE TYPE, ALTER COLUMN, all DML) is
// skip-and-declared, each locked by a test. View/Procedure/Trigger are populated
// with Name/Pos (and Table for triggers); their bodies, view columns, and the
// materialized flag are NOT captured — a declared limit for the future
// DB-020..041 rules. No SQL parsing library is used (CGO_ENABLED=0).
package sqlddl
