// Package sqlddl is codefit's hand-written PostgreSQL DDL parser. It implements
// providers.SchemaParser (the second implementer after the Prisma parser), turning
// ordered SQL-DDL migrations into the neutral core/db.Schema.
//
// Two pieces (ADR 0018): a statement splitter that is aware of dollar-quoting
// ($$…$$ / $tag$…$tag$), strings, quoted identifiers and comments — so a PL/pgSQL
// DO block's internal semicolons never cut a statement — and an incremental
// reducer that applies statements IN ORDER over a mutable schema (CREATE TABLE
// creates, ALTER ADD/DROP COLUMN mutates, CREATE INDEX indexes, ADD CONSTRAINT
// constrains), accumulating to the final state.
//
// It parses a DECLARED SUBSET; everything outside (CHECK constraints, ON DELETE
// actions, partial-index WHERE, CREATE TYPE, ALTER COLUMN, all DML) is
// skip-and-declared, each locked by a test. View/Procedure/Trigger are populated
// with Name/Pos (and Table for triggers); their bodies, view columns, and the
// materialized flag are NOT captured — a declared limit for the future
// DB-020..041 rules. No SQL parsing library is used (CGO_ENABLED=0).
package sqlddl
