-- CONSTRUCTED / SYNTHETIC fixture — NOT vendored, NOT copied from any
-- upstream corpus. Authored for codefit's own test suite
-- (parser-records-unrecognized-drops, ADR 0034 SS2.4) to prove that a CREATE
-- INDEX-shaped statement reCreateIndex's own grammar does not cover is
-- GENUINELY UNRECOGNIZED — not a declared skip — and must mark its table
-- unproven rather than vanish silently.
--
-- Why constructed: a case-insensitive grep across
-- internal/providers/sqlddl/testdata/ for COLUMNSTORE, "ON ONLY", and an
-- anonymous "CREATE INDEX ON" (no index name) returns ZERO genuine hits — a
-- regression test written against the existing corpus would pass
-- VACUOUSLY. Every statement below is real, valid syntax from its origin
-- dialect (T-SQL's own COLUMNSTORE grammar; PostgreSQL's own anonymous-index
-- and ON ONLY partitioned-table grammar) — only the assembly into one file,
-- parsed under the default dialect (this reducer's dispatch is dialect-free
-- text matching, ADR 0022), is synthetic.

CREATE TABLE fact_sales (
    sale_id integer NOT NULL,
    amount numeric(12,2)
);

-- T-SQL: CREATE [CLUSTERED] COLUMNSTORE INDEX — reCreateIndex requires a
-- mandatory index name captured right before "on", and does not know the
-- CLUSTERED/COLUMNSTORE vocabulary at all.
CREATE CLUSTERED COLUMNSTORE INDEX cci_fact_sales ON fact_sales;

CREATE TABLE event_log (
    event_id integer NOT NULL,
    payload text
);

-- PostgreSQL: an anonymous index (no index name) — legal PostgreSQL syntax,
-- but reCreateIndex's index-name capture group is mandatory.
CREATE INDEX ON event_log USING brin (event_id);

CREATE TABLE metrics_partitioned (
    metric_id integer NOT NULL,
    value numeric
);

-- PostgreSQL: CREATE INDEX ... ON ONLY <table> — a partitioned parent
-- table's own index declaration; reCreateIndex has no ONLY vocabulary
-- either.
CREATE INDEX idx_metrics_value ON ONLY metrics_partitioned (value);

CREATE TABLE articles (
    article_id integer NOT NULL,
    body text
);

-- MySQL/T-SQL: CREATE FULLTEXT INDEX — a distinct standalone-statement
-- vocabulary reCreateIndex does not know at all, even though this package
-- already recognizes FULLTEXT as inline/ADD index vocabulary elsewhere
-- (reduce.go's isInlineKeyIndexForm/isAddKeyIndexForm) — an inconsistency
-- internal to this reducer, not a new dialect gap.
CREATE FULLTEXT INDEX idx_articles_body ON articles (body);

CREATE TABLE venues (
    venue_id integer NOT NULL,
    location geometry
);

-- MySQL/T-SQL: CREATE SPATIAL INDEX — same standalone-statement gap as
-- FULLTEXT above.
CREATE SPATIAL INDEX idx_venues_location ON venues (location);

CREATE TABLE documents (
    document_id integer NOT NULL,
    payload_xml xml
);

-- T-SQL: CREATE XML INDEX — an XML-typed column's index, unrelated to
-- reCreateIndex's grammar.
CREATE XML INDEX idx_documents_payload ON documents (payload_xml);

CREATE TABLE catalog_items (
    catalog_item_id integer NOT NULL,
    payload_xml xml
);

-- T-SQL: CREATE PRIMARY XML INDEX — the mandatory first XML index on a
-- table before any secondary XML index can be declared.
CREATE PRIMARY XML INDEX idx_catalog_items_payload ON catalog_items (payload_xml);
