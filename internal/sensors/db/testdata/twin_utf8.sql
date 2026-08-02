-- Twin fixture, encoding half A: UTF-8, no byte-order mark.
--
-- twin_utf16le_bom.sql is THE SAME DDL saved as UTF-16LE with a BOM (the shape
-- pg_dump produces when PowerShell redirects its output), and
-- twin_utf16le_nobom.sql is the same bytes with the mark removed. The three are
-- kept in step by TestSensorDB_Twin_UTF16LEWithBOM_ParsesIdenticallyToUTF8.

CREATE TABLE customer (
    customer_id integer NOT NULL,
    email character varying(120) NOT NULL,
    created_at timestamp without time zone NOT NULL
);

CREATE TABLE orders (
    order_id integer NOT NULL,
    customer_id integer NOT NULL,
    total numeric(10,2) NOT NULL
);

ALTER TABLE ONLY customer
    ADD CONSTRAINT customer_pkey PRIMARY KEY (customer_id);

ALTER TABLE ONLY orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (order_id);

ALTER TABLE ONLY orders
    ADD CONSTRAINT orders_customer_fk FOREIGN KEY (customer_id) REFERENCES customer(customer_id);
