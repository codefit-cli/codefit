-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite (db-model-completeness-
-- contract, design SS3c / N2 correction) to prove the reducer's RECOGNIZED-skip
-- vocabulary (OWNER TO / ENABLE / ALTER COLUMN / CHECK) does NOT mute the
-- table's completeness signal.
--
-- Why constructed: N2's original evidence was that the vendored
-- pagila_excerpt.sql corpus already contains OWNER TO/RENAME/ENABLE/ALTER
-- COLUMN statements. That evidence was FALSE — a case-insensitive grep across
-- internal/providers/sqlddl/testdata/ returns ZERO genuine hits for any of
-- those (the three apparent hits are all "PRIMARY KEY CLUSTERED" false-
-- positives of an over-broad CLUSTER pattern). A regression test written
-- against the existing corpus would pass VACUOUSLY. This fixture is
-- deliberately AUTHORED so the statements it needs actually exist in the file
-- under test — declared synthetic in the open, per the same discipline
-- pg_constructed_exception_handler.sql already uses in this directory.
--
-- Every statement below is real, valid PostgreSQL syntax (the exact forms
-- pg_dump itself emits for OWNER TO, and idiomatic RLS/ALTER COLUMN/CHECK
-- usage) — only the assembly into one small file, and the choice of which
-- statements to include, is synthetic.

CREATE TABLE public.widget (
    widget_id integer NOT NULL,
    name text NOT NULL
);

ALTER TABLE public.widget OWNER TO postgres;

ALTER TABLE public.widget ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.widget ALTER COLUMN name SET NOT NULL;

ALTER TABLE public.widget ADD CONSTRAINT widget_name_not_blank CHECK (name <> '');

-- A second table, genuinely missing a primary key, to prove the recognized
-- skips above do not cost DB-050 its capability on a DIFFERENT table in the
-- same file (the positive control, spec "Domain: Positive Control").
CREATE TABLE public.widget_no_pk (
    widget_id integer NOT NULL,
    name text NOT NULL
);
