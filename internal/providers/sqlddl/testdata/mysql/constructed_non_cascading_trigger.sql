-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite (feat/tsql-routine-fixtures)
-- to supply the DB-040 real-NEGATIVE cell MySQL lacks upstream.
--
-- Why constructed: EVERY trigger in the real vendored Sakila corpus
-- (testdata/mysql/sakila_real_objects.sql: ins_film / upd_film / del_film) is a
-- genuine DB-040 POSITIVE — each one cascades a write into the film_text table,
-- a table OTHER than its own `film`. Sakila ships no trigger that writes only
-- its own table (or nothing at all), so the NEGATIVE fire path — a trigger with
-- NO cross-table DML → DB-040 must NOT fire — is proven here by this small,
-- deliberately hand-written trigger instead. This follows the same honesty
-- discipline 0.2.2/0.2.3 used for constructed cases (DB-020's positive, DB-031's
-- PostgreSQL negative): the synthetic origin is declared in the open, never
-- implied to be upstream.
--
-- The body below is valid MySQL: a BEFORE INSERT trigger that only stamps a
-- column on the NEW row (SET NEW.created_at = NOW()). It performs NO
-- INSERT/UPDATE/DELETE against any table, so it has no cross-table cascade at
-- all — the structural inverse of the vendored film triggers. It is wrapped in
-- a DELIMITER block so the trigger body is captured COMPLETE (the terminator is
-- the custom delimiter, not ';'), the same shape the real Sakila fixture uses.

DELIMITER //
CREATE TRIGGER `trg_orders_stamp` BEFORE INSERT ON `orders` FOR EACH ROW BEGIN
    SET NEW.created_at = NOW();
END//
DELIMITER ;
