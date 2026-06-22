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
}
