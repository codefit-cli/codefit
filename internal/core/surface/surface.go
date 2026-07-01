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
)
