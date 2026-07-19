-- Vendored VERBATIM excerpt from the OFFICIAL Sakila sample database schema
-- (the `customer_list` VIEW), copied UNALTERED — not "in the style of" —
-- from Oracle/MySQL's own canonical distribution. This file exists to give
-- DB-020 (view sensitive-column exposure, Phase 2.2) real MySQL dogfood
-- evidence: `sakila_excerpt.sql` in this same directory has ONLY tables (no
-- view/procedure/trigger data at all), so it cannot exercise a view rule —
-- see architecture/db-phase-2-2-scope (obs #1042) and
-- architecture/unit-c-tsql-risk-reeval (obs #1056), which required either a
-- real vendored Sakila view or DB-020 shipping declared NotCovered on MySQL.
--
--   Source:  https://downloads.mysql.com/docs/sakila-db.tar.gz
--            (sakila-db/sakila-schema.sql, "Sakila Sample Database Schema,
--            Version 1.5")
--   Also referenced from:
--            https://dev.mysql.com/doc/sakila/en/sakila-license.html
--            https://dev.mysql.com/doc/sakila/en/sakila-structure-views.html
--
-- Object taken (verbatim, byte-for-byte, no CRLF present in the source to
-- normalize): CREATE VIEW `customer_list`. Not the whole schema — trimmed to
-- this one view. The view's own upstream FROM/JOIN clauses reference
-- `customer`, `address`, `city`, `country` — none of those tables are
-- present in this excerpt; that is expected and does not affect the DDL
-- parser, which is structural and does not resolve cross-object references
-- at parse time (same disclosed limit as the T-SQL AdventureWorks excerpt in
-- ../tsql/adventureworks_real_objects.sql).
--
-- License: New BSD (per dev.mysql.com/doc/sakila/en/sakila-license.html:
-- "sakila-schema.sql ... licensed under the New BSD license").
--
--   Copyright (c) 2006, 2026, Oracle and/or its affiliates.
--
--   Redistribution and use in source and binary forms, with or without
--   modification, are permitted provided that the following conditions are
--   met:
--
--   * Redistributions of source code must retain the above copyright notice,
--     this list of conditions and the following disclaimer.
--   * Redistributions in binary form must reproduce the above copyright
--     notice, this list of conditions and the following disclaimer in the
--     documentation and/or other materials provided with the distribution.
--   * Neither the name of Oracle nor the names of its contributors may be
--     used to endorse or promote products derived from this software
--     without specific prior written permission.
--
--   THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS
--   IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
--   TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A
--   PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER
--   OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
--   EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
--   PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
--   PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
--   LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
--   NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
--   SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
--
-- codefit itself is Apache-2.0; this file is a vendored New-BSD-licensed
-- excerpt and carries its own notice above, per the same vendoring
-- discipline already applied to the Pagila (Copyright Devrim Gündüz) and
-- AdventureWorks (MIT, Microsoft) excerpts in this testdata tree.

CREATE VIEW customer_list
AS
SELECT cu.customer_id AS ID, CONCAT(cu.first_name, _utf8mb4' ', cu.last_name) AS name, a.address AS address, a.postal_code AS `zip code`,
	a.phone AS phone, city.city AS city, country.country AS country, IF(cu.active, _utf8mb4'active',_utf8mb4'') AS notes, cu.store_id AS SID
FROM customer AS cu JOIN address AS a ON cu.address_id = a.address_id JOIN city ON a.city_id = city.city_id
	JOIN country ON city.country_id = country.country_id;

-- ---------------------------------------------------------------------------
-- Appended for the DB-030/DB-031/DB-040 routine fixture extension
-- (feat/tsql-routine-fixtures): real upstream Sakila PROCEDURE, FUNCTION, and
-- the three film TRIGGERS, vendored VERBATIM (byte-for-byte; the source is LF,
-- no CRLF to normalize) from the SAME official Oracle/MySQL distribution as
-- the customer_list view above.
--
--   Source:  https://downloads.mysql.com/docs/sakila-db.tar.gz
--            (sakila-db/sakila-schema.sql, "Sakila Sample Database Schema,
--            Version 1.5")
--   Objects taken (verbatim, unaltered):
--     - CREATE TRIGGER `ins_film` / `upd_film` / `del_film`  — each writes the
--       SEPARATE film_text table (INSERT/UPDATE/DELETE): a DB-040 trigger-
--       cascade POSITIVE. None makes any external/procedure call, so each is
--       also a DB-041 NEGATIVE.
--     - CREATE PROCEDURE rewards_report  — multi-statement, but NO dynamic SQL
--       (no PREPARE/EXECUTE) and NO DECLARE ... HANDLER: a DB-030 NEGATIVE and,
--       lacking any exception handler, a DB-031 POSITIVE.
--     - CREATE FUNCTION inventory_held_by_customer  — carries a real
--       "DECLARE EXIT HANDLER FOR NOT FOUND": a DB-031 NEGATIVE (genuine
--       exception handling).
--
-- Kept at end-of-file so it does not shift the line the DB-020 view tests pin
-- for customer_list above. The routines carry their upstream client
-- "DELIMITER ;;" / "DELIMITER //" / "DELIMITER $$" directives verbatim;
-- codefit's sqlddl split() honors them (the active-terminator override is what
-- keeps each multi-statement body a single statement). rewards_report's
-- INSERT ... SELECT FROM payment and the triggers' references to
-- film_text/film are cross-object references not present in this excerpt; that
-- is expected and does not affect the structural DDL parser (same disclosed
-- limit as the customer_list view above).
-- ---------------------------------------------------------------------------

DELIMITER ;;
CREATE TRIGGER `ins_film` AFTER INSERT ON `film` FOR EACH ROW BEGIN
    INSERT INTO film_text (film_id, title, description)
        VALUES (new.film_id, new.title, new.description);
  END;;


CREATE TRIGGER `upd_film` AFTER UPDATE ON `film` FOR EACH ROW BEGIN
    IF (old.title != new.title) OR (old.description != new.description) OR (old.film_id != new.film_id)
    THEN
        UPDATE film_text
            SET title=new.title,
                description=new.description,
                film_id=new.film_id
        WHERE film_id=old.film_id;
    END IF;
  END;;


CREATE TRIGGER `del_film` AFTER DELETE ON `film` FOR EACH ROW BEGIN
    DELETE FROM film_text WHERE film_id = old.film_id;
  END;;

DELIMITER ;

DELIMITER //

CREATE PROCEDURE rewards_report (
    IN min_monthly_purchases TINYINT UNSIGNED
    , IN min_dollar_amount_purchased DECIMAL(10,2)
    , OUT count_rewardees INT
)
LANGUAGE SQL
NOT DETERMINISTIC
READS SQL DATA
SQL SECURITY DEFINER
COMMENT 'Provides a customizable report on best customers'
proc: BEGIN

    DECLARE last_month_start DATE;
    DECLARE last_month_end DATE;

    /* Some sanity checks... */
    IF min_monthly_purchases = 0 THEN
        SELECT 'Minimum monthly purchases parameter must be > 0';
        LEAVE proc;
    END IF;
    IF min_dollar_amount_purchased = 0.00 THEN
        SELECT 'Minimum monthly dollar amount purchased parameter must be > $0.00';
        LEAVE proc;
    END IF;

    /* Determine start and end time periods */
    SET last_month_start = DATE_SUB(CURRENT_DATE(), INTERVAL 1 MONTH);
    SET last_month_start = STR_TO_DATE(CONCAT(YEAR(last_month_start),'-',MONTH(last_month_start),'-01'),'%Y-%m-%d');
    SET last_month_end = LAST_DAY(last_month_start);

    /*
        Create a temporary storage area for
        Customer IDs.
    */
    CREATE TEMPORARY TABLE tmpCustomer (customer_id SMALLINT UNSIGNED NOT NULL PRIMARY KEY);

    /*
        Find all customers meeting the
        monthly purchase requirements
    */
    INSERT INTO tmpCustomer (customer_id)
    SELECT p.customer_id
    FROM payment AS p
    WHERE DATE(p.payment_date) BETWEEN last_month_start AND last_month_end
    GROUP BY customer_id
    HAVING SUM(p.amount) > min_dollar_amount_purchased
    AND COUNT(customer_id) > min_monthly_purchases;

    /* Populate OUT parameter with count of found customers */
    SELECT COUNT(*) FROM tmpCustomer INTO count_rewardees;

    /*
        Output ALL customer information of matching rewardees.
        Customize output as needed.
    */
    SELECT c.*
    FROM tmpCustomer AS t
    INNER JOIN customer AS c ON t.customer_id = c.customer_id;

    /* Clean up */
    DROP TABLE tmpCustomer;
END //

DELIMITER ;

DELIMITER $$

CREATE FUNCTION inventory_held_by_customer(p_inventory_id INT) RETURNS INT
READS SQL DATA
BEGIN
  DECLARE v_customer_id INT;
  DECLARE EXIT HANDLER FOR NOT FOUND RETURN NULL;

  SELECT customer_id INTO v_customer_id
  FROM rental
  WHERE return_date IS NULL
  AND inventory_id = p_inventory_id;

  RETURN v_customer_id;
END $$

DELIMITER ;
