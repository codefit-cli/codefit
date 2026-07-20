-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite (feat/tsql-routine-fixtures)
-- to supply the DB-040 real-POSITIVE cell PostgreSQL lacks upstream.
--
-- Why constructed: the only trigger FUNCTIONS in the dogfooded Pagila excerpt
-- (testdata/pagila_excerpt.sql) do NOT write another table — public.last_updated
-- only sets NEW.last_update (a genuine DB-040 NEGATIVE, used as the real PG
-- negative), and film_fulltext_trigger names the built-in tsvector_update_trigger
-- which has no CREATE FUNCTION to resolve at all (the rule abstains). No real
-- Pagila trigger performs a cross-table cascade, so the POSITIVE fire path is
-- proven here by this small, deliberately hand-written trigger + function pair
-- instead. Same honesty discipline as DB-020's positive and DB-031's PG
-- negative: the synthetic origin is declared in the open, never implied upstream.
--
-- This fixture deliberately exercises the ADR-0026 trigger→function resolution
-- path that distinguishes PostgreSQL from MySQL/T-SQL: a PostgreSQL trigger
-- carries NO inline body — it is a WIRE from an event to a function, and the
-- cascade logic lives in that function. DB-040 must follow
-- Trigger.ExecutesFunction (via Schema.ExecutedProcedure) to the function's own
-- Body and scan THAT, not the (bodyless) trigger statement. Here the trigger
-- statement itself contains no DML; the INSERT into public.order_audit (a table
-- OTHER than the trigger's own public.orders) lives entirely in the function.
-- The write is intentionally UNDOCUMENTED (no comment beside the INSERT), so
-- the surfaced documented_by_comment fact is false — the concerning case the
-- agent should scrutinize.

CREATE FUNCTION public.audit_order_changes() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    INSERT INTO public.order_audit (order_id, changed_at, old_status, new_status)
        VALUES (NEW.order_id, CURRENT_TIMESTAMP, OLD.status, NEW.status);
    RETURN NEW;
END
$$;

CREATE TRIGGER order_audit_trigger AFTER UPDATE ON public.orders
    FOR EACH ROW EXECUTE FUNCTION public.audit_order_changes();
