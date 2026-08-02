-- CONSTRUCTED (SYNTHETIC) PostgreSQL fixture for the table-shaped-head floor
-- (ADR 0043). It is authored, not vendored, and the reason is itself a
-- measurement: NONE of the forms below appears at the top level of any of the
-- 26 external corpora surveyed for ADR 0041, and none appears in any of this
-- repo's own 19 .sql fixtures. A regression on any of them could not be caught
-- by real DDL at all, so this file is their only control.
--
-- Three DIFFERENT dispositions are exercised deliberately in one file, because
-- the whole point of the slice is that they are not the same fact:
--
--   MODELED  — CREATE UNLOGGED TABLE. An unlogged table skips the WAL; it is
--              still ordinary persistent storage and belongs in the schema.
--   WITHHELD — the TEMP/TEMPORARY family. Read correctly, deliberately left
--              out of the model: a session-scoped table is not part of the
--              persistent schema, and admitting it would make DB-050 affirm
--              "table without a primary key" over scratch space.
--   DECLARED — a CREATE ... TABLE head no dispatch branch reduces. Recorded
--              verbatim on Schema.Unreduced; never guessed at, never silent.

-- CONTROL: the ordinary form, which must keep reducing exactly as before.
CREATE TABLE keeper (
    id integer PRIMARY KEY,
    label text
);

-- MODELED: UNLOGGED.
CREATE UNLOGGED TABLE events (
    id integer PRIMARY KEY,
    payload text
);

-- MODELED: UNLOGGED plus IF NOT EXISTS — the group-numbering control. The
-- widened prefix must stay NON-capturing, or reduceCreateTable reads the name
-- out of the wrong submatch.
CREATE UNLOGGED TABLE IF NOT EXISTS events_archive (
    id integer PRIMARY KEY
);

-- WITHHELD: every PostgreSQL session-scoped spelling.
CREATE TEMP TABLE scratch_temp (id integer);
CREATE TEMPORARY TABLE scratch_temporary (id integer);
CREATE GLOBAL TEMPORARY TABLE scratch_global (id integer);
CREATE LOCAL TEMPORARY TABLE scratch_local (id integer);
CREATE TEMPORARY TABLE IF NOT EXISTS scratch_ine (id integer);

-- DECLARED: two heads this reducer has no branch for. It reduces neither and
-- fabricates nothing from either — it records them verbatim so the scan can
-- say what it did not read.
CREATE FOREIGN TABLE external_orders (id integer) SERVER remote_srv;
CREATE TABLE summary_ctas AS SELECT id FROM keeper;
