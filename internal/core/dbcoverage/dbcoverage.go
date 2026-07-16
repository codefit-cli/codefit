package dbcoverage

// Deterministic returns the DB dimension's deterministic coverage prose — the
// classes the DB rules affirm with certainty 1.0. Every entry here is verbatim
// text relocated from a language provider's CoverageManifest (a pure LOCATION
// move, not a content edit); it still describes the DB dimension's rules as of
// the change that wrote it, not the rules of today (Paso 2 of this change
// updates the prose, not this one).
func Deterministic() []string {
	return []string{
		"Database structure — table without a primary key (DB-050): a model with no @id and no @@id. A table with no primary key is a structurally undeniable defect, so it is affirmed deterministically. Read from the configured schema — a Prisma schema.prisma OR a directory of SQL-DDL (Flyway) migrations reconstructed to their final state (database.schema_paths). SQL-DDL parsing supports THREE dialects — PostgreSQL, MySQL, and SQL Server (T-SQL) — selected by the `database.type` setting in .codefit.yaml (postgresql | mysql | sqlserver); `sqlite` is recognized but returns an explicit \"not supported yet\" note rather than silently parsing as Postgres. All DB rules (DB-050 and below) are dialect-agnostic: they reason over the neutral db.Schema model reconstructed by whichever dialect parsed the DDL, never over dialect-specific fields, so the same rule set runs identically regardless of dialect. An unmapped type keyword in the source DDL (one the dialect's type vocabulary does not recognize) falls back to db.TypeUnknown — an honest fallback, never a silent guess. The DB dimension covers only what the schema states, not query behavior. NOTE (scalability): the DB rules are language-neutral (they reason over the neutral schema model, not TypeScript), so this DB coverage prose is duplicated per-provider today; it should move to a neutral DB-coverage source when a second ORM provider lands.",
	}
}

// Reasoning returns the DB dimension's coverage prose mapped as surface for the
// agent to reason over — schema-only structural facts codefit does not affirm.
// Verbatim relocation; see the Deterministic doc comment.
func Reasoning() []string {
	return []string{
		"Database structure from the schema, mapped as surface for the agent to reason (read from database.schema_paths; schema-only, no query analysis): foreign keys with no covering index (DB-001) — a FK is covered when some index's leading columns match it (the primary key counts as an implicit index, a @unique as an index); whether an un-indexed FK matters depends on the table's size and access pattern, so codefit states the fact (fk_columns, existing_indexes, covering_index_detected=false) and the agent judges. Exact duplicate indexes (DB-011) — two indexes on the same columns in the same order with the same uniqueness; which to drop is the human's call. Multivalued (array) columns (DB-002) — an array violates 1NF, but a native array (Postgres) is legitimate sometimes, so it is surfaced not affirmed. Prefix-redundant indexes ([a] subsumed by [a,b]) are NOT yet detected — a known gap for a later slice.",
		"Database structure — name-heuristic checks, mapped as surface (schema-only). These read meaning from column names, which is inherently noisy, so they are NEVER affirmed: codefit states the structural fact and the agent judges. (1) Foreign key typed as text (DB-051): a String/Text FK whose referenced key is numeric, or an unbounded @db.Text key — a String FK to a String uuid/cuid key does NOT fire (it is a structural type-mismatch check, not a name guess), and an unresolvable reference does not fire. Facts: type_mismatch, text_key, referenced_type_resolved. (2) Missing audit timestamps (DB-052): a table with NEITHER createdAt NOR updatedAt; looks_like_join_table is exposed so link tables can be dismissed. Only-one-missing is a deferred candidate, not fired yet. (3) Sensitive column stored in the clear (DB-053): a column whose name matches a sensitive token (password, token, apiKey, ssn, …) and whose type holds a value (String/Text/Bytes). It ALWAYS emits; an encryption hint in the name (passwordHash, encrypted…) is reported as the fact encryption_hint_in_name, NOT used to suppress — a name is not a guarantee, and hiding a possible plaintext secret would be a silent false negative. Names are matched by component (camelCase/snake_case), never raw substring, so passwordChangedAt (a DateTime) and passwordResetCount (an Int) do not fire. (4) Repeating groups (DB-003): two or more same-typed columns sharing a base name with numeric suffixes (phone1/phone2/phone3) — a 1NF smell the agent weighs against an intentional fixed set (address line 1/2).",
	}
}

// NotCovered returns the DB dimension's explicit gaps: what codefit does not
// audit for this dimension, stated plainly rather than left silent. Verbatim
// relocation; see the Deterministic doc comment.
func NotCovered() []string {
	return []string{
		"Database query behaviour: N+1 access patterns and index-vs-query analysis (whether an existing index serves the queries the code actually runs) are NOT covered — the DB dimension is schema-only, it does not read the application's queries.",
		"Database views, stored procedures/functions, and triggers (DB-020..041): the SQL-DDL parser records their names, but codefit has NO rules auditing them yet — a known gap, not a silent one.",
		"OLAP / data-warehouse schemas (star/snowflake models, slowly-changing dimensions, columnar/partitioning concerns): out of scope; the DB dimension audits OLTP structure only.",
		"SQL-DDL dialect known limits (declared, not silent): (1) a T-SQL GO-batched stored-procedure/trigger BODY containing a CREATE TABLE-shaped fragment may surface as a spurious top-level table — codefit does not model routine bodies; MySQL routine bodies wrapped in DELIMITER //...// are NOT affected (handled correctly). (2) A MySQL client DELIMITER directive is recognized only when its argument is punctuation (//, $$); a word-based delimiter such as DELIMITER GO is NOT recognized. (3) The T-SQL GO batch separator is recognized only when a line is exactly \"GO\"; a column literally named go alone on its own line would collide (vanishingly rare, accepted). (4) An inline index whose NAME is itself a type keyword (e.g. KEY int (col), an index named \"int\") is read as a column — the KEY/INDEX-vs-column discriminator trusts a type-named token as a column (pathological, accepted).",
		"SQL-DDL dialect assumptions: MySQL parsing assumes ANSI_QUOTES is OFF (a bare \" is read as a string literal, not an identifier quote); the parser binds a single dialect per project at construction (a project mixing dialects is not supported). A project mixing .prisma and .sql schema inputs is likewise out of scope.",
	}
}
