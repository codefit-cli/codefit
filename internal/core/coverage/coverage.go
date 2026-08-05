package coverage

// Manifest declares, for one language, which classes of problems codefit covers
// and how — and, honestly, which it does not (PRD section 10).
type Manifest struct {
	Language string
	// Deterministic lists the classes codefit detects with coded patterns
	// (certainty 1.0): hardcoded secrets, SQL/command injection, weak crypto, ...
	Deterministic []string
	// Reasoning lists the classes codefit maps as surface for the agent to
	// reason about: IDOR, broken authz, over-fetching, ...
	Reasoning []string
	// NotCovered lists, explicitly, what codefit does not audit: race
	// conditions, architectural design flaws, business-logic correctness, ...
	NotCovered []string
	// DeliveredElsewhere lists capabilities the PRD promises under one
	// identifier that codefit delivers under ANOTHER — the capability exists,
	// but not as a rule carrying the promised id (N+1 is promised as DB-201 and
	// shipped as the provider's nplus1 surface category).
	//
	// It is a third answer, not a variant of the other two. Silence would hide a
	// capability the agent could have used; NotCovered would call a shipped
	// capability absent. Each entry carries BOTH names and enough prose to
	// follow the mapping (ADR 0057).
	DeliveredElsewhere []string
}
