package surface

// Category is a class of auditable surface the agent reasons about. These match
// the Category field of findings.SurfaceItem.
type Category string

const (
	CategoryIDOR      Category = "idor"      // endpoints that access a resource by ID
	CategoryAuthz     Category = "authz"     // protectable handlers
	CategoryOverfetch Category = "overfetch" // serializations of domain objects
	CategoryNPlus1    Category = "nplus1"    // a query call sits inside a loop (dimension db)

	// DB-structure surface categories (schema-only rules, dimension "db"). One
	// category per rule so a baseline fingerprint is distinct per rule, the same
	// reason a finding's fingerprint carries its rule ID. These do not participate
	// in the scan-all endpoint bucketing (idor/authz/overfetch) — the DB sensor is
	// standalone.
	CategoryDBFKNoIndex   Category = "db-fk-no-index"        // DB-001: FK with no covering index
	CategoryDBDupIndex    Category = "db-duplicate-index"    // DB-011: exact duplicate index
	CategoryDBMultivalued Category = "db-multivalued-column" // DB-002: multivalued (array) column

	// CategoryDBViewSensitiveColumn (DB-020, Phase 2.2): a VIEW's top-level
	// SELECT column list exposes a column/alias whose name matches a
	// sensitive token (the same name-heuristic vocabulary CategoryDBSensitive
	// Unencrypted/DB-053 already uses). Body-derived (View.Body), unlike the
	// structural DB-001/011/002 categories above — grouped with the name-
	// heuristic block below it would also be reasonable; it lives here
	// because it is schema-STRUCTURE-adjacent (a view definition), not a
	// column-type heuristic.
	CategoryDBViewSensitiveColumn Category = "db-view-sensitive-column"

	// CategoryDBPrefixRedundantIndex (DB-011's prefix-redundant half, Unit E,
	// Phase 2.2): an index [a] whose columns are a strict leading prefix of
	// another index-like column list [a,b] on the same table (a real
	// composite index, or the primary key treated as an implicit index, same
	// as DB-001/rules.go's indexLike). Distinct from CategoryDBDupIndex
	// (DB-011's EXACT-duplicate case, same length): the two are mutually
	// exclusive by construction (a strict prefix is, by definition, strictly
	// SHORTER than what subsumes it). Closes the gap declared at
	// coverage.go:34 ("Prefix-redundant indexes ... are NOT yet detected").
	CategoryDBPrefixRedundantIndex Category = "db-prefix-redundant-index"

	// CategoryDBRoutineNoExceptionHandling (DB-031, 0.2.3 routine-body rules):
	// a stored procedure/function whose captured body contains NO exception-
	// handling construct for its dialect (T-SQL BEGIN TRY, MySQL DECLARE ...
	// HANDLER, PL/pgSQL EXCEPTION WHEN). This states an ABSENCE as a structural
	// fact, never an affirmation of a defect: whether the missing handler
	// matters — and whether a PRESENT handler is adequate (an empty CATCH is
	// "present" here yet may still be a bug) — is the agent's judgment, not
	// codefit's. Body-derived (Procedure.Body), read through the same bounded,
	// string/comment-aware token scanner discipline as DB-020, never a general
	// SQL parser. Gated on Body.Complete (ADR 0004/0025): a body the parser
	// could not prove whole is never evaluated, so an absence over unread text
	// is never falsely affirmed.
	CategoryDBRoutineNoExceptionHandling Category = "db-routine-no-exception-handling"

	// CategoryDBTriggerCrossTableCascade (DB-040, 0.2.3 routine-body rules): a
	// TRIGGER whose body performs DML (INSERT/UPDATE/DELETE) against a table
	// OTHER than the trigger's OWN table — a cross-table cascade. It states the
	// structural FACT ("this trigger writes to other table(s): X, Y") plus
	// whether a comment documents the write near it (documented_by_comment),
	// never an affirmation of a defect: whether the cascade is intentional and
	// correct is the AGENT's judgment. Body source is per-dialect: MySQL/T-SQL
	// triggers carry an inline Body scanned directly; a PostgreSQL trigger has
	// NO inline body (ADR 0026) — its logic lives in the executed function,
	// resolved via Schema.ExecutedProcedure(t), whose own Body is scanned
	// instead; when that resolution yields nothing (a built-in like
	// tsvector_update_trigger), the rule abstains for that trigger. Read through
	// the same bounded, string/comment-aware token scanner discipline as DB-020/
	// DB-031, never a general SQL parser. Gated on the scanned body's
	// Body.Complete (ADR 0004/0025).
	CategoryDBTriggerCrossTableCascade Category = "db-trigger-cross-table-cascade"

	// CategoryDBTriggerExternalCall (DB-041, 0.2.3 routine-body rules): a TRIGGER
	// whose body invokes an EXTERNAL-EFFECTING call — one that reaches OUTSIDE the
	// database (a shell exec, OLE automation, email, cross-database/remote query,
	// async notification, or an untrusted-language routine). It states the
	// structural FACT ("this trigger makes external call(s): X"), never an
	// affirmation that the call is unsafe — the exploitability is the AGENT's
	// judgment. STRICT vocabulary per dialect: a plain EXECUTE/CALL of an internal
	// stored procedure is NOT external and does not fire (the noise a loose rule
	// would create). Body source is per-dialect, resolving a PostgreSQL trigger to
	// its executed function (ADR 0026) exactly as DB-040 does. Bounded, string/
	// comment-aware token scanner; gated on Body.Complete (ADR 0004/0025).
	CategoryDBTriggerExternalCall Category = "db-trigger-external-call"

	// CategoryDBDynamicSQLInRoutine (DB-030, 0.2.3 routine-body rules): a stored
	// procedure or function whose body CONSTRUCTS and runs SQL at runtime from a
	// string (T-SQL sp_executesql / EXEC(@var), PL/pgSQL EXECUTE over a built
	// string / format() / quote_*, MySQL PREPARE ... FROM). It states the FACT
	// that dynamic SQL is built here; whether it is injectable is the AGENT's
	// judgment (codefit maps the surface, it does not do taint analysis). A static
	// EXEC/CALL of a named internal procedure is NOT dynamic SQL and does not fire.
	// Bounded string/comment-aware scanner; gated on Body.Complete (ADR 0004/0025).
	CategoryDBDynamicSQLInRoutine Category = "db-dynamic-sql-in-routine"

	// CategoryDBFilteredColumnNoIndex (DB-010, index-vs-query cross): a query in the
	// CODE filters by a column that the SCHEMA does not index — no index-like list
	// (an index, a unique constraint, or the primary key) has that column as a
	// LEADING prefix (db.CoveredByOrderedPrefix). It is the first rule of the code↔schema
	// cross (ADR 0029): unlike the schema-only DB categories, establishing it needs
	// BOTH the parsed schema AND the code's query filters. It is SURFACE, not a
	// deterministic finding — the structural fact (filtered-and-uncovered) is
	// certain once reconcile matches exactly, but whether a missing index MATTERS
	// depends on the column's cardinality, the table's size, and the write load, the
	// same agent judgment DB-001 (FK without index) already defers (ADR 0004/0005,
	// ADR 0030). Reconcile abstains (and the rule emits nothing) on any inexact
	// match — the Complete==false floor.
	CategoryDBFilteredColumnNoIndex Category = "db-filtered-column-no-index"

	// CategoryDBNoCompositeIndex (DB-013, index-vs-query cross): a query in the CODE
	// filters by MULTIPLE columns of one table together (a AND b) that no composite
	// index covers — no index-like list has that column SET as its leading columns
	// (db.CoveredBySetPrefix). It is the multi-column sibling of DB-010: a
	// multi-column WHERE wants a composite index, not a standalone index per column,
	// so it is DB-013's, never split into per-column DB-010 concerns (precedence, ADR
	// 0031). SURFACE, not a finding (same reasoning as DB-010, ADR 0030): whether the
	// composite index matters — and whether column ORDER matters for a range filter,
	// which codefit does not capture — is the agent's judgment. Reconcile abstains
	// (emits nothing) on any inexact match.
	CategoryDBNoCompositeIndex Category = "db-no-composite-index"

	// DW (data-warehouse) surface categories — the star/snowflake + SCD rule
	// family (DW-0xx, RF-03 OLAP closure, slice S2). They are produced by
	// internal/core/dwrules, which reasons over the neutral schema PLUS the
	// paradigm/table-role classification (ADR 0033), and they are ALL pure
	// surface (ADR 0017): a warehouse-modelling choice is a design judgment,
	// never a structurally undeniable defect, so codefit states the observed
	// shape and the agent decides. One category per rule, same per-rule
	// baseline-fingerprint reasoning as the DB-0xx categories above.
	//
	// Every one of these yields ZERO value on a Prisma-only project: a
	// schema.prisma expresses no warehouse concept, so a Prisma schema whose
	// models happen to be named fact_/dim_ is the only way any of them can
	// even classify — see COVERAGE.md's Prisma-zero-value note.
	CategoryDWNoFactDimensionFK       Category = "dw-fact-no-dimension-fk"       // DW-001: fact table with no FK to any dimension
	CategoryDWDimensionNoSurrogateKey Category = "dw-dimension-no-surrogate-key" // DW-002: dimension keyed by a natural/composite business key
	CategoryDWNoTimeDimension         Category = "dw-no-time-dimension"          // DW-005: facts present, no time dimension
	CategoryDWSCD2NoCurrencyIndex     Category = "dw-scd2-no-currency-index"     // DW-010: SCD-2 dimension with no index serving its currency lookup
	CategoryDWMixedSCDStrategies      Category = "dw-mixed-scd-strategies"       // DW-011: SCD-1 and SCD-2 dimensions mixed in one schema
	CategoryDWNoColumnarIndex         Category = "dw-fact-no-columnar-index"     // DW-021: fact table with no dialect-recognized columnar/analytic index

	// Name-heuristic DB categories (slice 2b) — pure surface (ADR 0017).
	CategoryDBFKTextType           Category = "db-fk-text-type"          // DB-051: FK typed as text vs a numeric/uuid key
	CategoryDBNoTimestamps         Category = "db-no-timestamps"         // DB-052: missing audit timestamps
	CategoryDBSensitiveUnencrypted Category = "db-sensitive-unencrypted" // DB-053: sensitive-looking column stored in the clear
	CategoryDBRepeatingGroups      Category = "db-repeating-groups"      // DB-003: repeating groups (1NF smell)

	// CategoryDBTableStructureUnproven (db-model-completeness-contract,
	// design SS5): codefit could not reduce one or more statements affecting
	// this table, so it CANNOT TELL whether the table declares a primary
	// key. This is DB-050's ROUTED item for a table whose structure is
	// unproven (Table.Complete==false) — its OWN category, never a reuse of
	// any existing one: "this table has no primary key" and "codefit cannot
	// tell whether this table has a primary key" are different claims, and
	// blurring them is the exact sin this contract exists to kill. Emitted
	// ONLY as DB-050's routed affirmation — never widened into a generic
	// per-table "unproven" item for the other seven absence-based rules,
	// which abstain silently instead (the per-scan Result.Note inventory is
	// their carrier, design SS7a).
	CategoryDBTableStructureUnproven Category = "db-table-structure-unproven"
)
