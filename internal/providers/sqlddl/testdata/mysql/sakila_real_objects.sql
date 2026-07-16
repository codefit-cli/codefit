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
