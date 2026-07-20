-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite (feat/tsql-routine-fixtures)
-- to supply the DB-041 real-POSITIVE cell PostgreSQL lacks upstream.
--
-- Why constructed: no trigger function in the dogfooded Pagila corpus makes an
-- external-effecting call — public.last_updated only sets NEW (the real PG
-- NEGATIVE), and film_fulltext_trigger names a built-in that does not resolve.
-- No real Pagila trigger issues NOTIFY / dblink / COPY ... PROGRAM, so the
-- POSITIVE fire path is proven here by this small, deliberately hand-written
-- trigger + function pair instead. Same honesty discipline as DB-020's positive
-- and DB-031's PG negative: the synthetic origin is declared, never implied
-- upstream.
--
-- Like DB-040's constructed PG positive, this fixture exercises the ADR-0026
-- trigger→function resolution: a PostgreSQL trigger carries NO inline body, so
-- DB-041 must follow Trigger.ExecutesFunction (via Schema.ExecutedProcedure) to
-- the function's own body and scan THAT. The trigger statement itself makes no
-- call; the external-effecting NOTIFY (an async signal delivered to listeners
-- OUTSIDE the transaction) lives entirely in the function.

CREATE FUNCTION public.notify_order_change() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NOTIFY order_events;
    RETURN NEW;
END
$$;

CREATE TRIGGER order_notify_trigger AFTER INSERT ON public.orders
    FOR EACH ROW EXECUTE FUNCTION public.notify_order_change();
