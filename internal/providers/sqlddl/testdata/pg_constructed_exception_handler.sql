-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite (feat/tsql-routine-fixtures)
-- to supply the DB-031 real-NEGATIVE cell PostgreSQL lacks upstream.
--
-- Why constructed: the real vendored Pagila routine (public.rewards_report,
-- testdata/pagila_real_objects.sql) is a genuine DB-031 POSITIVE — it RAISEs
-- but carries NO exception-handler clause. No routine in the dogfooded Pagila
-- excerpt happens to contain a real PL/pgSQL "EXCEPTION WHEN" handler block, so
-- the NEGATIVE fire path (a routine WITH a handler → DB-031 must NOT fire) is
-- proven here by this small, deliberately hand-written function instead. This
-- follows the same honesty discipline 0.2.2 used for constructed cases (e.g.
-- DB-020's positive, DB-011b's positives): the synthetic origin is declared in
-- the open, never implied to be upstream.
--
-- The body below is valid PL/pgSQL: a real BEGIN ... EXCEPTION WHEN ... END
-- handler block (WHEN division_by_zero / WHEN others). It is the structural
-- inverse of the vendored rewards_report positive.

CREATE FUNCTION public.safe_divide(numerator numeric, denominator numeric) RETURNS numeric
    LANGUAGE plpgsql
    AS $$
DECLARE
    result numeric;
BEGIN
    result := numerator / denominator;
    RETURN result;
EXCEPTION
    WHEN division_by_zero THEN
        RAISE NOTICE 'division by zero, returning NULL';
        RETURN NULL;
    WHEN others THEN
        RETURN NULL;
END
$$;
