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

	// Name-heuristic DB categories (slice 2b) — pure surface (ADR 0017).
	CategoryDBFKTextType           Category = "db-fk-text-type"          // DB-051: FK typed as text vs a numeric/uuid key
	CategoryDBNoTimestamps         Category = "db-no-timestamps"         // DB-052: missing audit timestamps
	CategoryDBSensitiveUnencrypted Category = "db-sensitive-unencrypted" // DB-053: sensitive-looking column stored in the clear
	CategoryDBRepeatingGroups      Category = "db-repeating-groups"      // DB-003: repeating groups (1NF smell)
)
