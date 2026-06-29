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

## TypeScript / Next.js / Express / Fastify / Prisma

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
- **Express & Fastify.** The same IDOR / broken-authorization / over-fetching
  surface above is mapped for these non-Next.js frameworks. Handlers are discovered
  **by shape, never by path** — an Express `router.<verb>('/path', …middleware,
  handler)` call, and Fastify's options-object form `.<verb>('/path', { handler,
  preHandler })` — so a same-named non-route call (`map.get('/k', v)`,
  `arr.get(0, cb)`) is not mistaken for a route (a handler needs a string-literal
  path **and** an inline function). The client id-input is read from
  `req.params`/`query`/`body` (Express) or `request.*` (Fastify), keyed off the
  handler's **first parameter** (the name is the developer's, not assumed `req`), so
  a non-standard route param like `slug` is not a blind spot. The over-fetch sink is
  the response object's `.json()`/`.send()` (Express `res`, Fastify `reply`), keyed
  off the **second parameter**. The authorization guard here is **route middleware**,
  not a body call: codefit reads Express positional middleware
  (`router.post('/x', auth.required, handler)`) and Fastify `preHandler`/`onRequest`
  hooks, and the signal states honestly whether it looked in the body or also the
  route middleware. **Cross-file (option C):** when the access/operation is not local
  to the handler body — the id is passed to a service in another file (the common
  controller→service split) — codefit emits `indirect_access=true` and names the
  callee in `indirect_call`; it does **not** follow the call across files, the agent
  reasons over the named function.

### Not covered (declared, not silent)

- Race conditions in business logic.
- Architectural design flaws.
- Business-logic correctness (not a security property).
- Deep static taint analysis — covered by surface mapping + agent reasoning, not
  deterministically.
- **NestJS and other JS frameworks** beyond Next.js, Express, and Fastify — **not
  yet covered**, a known gap, not a silent one. NestJS in particular declares routes
  through TypeScript **decorators** (`@Controller` / `@Get` / `@Body`), which the
  surface does not yet read.
- **Express/Fastify handler passed by reference.** A handler that is a named
  identifier rather than an inline function (`router.get('/x', listUsers)`, with
  `listUsers` defined elsewhere) is not enumerated — codefit maps inline handler
  bodies; a body in another scope is a cross-file case for the agent. (An auth
  **guard** by reference is unaffected — it is matched by name in the registration.)
- **Inline FormData → service frontier.** A Server Action input read inline as
  `formData.get('key')` and passed **directly** into a service call (no
  intermediate variable, no local Prisma access) may not link to that indirect
  access; bound to a variable first, it links. The local-access case always
  enumerates.

## Dependency CVEs (OSV.dev)

Cross-ecosystem, language-independent: `codefit-check-cves` reads the project's
dependency manifests and queries [OSV.dev](https://osv.dev) (free, no API key,
aggregating the GitHub Advisory Database, Go vuln DB, distro feeds and more) for
known vulnerabilities affecting the exact installed versions. codefit keeps **no
vulnerability database of its own** — the data is always fresh.

### What it reads — EXACT versions only

- **npm** — `package-lock.json` (lockfileVersion 1, 2, 3). The resolved, pinned
  versions.
- **Go** — `go.mod` (the `require` graph, direct + `// indirect`; Go pins exact
  versions via MVS).

It does **not** resolve the ranges in `package.json` (`^1.2.0`): a range is not a
version, and OSV queries an exact version. When a manifest is present without its
lockfile, codefit reports an **honest note** and checks nothing for that
ecosystem — it never guesses an installed version.

### What it reports

Per vulnerable dependency: the OSV/GHSA/CVE id, a summary, the **severity as
OSV reports it**, the first fixed version, and references.

### Not covered (declared, not silent)

- **codefit does not recompute the CVSS score.** It surfaces OSV's severity — the
  GHSA label (`LOW`/`MODERATE`/`HIGH`/`CRITICAL`) when present, else the CVSS
  vector string, else `UNKNOWN`. Computing a numeric base score from the vector is
  out of scope.
- **No lockfile → no check** for that ecosystem (reported as a note, never guessed).
- **Only `package-lock.json` and `go.mod` at the project root.** yarn/pnpm
  lockfiles and nested/monorepo manifests are not yet read.
- A Go **`replace`** redirecting to another version is not applied (the required
  version is reported); a pre-1.17 `go.mod` may under-list indirect deps (resolving
  the full graph would need the Go toolchain at runtime).

## Go

The Go provider audits codefit itself (self-audit in CI): static security
(hardcoded secrets, weak crypto) and best-practice detectors via `go/ast`, plus
authorization surface for HTTP handlers. A full prose manifest like the one above
lands when the Go provider emits one.
