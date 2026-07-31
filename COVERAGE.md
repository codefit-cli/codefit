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
  A table with no primary key is structurally undeniable, so it is **affirmed** —
  but **only over a table whose structure the parser could PROVE complete**
  (`db.Table.Complete`, ADR 0034, `db-model-completeness-contract`). When one or
  more statements affecting a table could not be reduced (a dropped `ALTER TABLE`
  shape, a malformed `CREATE TABLE` body, an unrecognized Prisma model-body line),
  DB-050 does **not** affirm — it **routes** that table to a dedicated
  `db-table-structure-unproven` surface item instead (see "Table structural
  completeness" below), carrying the raw unreduced statement and `file:line` so the
  agent can read the source DDL itself. "Absence of DATA" (a genuinely missing key)
  and "absence of FEATURE" (a key the parser could not read) are **different
  claims**, and DB-050 never blurs them. SQL-DDL parsing supports **three dialects —
  PostgreSQL, MySQL, and SQL Server (T-SQL)** — selected by `database.type` in
  `.codefit.yaml` (`postgresql` | `mysql` | `sqlserver`); `sqlite` is recognized but
  returns an explicit "not supported yet" note rather than silently parsing as
  Postgres. All DB rules (DB-050 and below) are **dialect-agnostic**: they reason
  over the neutral `db.Schema` model regardless of which dialect parsed the DDL. An
  unmapped type keyword falls back to `db.TypeUnknown` — an honest fallback, never a
  silent guess. The DB dimension covers only what the schema states — no query
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
- **Index-vs-query — the code's queries crossed with the schema (`scan-all` only,
  Prisma).** Unlike every rule above (schema-only), these read BOTH sides: the WHERE
  columns of the code's Prisma queries **and** the schema's indexes. Both are
  **surface** — a missing index may or may not matter (cardinality, table size, write
  load are the agent's call), never affirmed. "Covered" counts an index's **leading
  columns**, the **primary key**, and a **`@unique`**; a filter that constrains a
  unique key is a single-row lookup and never fires. Matching is by the **logical
  field name** on both sides (Prisma `@map` physical names never enter).
  - **Filtered column with no index (DB-010).** One column the code filters on with
    nothing (index, unique, or PK) covering it as a leading column. A `Boolean` or
    `enum` column is **skipped** — low-cardinality by its declared type, where a
    standalone index is almost always wrong.
  - **Multi-column filter with no composite index (DB-013).** A `WHERE a AND b` with
    no composite index whose leading columns are that **set** — order-insensitive, so
    `[b,a]` covers `(a,b)`. The **same set recurring across many models is grouped
    into one item** listing every affected model (an architectural pattern — e.g.
    tenant scoping + soft-delete — not *N* findings).
  - **Declared limits — what keeps this channel trustworthy.** `OR`/`NOT` and nested
    relation filters are skipped at extraction. A Prisma **field name vs a physical
    SQL-DDL column name** (cross-naming-space) **abstains**. **Range predicates** are
    treated as equality — codefit does not capture the WHERE operator, a safe
    direction (false negatives, never a wrong suggestion). A **4-or-more-column
    filter abstains** — which subset to index needs selectivity codefit cannot see.
    **A column whose declared type does not reveal its real cardinality** still emits
    on DB-010, and the agent refutes it by reading the schema — one limit with two
    faces, both seen in the field: a `String` used as an enum (`Transaction.type` =
    `income`/`expense` in salonpro; `User.role` in umami) and a `DateTime?` used as a
    binary flag (`deletedAt` — null = live / set = deleted — in umami and papermark).
    `Boolean`/`enum` skipping handles the same concern but keys off the DECLARED type,
    which these two evade. Resolving both — and the range-operator case — needs the
    neutral query model to carry the WHERE's **literal values** (cardinality inferred
    from usage), a separate slice. **Cross-table (join) filters** are out of scope: a
    query filter names one model.
  - **Volume scales with the schema.** The cross emits per query-shape, so a larger
    schema surfaces more: measured **7 items on 18 models, 21 on 40, 57 on 77** — a
    steady ~1/3 of the DB channel, not a spike. It is proportional, not runaway; on a
    large monorepo expect tens of items, each a schema-anchored surface question.
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
- **Routine without exception handling (DB-031).** A **stored procedure or
  function** (both surface as `db.Procedure`) whose captured body contains **no
  exception-handling construct** for its dialect: T-SQL `BEGIN TRY` (paired with
  `BEGIN CATCH`), MySQL `DECLARE ... HANDLER`, or PL/pgSQL `EXCEPTION WHEN`. This
  states an **absence** as a structural fact, **never an affirmation** of a
  defect: whether the missing handler matters is the agent's judgment, and so is
  the **adequacy of a handler that is present** — an empty `CATCH`, or one that
  swallows every error, reads as "present" here (the construct exists) yet may
  still be a real bug. DB-031 detects the **structure**, it does not grade it.
  Read through a deliberately **bounded**, string/comment-aware token scanner
  (the same discipline as DB-020), never a general SQL parser: a handler-shaped
  token inside a string literal or comment does not false-match. **Critical:**
  PostgreSQL `RAISE EXCEPTION` is a **throw, not a handler** — the scanner
  matches `EXCEPTION` only when the very next keyword token is `WHEN`, so
  `RAISE EXCEPTION` and a bare `EXCEPTION` never count as handling. **Gated on
  `Body.Complete`** (ADR 0004/0025): a body the parser could not prove whole is
  never evaluated, so an absence over truncated text is never falsely affirmed.
  **Per-dialect coverage against real vendored DDL:**
  - **MySQL/Sakila** — real **positive** (`rewards_report`, no `HANDLER`) and
    real **negative** (`inventory_held_by_customer`, real
    `DECLARE EXIT HANDLER FOR NOT FOUND`).
  - **T-SQL/AdventureWorks** — real **positive** (`uspGetBillOfMaterials`, a
    recursive-CTE body with no `TRY`/`CATCH`) and real **negative**
    (`uspUpdateEmployeePersonalInfo`, real `BEGIN TRY ... BEGIN CATCH`).
  - **PostgreSQL** — real **positive** (Pagila `rewards_report`, which `RAISE`s
    `EXCEPTION` twice but has **no** `EXCEPTION WHEN` — the bare-`EXCEPTION`
    trap, locked by an explicit test) plus a **constructed negative**, declared
    synthetic in `testdata/pg_constructed_exception_handler.sql` (a small
    hand-written `safe_divide` with a real `BEGIN ... EXCEPTION WHEN ... END`
    block), since no routine in the dogfooded Pagila excerpt contains a real
    handler clause.

  **Triggers are intentionally out of scope** for DB-031 (their
  cross-table/external-effect risks are DB-040/DB-041's domain). **Zero value on
  Prisma-only projects:** Prisma's `schema.prisma` has no
  stored-procedure/function block concept, so a Prisma-only project has no
  routines for this rule to read.
- **Trigger cross-table cascade (DB-040).** A **trigger** whose body performs
  DML (INSERT/UPDATE/DELETE) against a table **other** than the one it fires on.
  Surface, never an affirmation: it states the facts (`writes_other_table: X`
  per other table, and `documented_by_comment`) and the **agent** judges whether
  the cascade is intentional — DB-040 detects the cross-table write, it does not
  grade it. **Body source is per-dialect (ADR 0026):** MySQL/T-SQL triggers
  carry an inline body scanned directly; a **PostgreSQL** trigger has no inline
  body, so the rule resolves `Schema.ExecutedProcedure(trigger)` to the executed
  function and scans **its** body, comparing writes against the trigger's own
  table — an unresolvable built-in (e.g. `tsvector_update_trigger`) makes the
  rule **abstain**. Bounded string/comment-aware scanner (DB-020/DB-031
  discipline), never a SQL-expression parser: a DML token inside a string or
  comment does not fabricate a write; the T-SQL `UPDATE(column)` function is
  excluded; a trigger's own event clause (`AFTER UPDATE`, `INSTEAD OF DELETE`)
  is not mistaken for a statement; and a schema-qualified write to the trigger's
  **own** table is correctly not a cascade. **Gated on `Body.Complete`** (ADR
  0004/0025). Per-dialect coverage: **T-SQL/AdventureWorks** — real POSITIVE
  (`uPurchaseOrderDetail` cascades into `TransactionHistory` and
  `PurchaseOrderHeader`, its same-table `PurchaseOrderDetail` write excluded) and
  real NEGATIVE (`dEmployee`, only RAISERROR/ROLLBACK); **MySQL/Sakila** — real
  POSITIVE (`ins_film`/`upd_film`/`del_film` cascade into `film_text`) and a
  **constructed** NEGATIVE, declared synthetic in
  `testdata/mysql/constructed_non_cascading_trigger.sql`; **PostgreSQL** — real
  NEGATIVE (Pagila `last_updated`, only sets `NEW`) and a **constructed**
  POSITIVE, declared synthetic in
  `testdata/pg_constructed_cascade_trigger.sql` (a trigger→function pair
  exercising the ADR-0026 resolution path). **Zero value on Prisma-only
  projects:** `schema.prisma` has no trigger block concept.
- **Trigger external-effecting call (DB-041).** A **trigger** whose body invokes
  a call that reaches **outside** the database (shell exec, OLE automation,
  email, remote/cross-database query, async notification, or a pipe to a
  program). Surface, never an affirmation: it states the fact (`external_call:
  X`) and the **agent** judges whether the call is safe. **Strict vocabulary is
  the line between real risk and noise:** an EXECUTE/CALL of an **internal**
  stored procedure (`EXECUTE dbo.uspLogError`, `CALL recompute_totals`) does
  **not** fire — that is the rule's trap, the token that looks like a call but is
  not external. Per-dialect vocabulary: **T-SQL** (`xp_cmdshell`, `sp_OA*`,
  `sp_send_dbmail`, `OPENROWSET`, `OPENQUERY`); **PostgreSQL** (`dblink` /
  `dblink_exec`, `NOTIFY` / `pg_notify`, `COPY ... PROGRAM`); **MySQL**
  (`sys_exec` / `sys_eval` UDFs). Body source is per-dialect, resolving a
  PostgreSQL trigger to its function (ADR 0026) as DB-040 does; gated on
  `Body.Complete`. Bounded string/comment-aware scanner: a token in a string or
  comment does not fire, and a member access (`NEW.notify`) is not the `NOTIFY`
  statement. Per-dialect coverage: **T-SQL/AdventureWorks** — a **constructed**
  POSITIVE, declared synthetic in
  `testdata/tsql/constructed_external_call_trigger.sql` (a trigger that EXECs
  `xp_cmdshell`), and a real **NEGATIVE / trap** (`uPurchaseOrderDetail`, whose
  only calls are EXECUTE of the internal `uspPrintError`/`uspLogError` logging
  procs); **PostgreSQL** — a **constructed** POSITIVE, declared synthetic in
  `testdata/pg_constructed_external_call_trigger.sql` (a trigger→function pair
  whose function issues `NOTIFY`), and a real NEGATIVE (Pagila `last_updated`);
  **MySQL** — **detectable without dogfood**: the scanner recognizes MySQL's
  external vocabulary (`sys_exec`/`sys_eval`) and would fire if one appeared, but
  a trigger making an external call is structurally rare and non-idiomatic in
  MySQL, so no real or constructed MySQL case is dogfooded. This is **distinct
  from _not covered_** (which would mean non-detectable): the capability exists,
  only the dogfood evidence is absent by the dialect's nature. **Zero value on
  Prisma-only projects.**
- **Dynamic SQL construction in a routine (DB-030).** A **stored procedure or
  function** whose body **builds and runs SQL from a string at runtime**.
  Surface, never an affirmation: it states the fact (`dynamic_sql: <marker>`) and
  the **agent** judges whether it is injectable — codefit maps the surface, it
  deliberately does **not** do taint analysis. **The trap:** a static EXEC/CALL
  of a **named internal** procedure (`EXEC dbo.uspFoo`) is **not** dynamic SQL
  and does not fire (same EXEC family as DB-041's trap, excluded for a different
  reason — there "not external", here "not dynamic"). Per-dialect markers:
  **T-SQL** (`sp_executesql`; `EXEC(<expr>)` in parentheses, not a literal proc
  name); **PL/pgSQL** (`EXECUTE '<string>'` / `EXECUTE format(...)`;
  `quote_literal` / `quote_ident`); **MySQL** (`PREPARE ... FROM`). Bounded
  string/comment-aware scanner; gated on `Body.Complete`. **Declared limit:** a
  bare `EXECUTE <variable>` with no construction marker visible in the same body
  is a miss. Per-dialect coverage: **PostgreSQL** — real POSITIVE (Pagila
  `rewards_report` builds a string with `quote_literal` and runs it via
  `EXECUTE`) and real NEGATIVE (`last_updated`); **MySQL** — a **constructed**
  POSITIVE, declared synthetic in
  `testdata/mysql/constructed_dynamic_sql_proc.sql` (`PREPARE ... FROM` a
  CONCATenated string), and a real NEGATIVE (Sakila `rewards_report`,
  temp-table based); **T-SQL** — a **constructed** POSITIVE, declared synthetic
  in `testdata/tsql/constructed_dynamic_sql_proc.sql` (`sp_executesql` over a
  built string), and a real NEGATIVE (`uspGetBillOfMaterials`). **Zero value on
  Prisma-only projects.**
- **Table structural completeness (`db-model-completeness-contract`, ADR 0034).**
  Every table carries a completeness signal (`db.Table.Complete`/`Note`/
  `Unreduced`) mirroring the pre-existing `Body.Complete` idiom (ADR 0004/0025) at
  TABLE granularity: `false` means the parser met at least one statement affecting
  the table it could NOT reduce and could not rule out as declaring a
  key/index/column. A **declared, recognized** skip (`CHECK`/`EXCLUDE`/`PARTITION`
  constraints; `ALTER COLUMN`/`RENAME`/`OWNER`/`ENABLE`/`DISABLE`/`CLUSTER`/`SET`/
  `RESET`/`VALIDATE`/`NO` alter actions) is **not** incompleteness — recording
  those would mute the whole DB dimension on ordinary PostgreSQL DDL, so only a
  genuinely unrecognized statement demotes a table. On the Prisma path, a
  model-body line that is neither a recognized field nor a `@@`-attribute demotes
  the table the same way; the deferred, unrelated top-level-line skip (a `view`
  block) does not. **Two-way disposition, no rule signature change (ADR 0015):**
  DB-050 (the dimension's one affirmation) **routes** an unproven table to the
  `db-table-structure-unproven` surface category instead of affirming; DB-001,
  DB-052, and the five DW-0xx rules **abstain silently** on an unproven table (a
  dropped statement might have declared the very index/column/key each rule is
  asking about). DW-005 and DW-011 are schema-level census judgments and abstain
  the **whole rule** — never a per-table skip, which would silently shrink the
  census and still emit. A genuinely PK-less table with a proven-complete
  structure still affirms DB-050 at confidence 1.0 — honesty costs nothing here.
  Per-scan, `sensors/db.Result.Note` (reaching the agent through scan-all's
  `DBSection.Note`) carries a bounded **inventory** of what could not be measured,
  aggregated by REASON never by table (a 200-table systematic gap is one line, not
  200) — this is an inventory of WHAT was measured, never a DIAGNOSIS of WHY the
  parser failed (no parser-internal identifiers reach scan output; the closed
  `Reason*` vocabulary in `core/db` is the type-level control). **Boundary,**
  stated so this contract does not over-promise: `Complete` covers DROPS, not
  FABRICATIONS — a reducer that believes it succeeded while inventing data
  (Pagila's `film.fulltext` phantom index, and the closed "`ADD  CONSTRAINT`"
  double-space fabrication, both documented below) reports `Complete=true`
  regardless; that class needs its own, separate control.
- **Paradigm and table-role detection, plus 3NF-suppression on OLAP-classified
  tables (S1, RF-03 OLAP closure).** codefit computes a schema's **paradigm**
  (`oltp` | `olap` | `mixed`) and each table's **warehouse role** (`fact` |
  `dimension` | `staging` | `mart` | `unclassified`) as a pure function of the
  schema: table-name prefixes (`fact_`/`dim_`/`stg_`/`mart_`) are the
  **primary** signal, corroborated by **real relational structure** — `fact_`
  tables need foreign-key fan-out to 2+ distinct tables, `dim_` tables need to
  be **referenced** (fan-in) by at least one other table. A lone single-column
  surrogate primary key is deliberately **not**, by itself, sufficient
  corroboration (nearly every ordinary OLTP table has one, so it would
  classify almost anything prefixed `fact_`/`dim_` as warehouse-role — fixed
  post-review, see ADR 0033). `stg_`/`mart_` still need no structural signal
  in S1. `database.paradigm` defaults to `"auto"` (detection decides); an
  **explicit** `oltp`/`olap`/`mixed` value in `.codefit.yaml` **always wins**
  over detection — developer autonomy is innegotiable. This slice's first
  consumer: DB-002 (multivalued column) and DB-003 (repeating groups) — still
  schema-only, deterministic 1NF checks, unchanged in shape — are
  **suppressed** for fact/dimension/mart-role tables under auto detection or
  an explicit `mixed` override (an intentionally denormalized warehouse table
  is not a 1NF violation), or dropped **schema-wide** under an explicit `olap`
  override. An explicit `oltp` override **never** suppresses, regardless of
  any detected role — the identical shape still fires exactly as before this
  slice. Suppression is never silent: when items are withheld, the note
  records how many and on how many tables, plus the `database.paradigm: oltp`
  escape hatch to see them. The DW-0xx columnar-index and partitioning rules
  are **not yet built** (see Not covered, below); the star-schema/SCD half
  landed in S2 — next entry.
- **Star-schema and slowly-changing-dimension checks (DW-001/002/005/010/011,
  S2, RF-03 OLAP closure).** Five rules reading the schema **plus** the S1
  paradigm/role classification. They reach **only** fact- and dimension-role
  tables — an `oltp` or `unclassified` table is never evaluated — and all five
  are **pure surface, never affirmations**: a warehouse-modelling choice is a
  design judgment, not a structurally undeniable defect.
  - **DW-001, fact table with no dimension FK.** A fact-role table whose
    foreign keys reach no dimension-role table. A FK to another **fact**, to
    staging, or to an unclassified table deliberately does **not** count — a
    fact-to-fact bridge looks joined but carries no dimensional context. A fact
    with zero FKs fires too, and says so (`foreign_keys: (none)`).
  - **DW-002, dimension without a surrogate key.** A dimension-role table whose
    primary key is **composite**, or a single column that is **not provably an
    integer** surrogate. The test is structural and narrow on purpose — a
    one-column integer primary key, the shape every well-modelled warehouse
    uses — so no name guessing is involved. **Declared limit (a):** a UUID/GUID
    surrogate types as a *string* in the neutral model and therefore **fires**;
    the emitted facts (`composite_primary_key`, `integer_primary_key`,
    `primary_key_column_resolved`) are what the agent needs to dismiss it in
    one step. **Declared limit (b) — the incomplete-reconstruction path:** when
    the primary key names a **single column the parser did not reconstruct onto
    the table**, proving an integer surrogate would require reading that
    column's type, so the rule **fires** as not-provably-a-surrogate and names
    the reason instead of hiding it — `primary_key_column_resolved=false`, with
    `primary_key_type` reading `(column not declared in the parsed schema)`.
    Deliberate and test-locked, and the **opposite** choice from DB-051, which
    does **not** fire on an unresolvable reference: DB-051 compares two types
    and has nothing to compare, while DW-002 asks whether a surrogate is
    *proven*, and an unread column proves nothing. Reachable, not hypothetical:
    SQL-DDL known limit (5) drops a column whose name is an index keyword and
    whose type is outside the dialect's vocabulary (real Pagila's
    `film.fulltext`) while a table-level `PRIMARY KEY (fulltext)` naming it
    survives into the model. A dimension with **no** primary key at all
    **abstains** — DB-050 already affirms that case, and two IDs for one defect
    is noise.
  - **DW-005, facts present but no time dimension.** Schema-level, at most
    **one** item, anchored on the first fact table. A time dimension is
    recognized by **either** the conventional name (`dim_date`/`dim_time`/
    `dim_calendar`, separator-insensitive) **or** the structural grain — a
    dimension whose primary key is a single date/datetime column. The
    structural signal keys on the **primary key**, not on "contains a date
    column": an `updated_at` stamp is not a calendar, and accepting any date
    column would suppress the rule on almost every schema (a silent false
    negative). **Declared limit:** a calendar keyed by an integer `yyyymmdd`
    smart key — AdventureWorksDW's own `DimDate` — is recognized only by
    **name**, since that key is structurally indistinguishable from any other
    surrogate.
  - **DW-010, SCD-2 dimension without a currency index.** A dimension carrying
    slowly-changing columns (`valid_from`/`valid_to`/`is_current`/
    `effective_date`, separator-insensitive so `validTo`/`isCurrent` match too)
    where **no index leads with** `valid_to` or `is_current` — so every "give
    me the current version" query scans the whole version history, the part
    that grows without bound. Coverage is delegated to the **same** shared
    helpers DB-001 and DB-010 use (`db.IndexLike` / `db.CoveredByOrderedPrefix`),
    so "what serves a lookup" is defined once: the primary key counts as an
    implicit index, and a composite index **not leading** on the currency
    column does **not** cover it. Either currency column being covered is
    enough to go quiet. A dimension with no slowly-changing columns is never
    evaluated; one with history columns but **neither** currency column
    abstains rather than demanding an index no query would use.
  - **DW-011, mixed SCD strategies.** Schema-level, one item, when some
    dimensions keep history and others overwrite in place — a report joining
    both mixes point-in-time with as-of-today attributes. **Time dimensions are
    excluded** from the comparison (a calendar is not slowly-changing by
    definition); counting one as SCD-1 would fire this rule on essentially
    every correctly built warehouse. A genuine split between two other
    dimensions still fires. DW-010 and DW-011 share one history vocabulary, and
    its **declared limit**: a dimension using a different vocabulary
    (AdventureWorksDW's `DimProduct` uses `StartDate`/`EndDate`/`Status`) reads
    as SCD-1.
  - **Dogfood status — stated plainly, not implied.** The positive and trap
    fire paths of all five rules are proven by **constructed** (declared
    synthetic, ADR 0028) schemas, **not** by real vendored DDL. Microsoft's
    AdventureWorksDW **is** vendored
    (`testdata/tsql/adventureworksdw_real_objects.sql`, MIT) and yields **no**
    DW finding, for two independent, test-locked reasons: its PascalCase
    Kimball names (`FactInternetSales`, `DimCustomer`) fall outside the
    recognized snake_case prefix vocabulary, and the T-SQL reducer drops the
    `ALTER TABLE ... ADD CONSTRAINT` shapes it uses, so its real primary and
    foreign keys never reach the model at all (see SQL-DDL known limits (6)).
  - **Zero value on Prisma-only projects.** A `schema.prisma` expresses no
    warehouse concept, so these rules can only classify a Prisma project whose
    models happen to be named `fact_`/`dim_`.

### Not covered (declared, not silent)

- Race conditions in business logic.
- Architectural design flaws.
- Business-logic correctness (not a security property).
- Deep static taint analysis — covered by surface mapping + agent reasoning, not
  deterministically.
- **JS server frameworks beyond Next.js, Express, Fastify, and NestJS** — **not yet
  covered**, a known gap, not a silent one.
- **Index-vs-query analysis** — whether the schema indexes the columns the code
  actually filters on — **is now covered** by DB-010 / DB-013 (see the *Index-vs-query*
  section above) for **Prisma** projects, in `scan-all`. What remains uncovered is
  declared there: range-vs-equality (no WHERE operator captured), a `String` used as
  an enum, cross-naming-space against a physical SQL-DDL schema, and cross-table
  (join) filters. Whether an existing index is *actually used* at runtime is a
  different, telemetry-only question — see DB-012 below. (**N+1 query-in-loop
  patterns** are a separate capability — mapped as per-handler surface in `scan-all`'s
  endpoint buckets, never in this DB section; see the N+1 entry above.)
- **Never-used index (DB-012)** is **not** covered, and this is **permanent**,
  not deferred: detecting an unused index requires runtime query telemetry
  (e.g. PostgreSQL's `pg_stat_user_indexes`) that only exists inside a live,
  running database with real traffic history. codefit's model is static and
  never connects to a database — it reads only DDL/schema text — so this rule
  is structurally incompatible with how codefit operates, not merely
  unscheduled.
- **The routine-body rule family is now COMPLETE.** DB-030 (dynamic SQL
  construction), DB-031 (routine without exception handling), DB-040 (trigger
  cross-table cascade), and DB-041 (trigger external-effecting call) are **all
  covered** as surface (see above), **none deferred**. The parser prerequisite
  was **done** — a multi-statement T-SQL routine body is captured **complete** to
  the `GO` batch separator (or EOF), **not** truncated at its first internal `;`
  (ADR 0027; PostgreSQL dollar-quoted and MySQL `DELIMITER`-wrapped bodies were
  already complete) — which is what let these rules read whole T-SQL bodies
  safely. (Had DB-031 shipped over the old truncated T-SQL body, "is exception
  handling present?" would have **falsely affirmed** an absence that was really
  just unread text past the cut — which is why the parser fix came first.) Each
  rule still **gates on `Body.Complete`** so a body the parser could not prove
  whole is never evaluated. The whole family carries the same Prisma-zero-value
  limit as DB-020: Prisma's `schema.prisma` has no stored-procedure/trigger
  block concept, so a Prisma-only project gets no value from any of these rules.
- **OLAP / data-warehouse rule family (DW-0xx), narrowed again as of S2.**
  Paradigm/table-role detection, 3NF-suppression on OLAP-classified tables,
  **and** the star-schema/SCD checks — DW-001, DW-002, DW-005, DW-010, DW-011 —
  **are now covered** (see above). What **remains** not covered: a
  columnar/analytic-index check (**DW-021**, planned for S3) and a partitioning
  check (**DW-020**, planned for S4). Neither fires today, under any dialect.
  Materialized-view refresh staleness (a DW-022 candidate) was evaluated and
  **permanently dropped**, same DB-012 lineage as never-used-index: refresh
  cadence lives in external cron/scheduler state absent from static DDL.
  Separately from the rules: table-role detection recognizes only the
  snake_case prefixes `fact_`/`dim_`/`stg_`/`mart_`, so a warehouse using
  **PascalCase Kimball naming** (`FactInternetSales`, `DimCustomer` — as
  Microsoft's own AdventureWorksDW does) classifies entirely as `unclassified`
  and gets **no value** from any DW rule. That is a naming-vocabulary limit,
  not a rule gap, and it is locked as a test against the vendored
  AdventureWorksDW DDL rather than left silent.
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
  fixed**. This is a **fabrication, not a silent drop**, so `db.Table.Complete`
  (ADR 0034) cannot catch it — the reducer believes it succeeded; the
  completeness contract's own doc comment states this boundary explicitly
  ("`Complete` covers DROPS, not FABRICATIONS") rather than over-promising a
  guarantee this mechanism does not provide. (6) **Three shapes of
  `ALTER TABLE ... ADD CONSTRAINT` are dropped** by the reducer, so the keys
  they declare never reach the model: T-SQL's
  `WITH CHECK` / `WITH NOCHECK` prefix (`ALTER TABLE x WITH CHECK ADD
  CONSTRAINT ...`); any whitespace other than a single space between `ADD` and
  `CONSTRAINT` (a newline, as formatted DDL commonly writes it); and
  comma-chained constraint lists (`ADD CONSTRAINT a ..., CONSTRAINT b ...`),
  where every constraint after the first is lost because it does not repeat
  `ADD`. Confirmed against real vendored AdventureWorksDW DDL, which uses **all
  three** — its three real primary keys and all eight real foreign keys are
  invisible. These three **shapes are still not parsed** (deliberately deferred
  parser-shape debt, unchanged by `db-model-completeness-contract`) — but the
  drop is **no longer silent**: each now marks the table's structural
  completeness (`db.Table.Complete=false`, ADR 0034) and DB-050 **routes** the
  table to a `db-table-structure-unproven` surface item (the raw unreduced
  statement plus `file:line`) instead of affirming. Real vendored
  AdventureWorksDW now yields **zero** DB-050 false affirmations and exactly 3
  routed surface items, locked in
  `internal/providers/sqlddl/dw_integration_test.go`
  (`TestDB050_AdventureWorksDW_NoFalseAffirmation_RoutesToSurfaceInstead`).
  Separately, the same investigation found and **closed** a related but
  distinct defect: a non-single-space `ADD`/`CONSTRAINT` (e.g. "`ADD  CONSTRAINT`",
  two spaces) used to hit the generic "`ADD `" column branch and **fabricate** a
  phantom column/key literally named "`CONSTRAINT`" rather than dropping
  cleanly — this fabrication path is now recognized at its source and converted
  into a recorded drop exactly like the three shapes above; it does not gain new
  parsing support, it stops inventing data. Locked in
  `internal/providers/sqlddl/fabrication_test.go`. (7) **Several `CREATE
  INDEX`-family statement shapes are not parsed** by `reCreateIndex` either, so
  the indexes they declare never reach the model: an anonymous PostgreSQL index
  (`CREATE INDEX ON t (...)`, no index name); PostgreSQL's `ON ONLY` clause on a
  partitioned parent table's own index; and the standalone `CREATE [UNIQUE]
  [CLUSTERED|NONCLUSTERED] [COLUMNSTORE|FULLTEXT|SPATIAL|[PRIMARY] XML] INDEX`
  forms — T-SQL's **ordinary** `CLUSTERED`/`NONCLUSTERED` index statement chief
  among them, which is **not** an exotic shape, it is T-SQL's everyday
  standalone index syntax. These shapes are **still not parsed** (deliberately
  deferred parser-shape debt, `parser-records-unrecognized-drops`; teaching
  `reCreateIndex` to actually read them is a named follow-up, not implemented
  here) — but, exactly like (6) above, the drop is **no longer silent**:
  `apply()`'s `default:` branch recognizes the `CREATE INDEX`-shaped head and
  marks the table's structural completeness (`db.Table.Complete=false`, ADR
  0034), attributing the drop to its named table when the statement's `ON` (or
  `ON ONLY`) clause resolves one, or to `Schema.Unreduced` when it does not (a
  wrong attribution is worse than none). On an ordinary SQL Server schema this
  means **every** table carrying a plain `CLUSTERED`/`NONCLUSTERED` index
  becomes `Complete=false` and DB-050 **routes** it instead of affirming — the
  honest cost of this fix, stated plainly rather than softened, and it
  amplifies schema-wide through `paradigm.Classification.Unprovable` (one
  incomplete table anywhere marks every **other** recognized-prefix
  `fact_`/`dim_`/`stg_`/`mart_` demotion in the same schema unprovable too, not
  just the demoted table itself). Locked in
  `internal/providers/sqlddl/unrecognized_index_form_test.go` and
  `internal/providers/sqlddl/tsql_ordinary_index_completeness_test.go`.
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
