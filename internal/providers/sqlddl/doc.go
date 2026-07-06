// Package sqlddl is codefit's hand-written SQL-DDL parser. It implements
// providers.SchemaParser (the second implementer after the Prisma parser), turning
// ordered SQL-DDL migrations into the neutral core/db.Schema. It supports THREE
// dialects today — PostgreSQL (Postgres(), the default New() construction),
// MySQL (MySQL()), and SQL Server/T-SQL (SQLServer()) — wired via
// internal/mcp/schemaparser.go, which selects the Dialect descriptor from the
// project's .codefit.yaml database.type setting; the tokenizer and reducer
// are dialect-agnostic by design, consuming only the selected Dialect's data.
//
// A Parser is bound to exactly one Dialect at construction time (New(opts
// ...Option), WithDialect) — see dialect.go for the descriptor shape. Two
// pieces consume it (ADR 0018, and the per-dialect descriptor of ADR 0022): a
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
//
// Known limit (Unit I rework, disclosed not silent): codefit does not model
// stored-procedure/trigger routine BODIES. For MySQL, a body wrapped by the
// client-tool "DELIMITER //" ... "DELIMITER ;" convention is handled
// correctly — split.go's DELIMITER tracking keeps the whole body as ONE
// statement, so it is captured by the routine/trigger HEAD regex and no
// body-internal fragment is ever reduced as its own top-level statement. For
// T-SQL, a GO-batched routine/trigger body has no such protection (its
// internal ';'s terminate normally, each becoming its own statement); a body
// fragment that happens to be CREATE-TABLE-shaped MAY therefore surface as a
// spurious top-level table. An earlier "inRoutineBody" guard tried to
// speculatively suppress this by matching BEGIN/END as raw text, but that
// guard was itself unsound — it matched BEGIN/END inside string literals,
// was not depth-counted (a nested BEGIN...END closed it early), and was
// never reset between files (a stuck-open guard from one file could swallow
// a later file's real tables) — real regressions on VALID input. It has been
// removed; the T-SQL routine-body case is now a documented, rare limit
// rather than a fragile guard. Separately, the "GO" batch-separator
// recognition (split.go) requires the ENTIRE trimmed line to be exactly
// "GO", so it cannot match part of a longer identifier — but it also means a
// column literally named "go" standing alone on its own line (vanishingly
// unlikely in real DDL) would collide with batch-separator recognition; this
// narrow collision is accepted, not guarded against. Relatedly, MySQL client
// DELIMITER directives are only recognized when their argument is
// punctuation-only (e.g. "DELIMITER //", "DELIMITER $$") — this is what lets
// an ordinary "delimiter VARCHAR(1)" column definition parse as a column, not
// a directive (Unit I rework, C1). A word-based custom delimiter (e.g.
// "DELIMITER GO") is therefore NOT recognized as a delimiter directive; this
// is a narrow, accepted limit, not a bug — punctuation-only delimiters cover
// the overwhelming majority of real-world MySQL dump/migration tooling.
package sqlddl
