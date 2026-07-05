-- Vendored excerpt from Pagila — a real PostgreSQL sample DB — to exercise the
-- SQL-DDL parser on genuine views, functions (dollar-quoted $$ and $_$ bodies with
-- internal semicolons), and triggers.
--   Source:  https://github.com/devrimgunduz/pagila  (pagila-schema.sql)
--   Commit:  5ba5a57
--   License: Copyright (c) Devrim Gündüz — see repo LICENSE.txt
-- Verbatim excerpt; only assembled into one file. Not the whole schema.

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
