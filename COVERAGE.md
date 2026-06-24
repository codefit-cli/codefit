# Coverage manifest

What codefit audits, and how. This is the honesty contract: it states what is
detected deterministically (codefit **affirms** it), what is mapped as surface
for the agent to reason (codefit **asks**), and what is **not covered** at all —
so a blind spot is *declared and known*, never silent (PRD §10).

> Source of truth: the per-provider manifest in code
> (`internal/providers/<lang>/coverage.go`, exposed by the `codefit-coverage`
> tool). This file mirrors it for human reading; once the MCP server lands,
> `codefit-coverage` generates it. Today only the **TypeScript** provider has a
> full manifest.

## TypeScript / Next.js / Prisma

### Deterministic — codefit affirms (certainty 1.0)

- **Hardcoded secrets.** A variable whose **name** looks like a credential
  (`password`, `apiKey`, `token`, `secret`, `authToken`, …) assigned a static
  string literal. Matched by variable **name + literal value** — codefit does NOT
  scan values for the shape of an API key, private key, or connection string, so a
  hardcoded secret not tied to a credential-named variable is not caught here.
- **Weak cryptography.** MD5 or SHA-1 hashing — `md5(x)`, `sha1(x)`, or
  `createHash('md5'|'sha1')`. **Known limit:** these are flagged **wherever they
  appear**; a non-security use (a cache key, an ETag) may be a false positive,
  because deciding whether a hash is security-relevant means following the data
  (surface). Also flagged: `Math.random()` assigned to a security-named variable
  (`token`, `nonce`, `salt`, …) — not a cryptographically secure source.
- **Dangerous code evaluation.** `eval()` / `new Function()` with a non-constant
  argument (an identifier, call, concatenation, or interpolated template). A
  constant string-literal argument is static code and is not flagged.
- **SQL injection — inline.** A query passed to `.query()` / `.execute()`
  assembled **inline** by string concatenation or an interpolated template, e.g.
  ``db.query(`SELECT ... ${userInput}`)``. Assembly through an intermediate
  variable is **surface** (below).
- **XSS — inline.** React `dangerouslySetInnerHTML` whose `__html` is built
  **inline** by concatenation or an interpolated template. A plain-variable
  `__html` (sanitized earlier?) is **surface**; a constant `__html` is not flagged.

### Reasoning — codefit maps surface, the agent judges

- **IDOR.** Next.js App Router handlers that receive a client-controlled
  identifier (route param, query string, or request body) and reach a resource —
  mapped so the agent verifies ownership is checked. codefit enumerates the
  id-input idioms and the local Prisma access; when the id leaves the handler body
  (passed to a service/repository), it is still enumerated with an honest "the
  access may be indirect" signal and the agent follows the data. **Known limit:**
  an id reached only after several revalidation steps may not be linked to its
  access — that deeper data-follow is the agent's. id-input is matched by
  structure, not by name, so a filter query param (`date`, `limit`) may be
  over-enumerated; the signal names the key so the agent dismisses it at a glance —
  accepted to avoid a name-based blind spot for non-standard id names.
- **Broken authorization.** Handlers that perform a sensitive operation — touch
  data or mutate state (a Prisma read/write, or an indirect service call) — mapped
  with a signal stating the operation and whether a known authz helper was detected
  in the body. Broader than IDOR (needs no client id), so it enumerates more
  handlers. Matched by the structural operation, **never by route name** (a path
  without `admin` may still need authorization). The queryable fact
  `known_authz_detected=false` means "no known authz pattern was detected here",
  **never** "this is unauthorized".
- **Over-fetching.** Points where a domain object is serialized to the response
  (`Response.json` / `NextResponse.json` / `JSON.stringify`) from a Prisma find —
  mapped with the fact `field_limiting_detected` (a `select`/`omit` clause present
  or not). codefit does **not** judge whether the exposed fields are sensitive —
  it doesn't know `passwordHash` is sensitive and `name` is not; that needs the
  schema and is the agent's. Serialization through a service is the frontier
  (codefit can't see the field selection). Matched by the serialization, never by
  model name.

### Not covered (declared, not silent)

- Race conditions in business logic.
- Architectural design flaws.
- Business-logic correctness (not a security property).
- Deep static taint analysis — covered by surface mapping + agent reasoning, not
  deterministically.

## Go

The Go provider audits codefit itself (self-audit in CI): static security
(hardcoded secrets, weak crypto) and best-practice detectors via `go/ast`, plus
authorization surface for HTTP handlers. A full prose manifest like the one above
lands when the Go provider emits one.
