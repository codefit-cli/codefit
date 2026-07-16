-- Vendored excerpt from Pagila — a real PostgreSQL sample DB — to exercise the
-- SQL-DDL parser on genuine views, functions (dollar-quoted $$ and $_$ bodies with
-- internal semicolons), triggers, and (architecture/pagila-fixture-real-indexes)
-- real indexes for the DB-011a/DB-011b/Unit E index-rule family.
--   Source:  https://github.com/devrimgunduz/pagila  (pagila-schema.sql)
--   Commit:  5ba5a57
-- Verbatim excerpt; only assembled into one file. Not the whole schema.
--
-- License: MIT
--   Copyright (c) Devrim Gündüz <devrim@gunduz.org>
--
--   Permission is hereby granted, free of charge, to any person obtaining a
--   copy of this software and associated documentation files (the
--   "Software"), to deal in the Software without restriction, including
--   without limitation the rights to use, copy, modify, merge, publish,
--   distribute, sublicense, and/or sell copies of the Software, and to
--   permit persons to whom the Software is furnished to do so, subject to
--   the following conditions:
--
--   The above copyright notice and this permission notice shall be
--   included in all copies or substantial portions of the Software.
--
--   THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS
--   OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
--   MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
--   IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
--   CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
--   TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
--   SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
--
--   Full license text: https://github.com/devrimgunduz/pagila/blob/master/LICENSE.txt
--
-- codefit itself is Apache-2.0; this file is a vendored MIT-licensed
-- excerpt and carries its own notice above, same discipline as the T-SQL
-- AdventureWorks and MySQL Sakila real-object fixtures. The README at
-- github.com/devrimgunduz/pagila loosely says "PostgreSQL license", but the
-- repo's actual LICENSE.txt is the MIT License reproduced above (verified
-- by reading it directly, not assumed from the README's wording).

CREATE TABLE public.actor (
    actor_id integer NOT NULL,
    first_name text NOT NULL,
    last_name text NOT NULL,
    last_update timestamp with time zone DEFAULT now() NOT NULL
);

