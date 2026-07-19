# Coverage manifest

What codefit audits, and how. This is the honesty contract: it states what is
detected deterministically (codefit **affirms** it), what is mapped as surface
for the agent to reason (codefit **asks**), and what is **not covered** at all —
so a blind spot is *declared and known*, never silent (PRD §10).

> Source of truth: the per-provider manifest in code
> (`internal/providers/<lang>/coverage.go`, exposed by the `codefit-coverage`
> tool as JSON), composed with the neutral DB dimension's own coverage source
> (`internal/core/dbcoverage/dbcoverage.go`, schema-driven and
> language-independent — appended into every provider's manifest, never
> duplicated per provider). This file is a **hand-maintained mirror** of that
> composed manifest for human reading — the MCP server has landed, but
> `codefit-coverage` returns JSON, not markdown, so this file is kept in sync
> manually whenever the in-code manifest changes. Today only the
> **TypeScript** provider has a full manifest.

## TypeScript / Next.js / Express / Fastify / NestJS / Prisma

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
- **Table without a primary key (DB-050).** A model with no `@id`/`@@id`, read from
  the configured schema — a Prisma `schema.prisma` **or** a directory of SQL-DDL
  (Flyway) migrations reconstructed to their final state (`database.schema_paths`).
  SQL-DDL parsing supports **three dialects — PostgreSQL, MySQL, and SQL Server
  (T-SQL)** — selected by `database.type` in `.codefit.yaml`
  (`postgresql` | `mysql` | `sqlserver`); `sqlite` is recognized but returns an
  explicit "not supported yet" note rather than silently parsing as Postgres. All
  DB rules (DB-050 and below) are **dialect-agnostic**: they reason over the
  neutral `db.Schema` model regardless of which dialect parsed the DDL. An
  unmapped type keyword falls back to `db.TypeUnknown` — an honest fallback, never
  a silent guess. A table with no primary key is structurally undeniable, so it is
  **affirmed**. The DB dimension covers only what the schema states — no query
  analysis.

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
- **N+1 query-in-loop pattern (DB-201).** Every query call site — a local Prisma
  access OR a call at the cross-function frontier (the same service/repository
  frontier IDOR/authz/over-fetching already declare, reusing `isPrismaCall`/
  `isServiceCall` verbatim) — that sits lexically inside a loop construct: a
  `for`/`for...of`/`for...in`/`while`/`do...while` statement, or a per-element
  callback iteration (`.forEach`/`.map`/`.flatMap`/`.filter`/`.reduce`/`.some`/
  `.every`/`.find`). Reuses the same handler-discovery mechanism as IDOR/authz/
  over-fetching (`auditTargets`), so it applies uniformly to Next.js route
  handlers and Server Actions, Express, Fastify, and NestJS, with no separate
  per-framework detector. Per ADR 0005 it is **ordered, never filtered**: a loop
  over a literal array of 3 elements is enumerated exactly like a loop over an
  unbounded query result — the iterated source is named as a fact (e.g. *"a
  literal array of 3 element(s)"*, *"the variable 'users'"*) so the agent
  dismisses an obviously-bounded loop at a glance, never filtered away. A call
  wrapped in `Promise.all(...)` is still enumerated as one query per element
  (`promise_all_wrapped`, vs. a directly-awaited sequential call,
  `awaited_in_loop`); `nested_loop` is exposed when the query sits under more
  than one enclosing loop. A cross-function-frontier call carries an honest
  signal that codefit did not follow it — whether it performs a per-iteration
  query is **not verified**. This is a database access pattern conceptually, but
  it is mapped as **per-endpoint surface** (from the application's code, not the
  schema), so it appears in `scan-all`'s endpoint buckets, **never in the
  schema-only DB section below**.
- **Express & Fastify.** The same IDOR / broken-authorization / over-fetching /
  N+1 surface above is mapped for these non-Next.js frameworks. Handlers are discovered
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
- **NestJS.** Same IDOR / authz / over-fetching / N+1 surface, for routes declared as
  decorated class methods. A handler is a method with an **HTTP-verb decorator**
  (`@Get`/`@Post`/…), detected by that shape, never by `@Controller`. The client
  id-input comes from the method's **parameter decorators** (`@Param('id')`,
  `@Query()`, `@Body()`) — a `@User`-style injected principal is not treated as a
  resource id. The authorization guard is **`@UseGuards`**, on the method or
  inherited from the class, detected by **presence** (the decorator is the
  mechanism; guard names are arbitrary, so codefit names the guard but does not
  match a known set). The over-fetch sink is the **return value** (NestJS serializes
  it, like a Server Action). A service call in another file is the cross-file
  frontier (option C). **Known limit:** a service method whose name collides with a
  Prisma method (`create`/`update`/`delete`/…) is reported as a *local* Prisma
  access, not an indirect call (still surfaced, with the real callee named); a
  handler returning through an explicit `@Res()` is not mapped. **Guards:** codefit
  detects `@UseGuards` (method- and class-level) only — NestJS auth applied as
  **module-bound middleware** (`consumer.apply(AuthMiddleware)` in a module's
  `configure()`) or a **global guard** (`APP_GUARD` / `app.useGlobalGuards`) is not
  detected, so an app that guards via middleware reads `known_authz_detected: false`
  across the board (a conservative *verify*, never a false *secure*).
