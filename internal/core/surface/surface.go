package surface

// Category is a class of auditable surface the agent reasons about. These match
// the Category field of findings.SurfaceItem.
type Category string

const (
	CategoryIDOR      Category = "idor"      // endpoints that access a resource by ID
	CategoryAuthz     Category = "authz"     // protectable handlers
	CategoryOverfetch Category = "overfetch" // serializations of domain objects

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

	// Name-heuristic DB categories (slice 2b) — pure surface (ADR 0017).
	CategoryDBFKTextType           Category = "db-fk-text-type"          // DB-051: FK typed as text vs a numeric/uuid key
	CategoryDBNoTimestamps         Category = "db-no-timestamps"         // DB-052: missing audit timestamps
	CategoryDBSensitiveUnencrypted Category = "db-sensitive-unencrypted" // DB-053: sensitive-looking column stored in the clear
	CategoryDBRepeatingGroups      Category = "db-repeating-groups"      // DB-003: repeating groups (1NF smell)
)
