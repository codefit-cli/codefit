-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite to reproduce the statement
-- SEQUENCE that pg_dump emits around every serial/identity column, and the
-- PHANTOM TABLE it used to materialize (ADR 0044).
--
-- Why constructed: a case-insensitive grep for "^\s*create\s+sequence" across
-- internal/providers/sqlddl/testdata/ returned ZERO hits before this file was
-- authored (`rg -c -i --glob '*.sql' '^\s*create\s+sequence'` → exit 1, no
-- output). Every sequence-related test written against the pre-existing corpus
-- would therefore have passed VACUOUSLY. The fixture is deliberately AUTHORED
-- so the statements under test actually exist in the file under test — the same
-- discipline pg_constructed_n2_recognized_skips.sql already declares in this
-- directory.
--
-- Every statement below is real, valid PostgreSQL syntax, in the exact order and
-- spelling pg_dump 16/17 writes it (CREATE TABLE, OWNER TO, CREATE SEQUENCE,
-- OWNER TO on the sequence, ALTER SEQUENCE ... OWNED BY, the ALTER COLUMN ...
-- SET DEFAULT nextval() wiring, and the setval() call from the data section).
-- Only the assembly into one small file is synthetic.

CREATE TABLE public.users (
    id bigint NOT NULL,
    email character varying(255),
    password character varying(255)
);

ALTER TABLE public.users OWNER TO postgres;

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER TABLE public.users_id_seq OWNER TO postgres;

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;

-- A second sequence in the "AS integer" spelling pg_dump uses for a plain
-- serial, with the IF NOT EXISTS form a hand-written migration adds.
CREATE SEQUENCE IF NOT EXISTS public.orders_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1;

ALTER TABLE public.orders_id_seq OWNER TO postgres;

CREATE TABLE public.orders (
    id integer NOT NULL,
    user_id bigint NOT NULL
);

ALTER TABLE public.orders OWNER TO postgres;

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);

ALTER TABLE ONLY public.orders ALTER COLUMN id SET DEFAULT nextval('public.orders_id_seq'::regclass);

SELECT pg_catalog.setval('public.users_id_seq', 42, true);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (id);