CREATE FUNCTION public._group_concat(text, text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $_$
SELECT CASE
  WHEN $2 IS NULL THEN $1
  WHEN $1 IS NULL THEN $2
  ELSE $1 || ', ' || $2
END
$_$;

CREATE FUNCTION public.last_updated() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.last_update = CURRENT_TIMESTAMP;
    RETURN NEW;
END $$;

CREATE VIEW public.actor_info AS
 SELECT a.actor_id,
    a.first_name,
    a.last_name
   FROM public.actor a
  GROUP BY a.actor_id, a.first_name, a.last_name;

CREATE TRIGGER last_updated BEFORE UPDATE ON public.actor FOR EACH ROW EXECUTE FUNCTION public.last_updated();

CREATE TRIGGER film_fulltext_trigger BEFORE INSERT OR UPDATE ON public.film FOR EACH ROW EXECUTE FUNCTION tsvector_update_trigger('fulltext', 'pg_catalog.english', 'title', 'description');

-- ---------------------------------------------------------------------------
-- Appended for the DB-011a/DB-011b/Unit E fixture extension
-- (architecture/pagila-fixture-real-indexes): real upstream Pagila
-- CREATE TABLE + CREATE INDEX statements, so PostgreSQL's index-rule family
-- (DB-011a exact-duplicate, DB-011b prefix-redundant, Unit E) has real
-- indexes to run against instead of shipping NotCovered for lack of any
-- index-like coverer at all. Same source/commit as above (5ba5a57).
--
-- Kept at end-of-file so it does not shift the line numbers the existing
-- golden snapshot and trigger-link tests pin for actor/the views/functions/
-- triggers above (same discipline as sakila_excerpt.sql's historical note).
--
-- Primary keys and foreign keys for these tables are declared upstream via
-- separate "ALTER TABLE ONLY ... ADD CONSTRAINT" statements, deliberately
-- NOT vendored here: codefit's sqlddl parser does not yet handle
-- PostgreSQL's "ALTER TABLE ONLY <table>" idiom (it mis-parses the literal
-- token "ONLY" as the table name and silently drops the real constraint) —
-- a pre-existing, separately reported parser limit, not something this
-- fixture extension papers over. actor above already ships PK-free for the
-- identical upstream reason.
-- ---------------------------------------------------------------------------

CREATE TABLE public.customer (
    customer_id integer DEFAULT nextval('public.customer_customer_id_seq'::regclass) NOT NULL,
    store_id integer NOT NULL,
    first_name text NOT NULL,
    last_name text NOT NULL,
    email text,
    address_id integer NOT NULL,
    activebool boolean DEFAULT true NOT NULL,
    create_date date DEFAULT CURRENT_DATE NOT NULL,
    last_update timestamp with time zone DEFAULT now(),
    active integer
);

-- Verbatim EXCEPT the trailing "fulltext tsvector NOT NULL" column, which is
-- deliberately omitted (real, discovered parser gap; reported, not fixed —
-- see architecture/pagila-fixture-real-indexes and db011_integration_test.go):
-- a real column literally named "fulltext" with an unmapped type (tsvector
-- is not in postgresTypeMap) collides with codefit's sqlddl parser's
-- MySQL-inline-index-shorthand discriminator (isInlineKeyIndexForm treats
-- the leading token FULLTEXT as the "FULLTEXT KEY/INDEX" shorthand keyword
-- whenever the following type is unmapped), silently dropping the column
-- and fabricating a phantom zero-column index instead. Every other column
-- below is the real, unmodified upstream text.
CREATE TABLE public.film (
    film_id integer DEFAULT nextval('public.film_film_id_seq'::regclass) NOT NULL,
    title text NOT NULL,
    description text,
    release_year public.year,
    language_id integer NOT NULL,
    original_language_id integer,
    rental_duration smallint DEFAULT 3 NOT NULL,
    rental_rate numeric(4,2) DEFAULT 4.99 NOT NULL,
    length smallint,
    replacement_cost numeric(5,2) DEFAULT 19.99 NOT NULL,
    rating public.mpaa_rating DEFAULT 'G'::public.mpaa_rating,
    last_update timestamp with time zone DEFAULT now() NOT NULL,
    special_features text[]
);

CREATE TABLE public.address (
    address_id integer DEFAULT nextval('public.address_address_id_seq'::regclass) NOT NULL,
    address text NOT NULL,
    address2 text,
    district text NOT NULL,
    city_id integer NOT NULL,
    postal_code text,
    phone text NOT NULL,
    last_update timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.rental (
    rental_id integer DEFAULT nextval('public.rental_rental_id_seq'::regclass) NOT NULL,
    rental_date timestamp with time zone NOT NULL,
    inventory_id integer NOT NULL,
    customer_id integer NOT NULL,
    return_date timestamp with time zone,
    staff_id integer NOT NULL,
    last_update timestamp with time zone DEFAULT now() NOT NULL
);

CREATE INDEX idx_fk_address_id ON public.customer USING btree (address_id);

CREATE INDEX idx_fk_city_id ON public.address USING btree (city_id);

CREATE INDEX idx_last_name ON public.customer USING btree (last_name);

CREATE INDEX idx_title ON public.film USING btree (title);

CREATE UNIQUE INDEX idx_unq_rental_rental_date_inventory_id_customer_id ON public.rental USING btree (rental_date, inventory_id, customer_id);

-- Real exact-duplicate index (DB-011a positive proof, no mutation needed).
-- Upstream declares "payment" PARTITION BY RANGE (payment_date) with 7
-- monthly partition children attached via a separate
-- "ALTER TABLE ONLY payment ATTACH PARTITION ..." statement; each child
-- (e.g. payment_p2022_01) is itself dumped by pg_dump as a completely
-- ordinary, standalone CREATE TABLE — the same shape as any other table,
-- with no "PARTITION OF" grammar in its own statement at all. Verified
-- directly: codefit's sqlddl parser does NOT handle the literal
-- "CREATE TABLE x PARTITION OF y FOR VALUES ..." grammar form (reported,
-- not used here) — this is the real, unmodified pg_dump text for
-- payment_p2022_01, no PARTITION OF/ATTACH PARTITION/ALTER TABLE ONLY
-- anywhere in it, which upstream genuinely carries TWO indexes on the
-- identical (customer_id) column list: idx_fk_payment_p2022_01_customer_id
-- and payment_p2022_01_customer_id_idx.
CREATE TABLE public.payment_p2022_01 (
    payment_id integer DEFAULT nextval('public.payment_payment_id_seq'::regclass) NOT NULL,
    customer_id integer NOT NULL,
    staff_id integer NOT NULL,
    rental_id integer NOT NULL,
    amount numeric(5,2) NOT NULL,
    payment_date timestamp with time zone NOT NULL
);

CREATE INDEX idx_fk_payment_p2022_01_customer_id ON public.payment_p2022_01 USING btree (customer_id);

CREATE INDEX payment_p2022_01_customer_id_idx ON public.payment_p2022_01 USING btree (customer_id);
