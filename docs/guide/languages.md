# Languages, dimensions and what is measured

> Which languages codefit audits today, how far each dimension reaches, and the
> numbers behind those claims — measured against real projects, not asserted.
> The per-rule limits live in [COVERAGE.md](../../COVERAGE.md), which mirrors the
> manifest the agent itself reads.

### What runs today, before you install

Reach is **per dimension AND per language**, and this is the first thing to know —
not a footnote. Two of the five dimensions codefit is scoped to audit ship today:

| dimension | what runs on `main` |
|---|---|
| **Security** (rules + surface mapping) | **TypeScript** — 4 of 4 surface categories · **Go** — 6 rules, 1 of 4 categories (`authz`) · **any other language: refused with an error**, never a false all-clear |
| **Database** (schema structure) | **any language** — the schema parser resolves by file shape (`.prisma` / `.sql`), never by your project's language |
| Complexity · Regression risk · Deep-review quality | **not built** — no sensor exists; they are scope, not capability |

So: a Python or Java backend gets the **database** dimension audited if it declares
`database.schema_paths`, and `by_dimension.security` comes back honestly as `null` —
not silently skipped. A TypeScript backend gets both. Every response states its own
reach rather than leaving a count to be misread as complete.

Full breakdown: [Status](#status--phase-2-the-database-dimension-olap-closed) ·
[Supported languages](#supported-languages) · [COVERAGE.md](../../COVERAGE.md).


## Status — Phase 2 (the database dimension), OLAP closed

**Phase 2 is the last phase that CLOSED. Phase 3 has started, and part of it is
usable today**: the finding cache and `changed_files` scoping (H0), and the closing
protocol (H4) — the agent records what it reasoned, `scan-all` hands it back, and a
confirmed still-present problem counts in the score. The rest of Phase 3 — the
practices, tests/regression and deep-review dimensions — is **not built**, and the
phase does not close until `codefit-review-code` exists. See
[docs/roadmap.md](../roadmap.md).

**Reach is per dimension AND per language — read this before you install.**
Security detection and surface mapping (IDOR, broken authorization, over-fetching,
N+1) run on **TypeScript** (all four categories) and on **Go** (broken authorization
only — 6 declared security rules, 1 of 4 surface categories); `codefit-scan-security`,
`codefit-scan-endpoint`, and `codefit-coverage` refuse any other language with a clear
error rather than a false all-clear. Every response from an exposed language states its
own reach: `codefit-coverage {language}` and `codefit-scan-all`'s `security` section
both carry a `surface_coverage` statement naming exactly which categories were mapped
and which were not — Go's coverage says so explicitly rather than leaving a bare
`surface_items` count to be misread as complete. The **database dimension does not
depend on your app's language**: `codefit-scan-db` (and `codefit-scan-all`) resolves
its schema parser by the schema file's shape (`.prisma` / `.sql`), never by the
project's language — so a Python backend that declares `database.schema_paths` still
gets the DB dimension audited, with `by_dimension.security` reported honestly as `null`
rather than silently skipped. See [Supported languages](#supported-languages) for
the full per-language, per-dimension breakdown.

**Works today, on `main`, validated in real use against real backends:**

- **Providers:** TypeScript (gotreesitter, pure Go) and Go (`go/ast`) — both exposed
  to `codefit-scan-security`/`-scan-endpoint`/`-scan-all`, with Go's narrower reach
  (6 security rules, 1 of 4 surface categories) stated in its own coverage answer,
  not implied as parity.
- **Deterministic security rules (TypeScript)** — five categories, each a fact at
  certainty 1.0 (see [COVERAGE.md](../../COVERAGE.md)).
- **Surface mapping** — four categories (IDOR, broken authorization,
  over-fetching, N+1) for Next.js (App Router + Server Actions), **Express, Fastify, and
  NestJS**, enumerated completely for the agent to reason. Resource access in another
  file is signalled (`indirect_access`), not followed across files.
- **`scan-all` three-bucket synthesis** + on-demand `scan-endpoint` detail.
- **Baseline** — a committed, content-addressed memory of the audited surface, with
  `baseline-list` / `-accept` / `-prune`, so a re-scan only surfaces what changed.
- **Scoping a scan to the files you changed** — an optional `changed_files` on
  `codefit-scan-all` / `codefit-scan-security`, with the narrowing declared in the
  response so a partial result never reads as a full one (see
  [Scoping a scan](tools.md#scoping-a-scan-to-the-files-you-changed) below). Exercised over four
  real TypeScript projects by a committed dogfood harness — the narrowing kept its whole-project
  denominator, left nothing unmatched, and reached the same verdict on those files as a full
  pass — though through the security sensor rather than the MCP handler; see
  [What has actually been measured](#what-has-actually-been-measured).
- **An opt-in content-hash finding cache** — the same codefit binary, over the same file
  bytes at the same path, reuses the analysis instead of recomputing it, so a recurring full
  scan costs about what an incremental one does. Off unless a project enables it, and a warm
  scan is byte-identical to a cold one (see [The finding cache](tools.md#the-finding-cache-opt-in)
  below). Exercised over the same four real projects: warm scans re-analysed **0 files** and
  came out byte-identical to cold. The timings that came with it are a measurement on one
  machine, not a speed codefit promises — read them, with their limits, under
  [What has actually been measured](#what-has-actually-been-measured).
- **`codefit init`** — detects the stack, writes `.codefit.yaml`, and installs
  codefit's own thin skill for each detected agent. It **never refuses over
  language**: on a root where no marker file (`go.mod`, `package.json`,
  `tsconfig.json`) resolves a provider it still writes both artifacts and
  declares the gap — which markers it looked for, that no code is scanned here,
  and that the DB dimension still audits the schema once `database.schema_paths`
  is set. It writes a `database:` section **only when it can PROVE one** —
  `schema_paths` is the one DB field codefit's sensors actually read, so it is
  the one that decides. Schema detection reads a Prisma `schema.prisma` **and**
  SQL migration directories, independently of the project's language (a Java or
  Go service with Flyway migrations is found too), but a directory is written
  live only when its apply order is provable from the filenames, the **real**
  SQL-DDL parser reconstructs at least one table from it, and it is the only
  directory that proved. Anything else — golang-migrate naming codefit cannot
  order, DDL that reconstructs nothing, two candidates it cannot choose between —
  gets the `database:` block **commented, naming the real path and the reason**,
  because a wrong `schema_paths` does not merely audit less: entries merge into
  one reconstructed model, so it audits a schema you do not have. When codefit
  finds nothing at all it says it looked and how deep it walked, and marks its
  example as an example. codefit does **not** sniff the SQL dialect: a proven
  block carries a commented `type:` line and the report names the dialect the
  proof ran under, because the proof says the DDL reconstructs — not that the
  dialect is right. A detected ORM does **not** buy a `database:` section on its
  own: no sensor reads `database.orm`, and a block holding only an ORM name would
  look configured while configuring nothing.
- **MCP stdio server** (official MCP Go SDK), single static binary, `CGO_ENABLED=0`.
- **Database-dimension auditing** — a neutral schema model with two schema parsers,
  **Prisma** (`schema.prisma`) and **SQL-DDL** (Flyway migrations, reconstructed
  incrementally), the latter supporting **PostgreSQL, MySQL, and SQL Server (T-SQL)**
  dialects selected by `database.type` in `.codefit.yaml` (`postgresql` | `mysql` |
  `sqlserver`; `sqlite` returns an explicit "not supported yet" note). It audits, all
  dialect-agnostic over the neutral schema: the schema's **structural quality** (keys,
  indexes, column shape, name-heuristic smells), **view** sensitive-column exposure,
  the **routine bodies** of stored procedures, functions, and triggers (dynamic SQL,
  missing exception handling, cross-table cascades, external-effecting calls), and
  **code×schema crosses** that check whether the columns the code actually filters on
  are backed by an index. It also **detects the schema's paradigm** (OLTP vs OLAP,
  overridable by `database.paradigm`) so an OLAP schema's intentional denormalization
  is not flagged as a normalization violation — the whole **schema** is judged before
  any table is called a fact or a dimension, so one `dim_`-named table cannot silence
  its own normalization findings inside an otherwise transactional schema — and on a
  schema it classifies as a
  warehouse it audits the **star-schema and slowly-changing-dimension shape** (a fact
  joining no dimension, a business key where a surrogate belongs, facts with no time
  dimension, an SCD-2 currency lookup no index serves, SCD-1 and SCD-2 mixed), a
  **fact table with no columnar/analytic index**, and a **census of its fact tables for
  declared table partitioning** — seven DW rules in all, with no OLAP rule left unbuilt.
  One structural fact is
  affirmed (a table with no primary key); everything else is surface the agent reasons.
  Run it standalone with `codefit-scan-db`; it also runs inside `scan-all` as its own
  section with a per-dimension score (the code×schema cross runs in `scan-all` only).
  The rule inventory lives in [COVERAGE.md](../../COVERAGE.md).
  Dogfooded on real Prisma and SQL-DDL (Postgres/MySQL/T-SQL) backends.

**The Phase-2 acceptance criterion is met.** The PRD asks for `codefit-scan-db` to
produce *verified real findings on a real project*. Measured on `main` through the
real handler, over an untouched UTF-16LE `pg_dump` of a production Postgres backend:
`measured: true`, 9 tables, structure proven for 9 of 9, **12 surface items, 0
deterministic findings**, paradigm `oltp`. Every one of the 12 was hand-checked
against the DDL — **0 false positives** — and the run also holds a verified *true
negative*: the schema declares 11 foreign keys and the FK rule fires on 10, staying
correctly silent on the eleventh, whose column a `UNIQUE` constraint already covers.
Read the number honestly, though: that project exercises a **narrow slice** — 9
tables, no views, procedures or triggers, nothing analytic — so only **3 of the 21
DB/DW rules fired at all**; breadth is evidenced by the 26-corpus survey, not by this
one project. And its `score` is **100** *alongside* those 12 items, which is correct
by design (surface is a question for the agent, so it is never scored) but reads as
"clean" to anyone who looks at the score first.

**On the roadmap (not yet in `main`):** the HTTP/SSE transport;
**literal values in the query model** — carrying the WHERE's literals so the
cross can infer cardinality from usage (a `String` used as an enum, a `DateTime` used
as a flag) and tell an equality filter from a range, the two field-observed limits of
the index-vs-query cross; Phase 3 code review / best practices / tests; Phase 4
knowledge packs + `update`.
See the [PRD](../PRD-codefit-v1.4.md) §25 and [VERSIONING.md](../../VERSIONING.md).

## What codefit covers today

Concretely, on `main` — so you know exactly what to expect without reading
[COVERAGE.md](../../COVERAGE.md) in full:

- **Languages — reach is per dimension, not per project.** Deterministic security
  rules and surface mapping run on TypeScript / TSX only. Go's provider is used
  internally for codefit's own CI self-audit — not exposed to a Go user's
  project. The **database dimension bullet below is language-independent**: it
  runs for any project (Go, Python, anything) that declares
  `database.schema_paths`, because its schema parser resolves by the schema
  file's shape, not by the app's language.
- **Deterministic rules (TypeScript, certainty 1.0).** Hardcoded secrets, weak
  crypto (MD5/SHA-1, insecure `Math.random` for tokens), dangerous
  `eval`/`new Function`, inline SQL injection, and inline XSS via
  `dangerouslySetInnerHTML` (React-specific by its pattern). These run on **any**
  `.ts`/`.tsx` file — no framework gate; which ones fire depends on the code, not the
  framework.
- **Surface mapping — the agent reasons.** IDOR, broken authorization,
  over-fetching, and the **N+1 query-in-loop** pattern (DB-201), for **Next.js** (App
  Router route handlers + Server Actions), **Express**, **Fastify**, and **NestJS**.
  Handlers are found by structural shape,
  never by path or name. When a handler reaches a resource through a service in
  another file, codefit flags it (`indirect_access`) and names the call — it does not
  follow it across files; the agent does. N+1 items are **ordered, never filtered**: a
  loop over a literal 3-element array is enumerated exactly like one over an unbounded
  query result, with the iterated source named as a fact so the agent dismisses it at a
  glance.
- **Dependency CVEs.** `codefit-check-cves` checks dependencies against
  [OSV.dev](https://osv.dev) (free, no API key) using the **exact** versions from
  `package-lock.json` / `go.mod` — never guessed from `package.json` ranges (no
  lockfile → an honest note, not a guess). codefit keeps no vuln DB of its own and
  surfaces OSV's severity rather than recomputing CVSS.
- **Database dimension — the agent reasons the surface.** Read from
  `database.schema_paths` (a Prisma `schema.prisma` or a directory of SQL-DDL / Flyway
  migrations reconstructed to the final schema). SQL-DDL parsing supports **three
  dialects**, selected by `database.type` in `.codefit.yaml`:

  ```yaml
  database:
    type: postgresql # postgresql | mysql | sqlserver (sqlite: not supported yet)
    paradigm: auto   # auto (default) | oltp | olap | mixed — codefit detects when unset; an explicit value wins
    schema_paths:
      - db/migrations
  ```

  codefit audits the schema's **structural quality** (keys, indexes, column shape, and
  name-heuristic smells), **view** sensitive-column exposure, the **routine bodies** of
  stored procedures, functions, and triggers (dynamic SQL, missing exception handling,
  cross-table cascades, external-effecting calls), and — in `scan-all` — **crosses the
  code's Prisma queries against the schema** to see whether the columns the code filters
  on are backed by an index. One structural fact is affirmed (a table with no primary
  key); everything else is surface the agent judges, all dialect-agnostic over the
  reconstructed neutral schema. codefit also **detects the schema's paradigm** (OLTP vs
  OLAP — `database.paradigm` overrides it) and does **not**
  flag an OLAP schema's intentional denormalization as a normalization violation. The
  question is asked of the **schema** before it is asked of any table: a schema counts as
  a warehouse only on schema-wide evidence (a declared calendar table, the `_sk`
  surrogate-key convention, or a numeric/text split across its column types), and inside
  a schema that shows none of those, no table gets a warehouse role at all — so one
  `dim_`-named table cannot decide its own silencing. When roles are withheld, the scan's
  note says so and names `database.paradigm: olap` as the escape hatch. On a
  schema it classifies as a warehouse it additionally audits the **star-schema and
  slowly-changing-dimension shape**: a fact table joining no dimension, a dimension keyed
  by a business key instead of a surrogate, facts with no time dimension, an SCD-2
  dimension whose "current version" lookup no index serves, and SCD-1 and SCD-2
  dimensions mixed in one schema — plus a **fact table with no columnar/analytic index**
  (a `brin` or `columnstore` method the parser read from the DDL; a fact table with only
  ordinary row-store indexes fires) and a **census of its fact tables for declared table
  partitioning** (one item for the whole schema, never one per table, naming which fact
  tables declare partitioning and which do not; declared partition children are excluded,
  since a partition is not a fact table). All seven are surface, never affirmations —
  whether a fact table is large enough to need partitioning or a columnar index is a
  runtime fact codefit cannot see, so it hands the agent the shape and the question.
  Table roles are recognized from the table **name**,
  **case-insensitively**, in three spellings: an underscore-delimited leading segment
  (`fact_`/`fct_`/`f_`, `dim_`/`d_`, `stg_`, `mart_`), an underscore-delimited trailing
  segment (`_fact`/`_facts`, `_dim`/`_dims`), or separator-free **PascalCase**
  (`FactInternetSales`, `DimCustomer`). A recognized name is only ever a *candidate* —
  real relational structure must corroborate it before a table is promoted. Declared
  limit: an **all-caps** name (`FACTORY_SETTINGS`) stays unclassified by design, as does
  a name with neither a delimiter nor a PascalCase boundary. See
  [COVERAGE.md](../../COVERAGE.md) for the full rule inventory and the declared SQL-DDL
  dialect limits.
- **Not covered (declared, not silent).** JS server frameworks beyond
  Next.js/Express/Fastify/NestJS; deep taint analysis; business-logic correctness;
  architectural and race-condition classes. An Express/Fastify handler passed by
  reference (not inline) is not enumerated. Full list and limits in
  [COVERAGE.md](../../COVERAGE.md).

## What has actually been measured

The scope and the cache are exercised over **real projects**, not fixtures alone, by a
harness that lives in the repository (`internal/mcp/dogfood_cache_test.go`). It is behind
the `dogfood` build tag, so the ordinary `go test ./...` never compiles it, and it **skips
clean** when `dogfood.local.json` — a **per-machine, gitignored** file listing the absolute
paths of your own clones — is absent. Whoever has the clones measures; whoever does not
breaks nothing. Reproducing the numbers below therefore means using **your** projects, not
these.

It runs the real security sensor cold and then warm over each project and requires the two
results to be **byte-identical**, and it drives `changed_files` over the same projects with
the paths spelled the way an agent actually hands over a git diff on Windows. It is
**read-only** over the clones: the configuration is synthesized in memory and the cache lives
in a temporary directory, which is why it drives the sensor rather than the MCP handler —
the handler reads `.codefit.yaml` from the project root, and turning the cache on through it
would mean writing inside somebody's working tree.

**One Windows machine, four projects, 2026-08-03:**

| project | files audited | findings | surface | cold | warm |
|---|---|---|---|---|---|
| salonpro | 317 | 1 | 386 | 5989 ms | 514 ms |
| bitacoras | 147 | 0 | 102 | 2473 ms | 168 ms |
| plantalinda | 309 | 0 | 0 | 5023 ms | 265 ms |
| metricasbatch | 14 | 0 | 0 | 465 ms | 11 ms |

The warm runs re-analysed **0 files** and matched the cold runs byte for byte on all four.
Read the milliseconds as what they are:

- **The cold column is not stable.** Repeats varied by roughly **±2x** — salonpro ran
  anywhere from 5989 to 11627 ms — because the operating system caches the filesystem
  underneath. The warm column was stable. Any ratio you compute from this table describes
  this machine on this day, not a speed codefit offers you.
- **Two of the four projects produced zero findings and zero surface,** and that is checked,
  not glossed over: metricasbatch is a Vite React SPA with no route handlers, and
  plantalinda's only Next.js route handler returns a static `new Response("ok")` — codefit's
  [declared frontier](../../README.md#what-it-affirms-and-what-it-asks) is correct to say nothing about
  either. They still exercise the walk, the store and the warm hit over hundreds of real
  files, and the harness requires *some* project in the corpus to carry findings and *some*
  project to carry surface, so an empty corpus cannot pass. But half of this corpus exercises
  almost nothing, and the table should be read knowing that.
- **Four projects one person happened to have clones of are not a representative sample**,
  and this is not a benchmark. It is evidence that both features survive real code.


## Supported languages

| Language / Ecosystem | Status |
| --- | --- |
| **Go** | `codefit-scan-security`, `codefit-scan-endpoint`, and `codefit-scan-all` audit Go code: **6** declared deterministic security rules (hardcoded secrets, SQL injection by string concatenation, OS command injection by string concatenation, an env var read without a default, insecure `math/rand` fed into a security value, weak hashing with MD5/SHA-1) and **1 of 4** surface categories mapped (`authz` — HTTP handlers; IDOR, over-fetching, and N+1 are not mapped for Go). Every `codefit-coverage`/scan response for Go states this reach explicitly (a `surface_coverage` field naming what was mapped and what was not), never a bare pass/fail. `codefit-scan-all` also audits the DB dimension (schema-only) when `database.schema_paths` is configured. |
| **TypeScript / Next.js / Express / Fastify / NestJS / Prisma** | Deterministic security rules (5 categories, any TS file) + surface mapping (IDOR, authz, over-fetching, N+1) for Next.js App Router, Server Actions, Express, Fastify, and NestJS. Cross-file resource access is signalled (`indirect_access`), not followed. Validated against real Next.js, Express/Prisma, and NestJS/Prisma backends. |
| Java / Spring | Roadmap |
| Python / FastAPI / Django | Roadmap |

Adding a language means implementing one `LanguageProvider` — it never touches the
core, the sensors, the MCP server, or the reporting (see
[CONTRIBUTING.md](../../CONTRIBUTING.md) and `docs/decisions/`).

