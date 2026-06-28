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

- **IDOR.** Next.js App Router **route handlers AND Server Actions** (`"use
  server"`) that receive a client-controlled identifier and reach a resource —
  mapped so the agent verifies ownership is checked. For a route handler the
  id-input is read from the request (route param, query string, request body); for
  a **Server Action** it arrives as the function's **arguments** (or a `FormData`),
  because an action is a POST endpoint whose input *is* its arguments. Server
  Actions are detected **by shape** — an async function under a `"use server"`
  directive at file level (every exported async function) or at function level (an
  inline action in a Server Component, or a non-exported one) — **never by
  filename**, so an action in `actions.ts`, `lib/`, or inline is not a blind spot.
  An **object-shaped argument** is covered: the parameter binding is the id-var, so
  a nested `data.id` flows to the access. When the id leaves the body (passed to a
  service/repository), it is still enumerated with an honest "the access may be
  indirect" signal and the agent follows the data. **Known limit:** an id reached
  only after several revalidation steps may not be linked to its access — the
  agent's. id-input is matched by structure, not by name, so a filter query param
  or a non-id action argument (`date`, `limit`) may be over-enumerated; the signal
  names what it read so the agent dismisses it at a glance. Facts:
  `local_access_detected` and `server_action` (true = the entry is a Server Action).
  **IDOR is about ownership, not permission:** an IDOR with a local access stays
  **actionable even when an authz helper is present** — a session/permission check
  does not prove the caller owns *this* resource, which codefit cannot verify from
  structure (ADR 0006 amended). `known_authz_detected` gates the authz concern, never
  the IDOR one.
- **Broken authorization.** Route handlers **and Server Actions** that perform a
  sensitive operation — touch data or mutate state (a Prisma read/write, or an
  indirect service call) — mapped with a signal stating the operation and whether a
  known authz helper was detected in the body. Broader than IDOR (needs no client
  id), so it enumerates more entries — and a **Server Action that mutates with no
  detected authz helper** is exactly the case worth surfacing (actions are POST
  endpoints devs often don't guard like endpoints). Matched by the structural
  operation, **never by route name** (a path without `admin` may still need
  authorization). The queryable fact `known_authz_detected=false` means "no known
  authz pattern was detected here", **never** "this is unauthorized". The recognized
  helper set is **built-in (NextAuth-style) PLUS the project's own helpers**: the
  agent identifies a custom helper (`requirePermission`, `getCurrentUser`, …) by
  reasoning over the code, a human approves, and codefit persists it in the committed
  baseline (`codefit-baseline-register-authz-helper`) and recognizes it on later scans
  without re-reasoning (ADR 0013). Registering clears the **authz** gap, never the
  **IDOR/ownership** one.
- **Over-fetching.** Points where a domain object is serialized from a Prisma find
  — for a route handler the sink is an explicit `Response.json` /
  `NextResponse.json` / `JSON.stringify`; for a **Server Action** it is the
  **return value**, which the framework serializes to the client (an action has no
  `Response.json`). Mapped with the fact `field_limiting_detected` (a
  `select`/`omit` clause present or not). codefit does **not** judge whether the
  exposed fields are sensitive — it doesn't know `passwordHash` is sensitive and
  `name` is not; that needs the schema and is the agent's. Serialization through a
  service is the frontier (codefit can't see the field selection). Matched by the
  serialization, never by model name.

### Not covered (declared, not silent)

- Race conditions in business logic.
- Architectural design flaws.
- Business-logic correctness (not a security property).
- Deep static taint analysis — covered by surface mapping + agent reasoning, not
  deterministically.
- **Non-Next.js JS frameworks.** The TypeScript surface is **Next.js-specific**
  (App Router route handlers + Server Actions). Express, Fastify, NestJS and other
  JS server frameworks are **not yet covered** — a known gap, not a silent one.
- **Inline FormData → service frontier.** A Server Action input read inline as
  `formData.get('key')` and passed **directly** into a service call (no
  intermediate variable, no local Prisma access) may not link to that indirect
  access; bound to a variable first, it links. The local-access case always
  enumerates.

## Go

The Go provider audits codefit itself (self-audit in CI): static security
(hardcoded secrets, weak crypto) and best-practice detectors via `go/ast`, plus
authorization surface for HTTP handlers. A full prose manifest like the one above
lands when the Go provider emits one.
