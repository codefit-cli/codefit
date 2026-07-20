-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any upstream
-- corpus. Hand-written for codefit's own test suite (feat/tsql-routine-fixtures)
-- to supply the DB-030 real-POSITIVE cell MySQL lacks upstream.
--
-- Why constructed: no procedure in the real vendored Sakila corpus builds SQL
-- dynamically — rewards_report uses a temp table and inventory_held_by_customer
-- is straight-line SQL (both are real DB-030 NEGATIVES). Sakila ships no
-- PREPARE ... FROM a built string, so the POSITIVE fire path is proven here by
-- this small, deliberately hand-written procedure instead. Same honesty
-- discipline as DB-020's positive and DB-031's PG negative: the synthetic origin
-- is declared, never implied upstream.
--
-- The body below is the canonical MySQL dynamic-SQL antipattern: it CONCATs a
-- column name and value into a query string and runs it via PREPARE ... FROM +
-- EXECUTE — a DB-030 positive. It is wrapped in a DELIMITER block so the body is
-- captured COMPLETE.

DELIMITER //
CREATE PROCEDURE search_orders(IN col_name VARCHAR(64), IN col_value VARCHAR(64))
BEGIN
    SET @sql = CONCAT('SELECT * FROM orders WHERE ', col_name, ' = ', col_value);
    PREPARE stmt FROM @sql;
    EXECUTE stmt;
    DEALLOCATE PREPARE stmt;
END//
DELIMITER ;
