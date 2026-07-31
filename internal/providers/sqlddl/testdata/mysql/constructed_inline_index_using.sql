-- CONSTRUCTED / SYNTHETIC fixture -- NOT vendored, NOT copied from any
-- upstream corpus. Authored for codefit's own test suite (F1, coordinator
-- review of index-method-capture) to prove that MySQL's inline and ALTER TABLE
-- ADD KEY|UNIQUE ... USING BTREE|HASH index_option is captured into
-- db.Index.Method.
--
-- Why constructed: real vendored Sakila (both sakila_excerpt.sql and
-- sakila_real_objects.sql) contains ZERO "USING" clauses anywhere -- a test
-- written against that corpus would pass VACUOUSLY. Every statement below is
-- real, valid MySQL syntax (index_type / index_option per MySQL's own CREATE
-- TABLE / ALTER TABLE grammar) -- only the assembly into one small schema is
-- synthetic.

CREATE TABLE widgets (
    id int NOT NULL,
    sku varchar(64),
    label varchar(255),
    UNIQUE KEY uq_widgets_sku (sku) USING BTREE,
    KEY idx_widgets_label (label) USING HASH
);

CREATE TABLE gadgets (
    id int NOT NULL,
    code varchar(64)
);

ALTER TABLE gadgets ADD KEY idx_gadgets_code (code) USING BTREE;
