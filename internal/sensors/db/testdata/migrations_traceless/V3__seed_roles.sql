-- STATE (c) — DECLARES NO SCHEMA AT ALL.
--
-- Data and permissions only. codefit read every statement here and there was no
-- structure in it to read. Like V2 this leaves no position, and for a third
-- reason again distinct from blindness.
INSERT INTO role (name) VALUES ('admin');
INSERT INTO role (name) VALUES ('staff');
UPDATE role SET name = 'ADMIN' WHERE name = 'admin';
GRANT SELECT ON role TO reader;