- **Database structure (from the schema, no query analysis).**
  - **FK with no covering index (DB-001).** A foreign key is *covered* when some
    index's **leading columns** match it — the **primary key counts as an implicit
    index**, a `@unique` as an index. Whether an un-indexed FK matters depends on the
    table's size/access pattern, so codefit states the fact (`fk_columns`,
    `existing_indexes`, `covering_index_detected: false`) and the agent judges.
  - **Exact duplicate index (DB-011a).** Two indexes on the same columns, same
    order, same uniqueness — a pure write/storage cost; which to drop is the
    human's call. **Dialect-uneven real-world coverage:** on **PostgreSQL/Pagila**
    this rule fires on a **genuine upstream duplicate** (the `payment_p2022_01`
    partition ships both `idx_fk_payment_p2022_01_customer_id` and
    `payment_p2022_01_customer_id_idx` on the identical column) — the only one of
    the three dialects where the positive case is proven by real vendored DDL, not
    a constructed one. On **MySQL/Sakila** and **SQL Server/AdventureWorks** the
    rule is verified **clean** against real DDL (no false positives), but its
    positive fire path is proven only by a **constructed (synthetic)** schema,
    since neither real corpus ships a duplicate index.
  - **Prefix-redundant index (DB-011b).** An index `[a]` that is a **strict
    leading prefix** of a wider index-like coverer `[a,b]` on the same table (a
    real index, or the primary key as an implicit index) — pure write/storage
    overhead, which to drop is the human's call. A **`UNIQUE` index never fires**
    (it enforces a constraint the wider composite doesn't guarantee alone).
    **Supported on all three dialects** — Pagila/Sakila/AdventureWorks all run
    **clean** against real vendored DDL (none of the three real corpora happens
    to contain a genuine prefix-redundant pair — an honest finding about how
    these schemas are indexed, not a rule gap); the positive fire path on every
    dialect is proven by mutating a copy of a real table (e.g. adding a synthetic
    index on the leading column of Sakila's own real `film_actor` composite
    primary key `[actor_id, film_id]`), never a fully synthetic fixture.
  - **Multivalued (array) column (DB-002).** An array violates 1NF, but a native
    array (Postgres) is legitimate sometimes — surfaced, not affirmed.
- **Database structure — name-heuristic checks (schema-only).** These read meaning
  from column names, so they are **never affirmed** — codefit states the fact, the
  agent judges. Names are matched **by component** (camelCase/snake_case), never raw
  substring.
  - **FK typed as text (DB-051).** A `String`/`Text` FK whose referenced key is
    **numeric**, or an unbounded `@db.Text` key. A `String` FK to a `String`
    uuid/cuid key does **not** fire (it is a structural type-mismatch check, not a
    name guess); an unresolvable reference does not fire. Facts: `type_mismatch`,
    `text_key`, `referenced_type_resolved`.
  - **Missing audit timestamps (DB-052).** A table with **neither** `createdAt`
    **nor** `updatedAt`. `looks_like_join_table` is exposed so link tables can be
    dismissed. "Only one missing" is a **deferred candidate**, not fired yet.
  - **Sensitive column in the clear (DB-053).** A column whose name matches a
    sensitive token (`password`, `token`, `apiKey`, `ssn`, …) held in a
    `String`/`Text`/`Bytes` type. It **always emits**; an encryption hint in the
    name (`passwordHash`, `encrypted`…) is reported as `encryption_hint_in_name`,
    **not** used to suppress — a name is not a guarantee, and hiding a possible
    plaintext secret would be a silent false negative. `passwordChangedAt`
    (DateTime) and `passwordResetCount` (Int) do not fire (type filter).
  - **Repeating groups (DB-003).** Two or more same-typed columns sharing a base
    name with numeric suffixes (`phone1/phone2/phone3`) — a 1NF smell weighed
    against an intentional fixed set (address line 1/2).
- **View sensitive-column exposure (DB-020).** A **`VIEW`** whose top-level
  `SELECT` column list exposes a column or alias whose name matches a sensitive
  token (the same vocabulary as DB-053). Read through a deliberately **bounded**
  `SELECT`-projection scanner, never a general SQL-expression parser: a function
  call, `CASE`, or subquery item with no alias is a declared miss for that one
  item, and `SELECT *` is a declared miss for the **whole view**. **Never an
  affirmation** — a column named `password` in a view says nothing about whether
  the value is masked or genuinely exposed, so this is always surface.
  **Runs clean on real DDL across all three dialects** — Pagila's `actor_info`,
  AdventureWorks' `vEmployee`, and Sakila's `customer_list` all legitimately
  confirm the negative case (a real, well-designed view's whole purpose is
  usually a curated projection, so it rarely re-exposes a sensitive column under
  a sensitive-looking name — a real finding about how views are written, not a
  gap in the rule). **The positive fire path is proven only by a constructed
  case** — renaming the real, vendored Sakila `customer_list` view's own
  trailing `AS SID` alias to `AS ssn`. No real view in any of the three
  dogfooded corpora genuinely exposes a sensitive-named column; that is stated
  plainly, not implied by the clean runs above. **Zero value on Prisma-only
  projects:** Prisma's `schema.prisma` has no view-block concept (ADR 0014
  places it out of scope), so a Prisma-only project has no views for this rule
  to read at all.

