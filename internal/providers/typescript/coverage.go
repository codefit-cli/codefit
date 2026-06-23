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
			"Hardcoded secrets: a variable whose NAME looks like a credential (password, apiKey, token, secret, authToken, …) assigned a static string literal. codefit matches by the variable name plus a literal value — it does NOT scan values for the shape of an API key, a private key, or a connection string, so a hardcoded secret that is not tied to a credential-named variable is not caught here.",
			"Weak cryptography: MD5 or SHA-1 hashing — called directly (md5(x), sha1(x)) or via createHash('md5'|'sha1'). These are flagged WHEREVER they appear; a non-security use such as a cache key or an ETag may therefore be a false positive, because deciding whether a hash is security-relevant requires following the data, which is surface. Also flagged: Math.random() assigned to a security-named variable (token, nonce, salt, …), which is not a cryptographically secure source.",
			"Dangerous code evaluation: eval() or new Function() called with a non-constant argument — an identifier, a call, a concatenation, or an interpolated template. A constant string-literal argument is static code and is not flagged.",
			"SQL injection built directly in the database call — a query passed to .query() or .execute() that is assembled inline by string concatenation or by an interpolated template literal, such as db.query(`SELECT ... ${userInput}`). When the query is assembled through an intermediate variable instead, that is surface (below), not here.",
			"Cross-site scripting through React's dangerouslySetInnerHTML when the __html value is built inline — by concatenation or by an interpolated template. When __html is set from a plain variable, its safety depends on earlier sanitization, which is surface (below), not here; a constant __html is not flagged.",
		},
		Reasoning: []string{
			"SQL injection where the query is assembled in steps through intermediate variables (for example: const q = \"...\" + input; db.query(q)) — codefit maps the database calls as surface so the agent can reason about where the query text came from.",
			"Cross-site scripting where dangerouslySetInnerHTML receives a variable whose safety depends on whether it was sanitized earlier — codefit maps these so the agent can judge whether the value is safe.",
			"IDOR: Next.js App Router handlers that receive a client-controlled identifier (route param, query string, or request body) and reach a resource — mapped so the agent verifies ownership is checked. codefit enumerates the id-input idioms and the local Prisma access deterministically; when the id leaves the handler body (passed to a service/repository call), it is still enumerated with an honest signal saying the access may be indirect, and the agent follows the data. An id reached only after several intermediate revalidation steps may not be linked to its access — that deeper data-follow is the agent's, declared here (ADR 0005).",
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
