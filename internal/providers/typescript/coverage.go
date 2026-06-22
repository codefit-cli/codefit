package typescript

import "github.com/codefit-cli/codefit/internal/core/coverage"

// CoverageManifest declares, in plain prose, what codefit audits for TypeScript
// and how — the single source for the human-facing COVERAGE.md and the
// codefit-coverage tool (PRD §10, RF-07). The two categories that split between
// a deterministic rule and mapped surface (ADR 0004) appear in BOTH lists, each
// side describing exactly what it covers, so an evaluator reads a division of
// labor, not a gap.
//
// CoverageManifest is a provider method (not yet on the shared LanguageProvider
// interface) — the interface convergence is deferred until the Go provider also
// emits this (ADR 0003).
func (*Provider) CoverageManifest() coverage.Manifest {
	return coverage.Manifest{
		Language: "typescript",
		Deterministic: []string{
			"Hardcoded secrets: API keys, tokens, passwords and connection strings written as string literals, including variables named like a credential that are assigned a literal value.",
			"Weak cryptography: MD5 or SHA-1 used for hashing, and Math.random() used to generate security tokens.",
			"Dangerous code evaluation: eval() or new Function() called with a non-constant argument.",
			"SQL injection built directly in the database call — a query assembled inline by template literal or string concatenation with a variable, such as db.query(`SELECT ... ${userInput}`).",
			"Cross-site scripting through React's dangerouslySetInnerHTML when the HTML is built inline rather than from a constant.",
		},
		Reasoning: []string{
			"SQL injection where the query is assembled in steps through intermediate variables (for example: const q = \"...\" + input; db.query(q)) — codefit maps the database calls as surface so the agent can reason about where the query text came from.",
			"Cross-site scripting where dangerouslySetInnerHTML receives a variable whose safety depends on whether it was sanitized earlier — codefit maps these so the agent can judge whether the value is safe.",
			"IDOR: endpoints that access a resource by an ID — mapped so the agent verifies that ownership is checked.",
			"Broken authorization: protectable request handlers — mapped so the agent verifies authentication and authorization are enforced.",
			"Over-fetching of sensitive data: serializations of domain objects that may expose more than intended.",
		},
		NotCovered: []string{
			"Race conditions in business logic.",
			"Architectural design flaws.",
			"Business-logic correctness (this is not a security property).",
			"Deep static taint analysis — covered by surface mapping plus agent reasoning, not deterministically.",
		},
	}
}