### Not covered (declared, not silent)

- Race conditions in business logic.
- Architectural design flaws.
- Business-logic correctness (not a security property).
- Deep static taint analysis — covered by surface mapping + agent reasoning, not
  deterministically.
- **JS server frameworks beyond Next.js, Express, Fastify, and NestJS** — **not yet
  covered**, a known gap, not a silent one.
- **Index-vs-query analysis** — whether an existing index actually serves the
  queries the application code runs — is **not** covered; the DB dimension is
  schema-only, it does not read query text or application code. (**N+1
  query-in-loop patterns are a different capability and are no longer part of
  this gap** — N+1 is mapped as per-handler surface by the language provider,
  not by the schema-only DB dimension; see the N+1 entry above. It appears in
  `scan-all`'s endpoint buckets, never in this DB section.)
- **Never-used index (DB-012)** is **not** covered, and this is **permanent**,
  not deferred: detecting an unused index requires runtime query telemetry
  (e.g. PostgreSQL's `pg_stat_user_indexes`) that only exists inside a live,
  running database with real traffic history. codefit's model is static and
  never connects to a database — it reads only DDL/schema text — so this rule
  is structurally incompatible with how codefit operates, not merely
  unscheduled.
- **Database views ARE covered** for one rule (DB-020, sensitive-column
  exposure — see above). **Stored procedures/functions and triggers** (DB-030
  dynamic SQL by string concatenation, DB-031 missing exception handling,
  DB-040 trigger cross-table DML, DB-041 trigger external-effecting call) are
  **not** covered in this release — the SQL-DDL parser records their names and,
  where the dialect allows, their bodies, but no rule reads them yet. This is
  **deferred, not abandoned**: it moves to a separate change
  (`routine-body-rules`, a later slice of `0.2.3`). The parser prerequisite is
  now **done** — a multi-statement T-SQL routine body is captured **complete**
  to the `GO` batch separator (or EOF), **not** truncated at its first internal
  `;` (ADR 0027; PostgreSQL dollar-quoted and MySQL `DELIMITER`-wrapped bodies
  were already complete). The blocker is lifted and the full body text is
  available marked complete; what remains is simply that the four rules are
  **not implemented yet** — no longer a parser gap. (Had they shipped over the
  old truncated T-SQL body, a rule like DB-031 ("is exception handling
  present?") would have **falsely affirmed** an absence that was really just
  unread text past the cut — which is exactly why the parser fix came first.)
  When the routine-body rules land, they will carry the same Prisma-zero-value
  limit as DB-020: Prisma's `schema.prisma` has no stored-procedure/trigger
  block concept, so a Prisma-only project gets no value from this rule family
  either.
- **OLAP / data-warehouse schemas** (star/snowflake, slowly-changing dimensions,
  columnar/partitioning) — out of scope; the DB dimension audits OLTP structure only.
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
- **SQL-DDL dialect known limits (declared, not silent).** (1) A T-SQL routine
  body is captured to the `GO` batch separator (or EOF), so a `CREATE TABLE`-shaped
  fragment inside a `GO`-batched procedure/trigger body is **absorbed into the
  body**, not surfaced as a spurious top-level table (ADR 0027, closing a limit
  ADR 0022 had declared); the trade is that a T-SQL routine with **no trailing
  `GO`** immediately followed by another statement absorbs that statement into
  the body — invalid T-SQL batching, the intentional boundary of ADR 0027. MySQL
  routine bodies wrapped in `DELIMITER //`...`//` are unaffected. (2) A MySQL client `DELIMITER` directive is
  recognized only when its argument is punctuation (`//`, `$$`); a word-based
  delimiter such as `DELIMITER GO` is **not** recognized. (3) The T-SQL `GO` batch
  separator is recognized only when a line is exactly `GO`; a column literally
  named `go` alone on its own line would collide (vanishingly rare, accepted).
  (4) An inline index whose **name** is itself a type keyword (e.g. `KEY int
  (col)`, an index named "int") is read as a column — the KEY/INDEX-vs-column
  discriminator trusts a type-named token as a column (pathological, accepted).
  (5) A column named exactly `key`, `index`, `fulltext`, or `spatial` whose type
  is **not** in the dialect's recognized type vocabulary (e.g. PostgreSQL's
  `tsvector`, as in real Pagila's `film.fulltext` column) collides with the
  **same** inline-index-shorthand heuristic from a different direction: the
  column is silently dropped and a phantom zero-column index is fabricated in
  its place instead. Confirmed against real vendored Pagila DDL; **not yet
  fixed**.
- **SQL-DDL dialect assumptions.** MySQL parsing assumes `ANSI_QUOTES` is OFF (a
  bare `"` is read as a string literal, not an identifier quote); the parser
  binds a **single dialect per project** at construction (a project mixing
  dialects is not supported). A project mixing `.prisma` and `.sql` schema
  inputs is likewise out of scope.

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
