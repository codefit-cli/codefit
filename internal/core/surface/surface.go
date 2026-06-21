package surface

// Category is a class of auditable surface the agent reasons about. These match
// the Category field of findings.SurfaceItem.
type Category string

const (
	CategoryIDOR      Category = "idor"      // endpoints that access a resource by ID
	CategoryAuthz     Category = "authz"     // protectable handlers
	CategoryOverfetch Category = "overfetch" // serializations of domain objects
)
