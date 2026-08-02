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
// Two boundaries inside the reducer are DERIVED rather than lexical, because
// neither is expressed by a delimiter the splitter can see. A statement run onto
// a CREATE TABLE's tail with no terminator is cut at that tail (ADR 0041), and a
// CREATE TABLE body item missing its separating comma before a table-level key
// constraint is cut at the constraint head (ADR 0042). Both rest on a grammar
// fact rather than a guess, both leave the host table proven, and both route
// anything they find but cannot reduce to the honest-abstention floor.
//
// Not every relation the reducer READS is a table, and it remembers the
// difference (ADR 0045). A CREATE SEQUENCE and a CREATE (MATERIALIZED) VIEW put
// their NAME in a reducer-internal registry with no model surface of their own,
// so the ownership statement pg_dump writes for every relation it dumps —
// "ALTER TABLE public.<name>_id_seq OWNER TO <role>", which PostgreSQL legally
// accepts for every relation kind — is not mistaken for a table declaration.
// Without it, a sequence became a zero-column table that DB-050 then asked the
// agent about. The guard is EVIDENCE-driven, never name- or action-driven: a
// name nothing in the scanned files declared still materializes with
// db.ReasonTableNeverDeclared, exactly as before.
//
// It parses a DECLARED SUBSET; everything outside (CHECK constraints, ON DELETE
// actions, partial-index WHERE, CREATE TYPE, ALTER COLUMN, all DML) is
// skip-and-declared, each locked by a test. View/Procedure/Trigger are populated
// with Name/Pos (and Table for triggers) AND with their BODY (db.Body{Text,
// Complete}), which is what the DB-020/030/031/040/041 rules read; view columns
// and the materialized flag are still NOT captured — a declared limit. No SQL
// parsing library is used (CGO_ENABLED=0).
//
// Routine bodies, and how the two dialect families get there (disclosed, not
// silent): for MySQL, a body wrapped by the client-tool "DELIMITER //" ...
// "DELIMITER ;" convention is kept as ONE statement by split.go's DELIMITER
// tracking, so it is captured by the routine/trigger HEAD regex and no
// body-internal fragment is ever reduced as its own top-level statement. For
// T-SQL, the body is captured to the "GO" batch separator (or EOF) per ADR
// 0027, so a CREATE-TABLE-shaped fragment inside a GO-batched routine body is
// ABSORBED into that body rather than surfacing as a spurious top-level table
// — which closes the phantom-table limit ADR 0022 had declared. The trade ADR
// 0027 accepted is the opposite shape: a T-SQL routine with NO trailing GO
// swallows whatever follows it up to EOF. An earlier "inRoutineBody" guard
// tried to suppress the phantom speculatively by matching BEGIN/END as raw
// text, but that guard was itself unsound — it matched BEGIN/END inside
// string literals, was not depth-counted (a nested BEGIN...END closed it
// early), and was never reset between files (a stuck-open guard from one file
// could swallow a later file's real tables) — real regressions on VALID
// input. It was removed and replaced by the batch-boundary capture above,
// which is structural rather than speculative. Separately, the "GO" batch-separator
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
