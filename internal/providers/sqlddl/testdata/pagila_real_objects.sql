-- Vendored VERBATIM excerpt from Pagila — a real PostgreSQL sample DB — to
-- supply a real DYNAMIC-SQL routine (the DB-030 POSITIVE that neither the
-- MySQL Sakila nor the T-SQL AdventureWorks corpus contains). Copied
-- UNALTERED (byte-for-byte; the source is LF, no CRLF to normalize) from the
-- SAME upstream/commit as testdata/pagila_excerpt.sql — NOT appended to that
-- file, to keep its pinned golden (pagila_excerpt.schema.golden.json) and the
-- many line-number-pinning trigger/index tests byte-stable. Lives in its own
-- *_real_objects.sql file, mirroring the mysql/ and tsql/ real-object
-- fixtures.
--   Source:  https://github.com/devrimgunduz/pagila  (pagila-schema.sql)
--   Commit:  5ba5a57
--   Object taken (verbatim): CREATE FUNCTION public.rewards_report(integer,
--     numeric) — a PL/pgSQL function that BUILDS a query string in tmpSQL and
--     runs it with "EXECUTE tmpSQL" (and "FOR rr IN EXECUTE '...'"): a DB-030
--     dynamic-SQL POSITIVE. It has NO EXCEPTION handler block (it RAISEs but
--     never catches), so it is also a DB-031 POSITIVE. The dollar-quoted
--     ($_$...$_$) body carries many internal ';' — the parser keeps it as one
--     statement (Complete=true). This is a NAME COLLISION with, but a
--     DIFFERENT routine from, Sakila's MySQL rewards_report PROCEDURE (which
--     uses a temp table and has NO dynamic SQL): the Pagila PL/pgSQL port
--     rewrote it to build and EXECUTE its INSERT/SELECT/DROP dynamically.
--   Cross-object references (customer, payment, tmpCustomer) are not present
--   in this excerpt; that is expected and does not affect the structural DDL
--   parser.
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
-- codefit itself is Apache-2.0; this file is a vendored MIT-licensed excerpt
-- and carries its own notice above, same discipline as pagila_excerpt.sql and
-- the T-SQL AdventureWorks / MySQL Sakila real-object fixtures.

CREATE FUNCTION public.rewards_report(min_monthly_purchases integer, min_dollar_amount_purchased numeric) RETURNS SETOF public.customer
    LANGUAGE plpgsql SECURITY DEFINER
    AS $_$
DECLARE
    last_month_start DATE;
    last_month_end DATE;
rr RECORD;
tmpSQL TEXT;
BEGIN

    /* Some sanity checks... */
    IF min_monthly_purchases = 0 THEN
        RAISE EXCEPTION 'Minimum monthly purchases parameter must be > 0';
    END IF;
    IF min_dollar_amount_purchased = 0.00 THEN
        RAISE EXCEPTION 'Minimum monthly dollar amount purchased parameter must be > $0.00';
    END IF;

    last_month_start := CURRENT_DATE - '3 month'::interval;
    last_month_start := to_date((extract(YEAR FROM last_month_start) || '-' || extract(MONTH FROM last_month_start) || '-01'),'YYYY-MM-DD');
    last_month_end := LAST_DAY(last_month_start);

    /*
    Create a temporary storage area for Customer IDs.
    */
    CREATE TEMPORARY TABLE tmpCustomer (customer_id INTEGER NOT NULL PRIMARY KEY);

    /*
    Find all customers meeting the monthly purchase requirements
    */

    tmpSQL := 'INSERT INTO tmpCustomer (customer_id)
        SELECT p.customer_id
        FROM payment AS p
        WHERE DATE(p.payment_date) BETWEEN '||quote_literal(last_month_start) ||' AND '|| quote_literal(last_month_end) || '
        GROUP BY customer_id
        HAVING SUM(p.amount) > '|| min_dollar_amount_purchased || '
        AND COUNT(customer_id) > ' ||min_monthly_purchases ;

    EXECUTE tmpSQL;

    /*
    Output ALL customer information of matching rewardees.
    Customize output as needed.
    */
    FOR rr IN EXECUTE 'SELECT c.* FROM tmpCustomer AS t INNER JOIN customer AS c ON t.customer_id = c.customer_id' LOOP
        RETURN NEXT rr;
    END LOOP;

    /* Clean up */
    tmpSQL := 'DROP TABLE tmpCustomer';
    EXECUTE tmpSQL;

RETURN;
END
$_$;
