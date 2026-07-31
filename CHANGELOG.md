# Changelog

All notable changes to codefit are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> **`v0.1.0` — Phase 1 complete.** codefit is usable end-to-end from `main`: the MCP
> stdio server, deterministic TypeScript security rules, surface mapping (IDOR /
> broken authz / over-fetching), the `scan-all` three-bucket synthesis, `codefit init`
> (config + self-discovering skill), and the **baseline** (a committed audit memory
> with list / accept / prune). Validated in real use against a Next.js/Prisma backend.
> Ahead: the HTTP/SSE transport and Phases 2–4 (DB, code review, knowledge packs) —
> see [VERSIONING.md](VERSIONING.md) and the [PRD](docs/PRD-codefit-v1.4.md) §25.

## [Unreleased]

### Changed

- **Table-role detection recognizes the naming real warehouses actually use.** An empirical
  yield measurement over 22 real public corpora found the DW-0xx family measuring near-zero
  because of its **name vocabulary**, not its rule logic: it matched four snake_case prefixes
  (`fact_`/`dim_`/`stg_`/`mart_`) case-**sensitively**, and 4 of 9 real warehouse corpora used
  exactly that Kimball convention, only capitalized. Role names are now matched
  case-insensitively in three spellings — an underscore-delimited **leading** segment
  (adding `fct_`, `f_`, `d_`), an underscore-delimited **trailing** segment (`_fact`/`_facts`,
  `_dim`/`_dims`, for TPC-DS's `date_dim`), and separator-free **PascalCase** (`FactInternetSales`,
  `DimCustomer`). Every entry is evidence-backed by that measurement, never speculative.
  **Structure still decides:** ADR 0033's corroboration gate is untouched — a fact candidate
  still needs FK fan-out ≥ 2 and a dimension candidate fan-in ≥ 1, so a wider name never
  substitutes for structure. Declared limits kept on purpose: an **all-caps** name
  (`FACTORY_SETTINGS`) and a name with neither a delimiter nor a PascalCase boundary stay
  unclassified, because a false promotion would silence that table's DB-002/DB-003 1NF findings.
- **The vendored AdventureWorksDW corpus is now recognized by name.** Microsoft's real DDL
  previously matched nothing; its three tables are now recognized candidates, locked against
  the **real parsed corpus**. It still yields no DW finding, but for **one** remaining reason
  instead of two: the T-SQL reducer still drops the `ALTER TABLE ... ADD CONSTRAINT` shapes it
  uses, so its real keys never reach the model and the corroboration gate has nothing to work
  with. That limit is unchanged and still declared.

## [0.2.5-alpha.2] — 2026-07-31

**Still on the way to Phase 2.5 — this release is a correctness fix, not new coverage.**
codefit's database rules stop concluding from what its parser could not read. The two
remaining OLAP items (DW-021 columnar index, DW-020 partitioning) are **still open**, so
this remains an alpha and does not claim `0.2.5`.

### Fixed

- **codefit no longer invents database findings it cannot prove.** `DB-050` was emitting
  `AFFIRMED (confidence 1.0): table has no primary key` over real vendored AdventureWorksDW
  DDL that plainly declares all three keys. The SQL-DDL reducer discarded `ALTER TABLE`
  shapes outside its declared subset **silently**, and the rule read that silence as
  evidence. For an auditor, inventing a problem costs more trust than missing one — and
  `1.0` is the strongest claim codefit can make.
- **Four surface categories were emitted without being declared** since `v0.2.3`:
  `db-routine-no-exception-handling`, `db-trigger-cross-table-cascade`,
  `db-trigger-external-call` and `db-dynamic-sql-in-routine`. Per ADR 0019 an
  emitted-but-undeclared category can never be baselined or pruned, which is the one way to
  corrupt a committed `.codefit-baseline`. Found by the new coverage-manifest enforcement
  test on its first run.

### Added

- **A per-table structural completeness contract on the neutral model** (ADR **0034**),
  generalising the existing `db.Body{Complete,Note}` precedent (ADR 0025) from routine
  bodies to the table's structural set. `db.Table` now carries `Complete`, `Note` and the
  raw `Unreduced` statements, and the reducer records a drop instead of swallowing it.
  Written as neutral-model doctrine; implemented for the DB dimension only.
- **The eight absence-based DB/DW rules now gate on completeness.** Seven abstain;
  `DB-050` — the dimension's only affirmation — **routes to a new
  `db-table-structure-unproven` surface item** carrying the raw statement and its
  `file:line`, so the agent reads the DDL itself rather than losing the signal entirely.
  `DW-005`/`DW-011` abstain as a whole rule, since a per-table skip would shrink a census
  and still emit.
- **A per-scan incompleteness inventory** on the DB result note, aggregated by reason and
  capped, so a systematic parser gap across 200 tables is one bounded line rather than 200
  items. The measured `scan-all` path now carries that note (it was being dropped).
- **Tests for `internal/core/dbcoverage/`**, which previously had none — including a
  correspondence check that fails when a registered rule has no manifest entry. That is the
  control that caught the undeclared-category defect above.
- **Executable debt locks** for TypeScript's unconsulted `HasError()` and Go's fail-loud
  parse behaviour: both assert today's actual behaviour, so the limits are machine-visible
  rather than prose.

### Notes

- The Prisma parser's model-body skip now marks its table unproven, so completeness is not
  affirmed on the most-used path without evidence.
- `db.Table.Complete` covers **drops**, not **fabrications** — a reducer bug that produces a
  wrong value rather than none is a separate class, documented in ADR 0034 §2.6 with its own
  characterisation test.

## [0.2.5-alpha.1] — 2026-07-30

**On the way to Phase 2.5 (RF-03 OLAP closure) — the OLAP rules that read the neutral
model as it already is.** codefit learns to tell a data warehouse from a transactional
database and audits its modelling. Two of the eight items in the PRD's OLAP scope
(§RF-03, lines 377-382) remain, so this is an **alpha**: it does not claim `0.2.5`.

### Added

- **Paradigm and table-role detection** (`internal/core/paradigm/`, a pure core leaf).
  A schema classifies `oltp` / `olap` / `mixed`, and each table `fact` / `dimension` /
  `staging` / `mart` / `unclassified`, as a pure function of the neutral `db.Schema`.
  Name prefixes (`fact_` / `dim_` / `stg_` / `mart_`) are the primary signal,
  **corroborated by real relational structure** — a fact needs FK fan-out to 2+ tables,
  a dimension needs fan-in ≥ 1. A lone surrogate primary key is deliberately **not**
  corroboration: that was caught in review, where it was silently classifying ordinary
  OLTP tables as warehouse-role.
- **`database.paradigm` config, default `auto`.** Detection **seeds** the classification;
  an explicit `oltp` / `olap` / `mixed` **overrides** it entirely. The developer always
  has the last word (ADR 0033).
- **3NF-suppression on OLAP tables.** DB-002 (multivalued attributes) and DB-003
  (repeating groups) no longer fire as normalization violations on fact/dimension/mart
  tables — intentional denormalization is the point of a warehouse — while firing
  unchanged on OLTP and unclassified tables. Every suppression leaves an audit trace in
  the sensor's `Note`, naming what was withheld and that `paradigm: oltp` reveals it.
- **Star-schema and slowly-changing-dimension rules** (`internal/core/dwrules/`), all
  **surface, never affirmations** (ADR 0017) — a modelling choice is a design judgment,
  not an undeniable defect, so codefit states the observed shape and the agent decides:
  - **DW-001** — a fact-role table whose foreign keys reach no dimension-role table. An
    FK to another fact, to staging, or to an unclassified table deliberately does not
    count. A fact with zero FKs fires and says `foreign_keys: (none)`.
  - **DW-002** — a dimension whose primary key is composite, or a single column not
    provably an integer surrogate. A dimension with **no** primary key abstains: DB-050
    already affirms that case.
  - **DW-005** — schema-level, one item: fact tables present with no time dimension.
    Recognized by conventional name (`dim_date`/`dim_time`/`dim_calendar`) **or** by
    grain (a primary key that is a single date/datetime column). Keyed on the primary
    key, not "contains a date column", so an `updated_at` stamp does not suppress it.
  - **DW-010** — a dimension carrying `valid_from`/`valid_to`/`is_current`/
    `effective_date` where no index leads with `valid_to` or `is_current`, so every
    "current version" query scans the whole history. Index coverage is delegated to the
    same shared helpers DB-001 and DB-010 use, so "what serves a lookup" is defined once.
  - **DW-011** — schema-level: some dimensions keep history while others overwrite in
    place. Time dimensions are excluded from the comparison — a calendar is not
    slowly-changing, and counting it would fire on nearly every correct warehouse.
- The DW categories are appended to the DB sensor's `OwnedCategories()`, so DW surface
  is baselineable and prunable like every other DB item (ADR 0019), and the family runs
  in both `codefit-scan-db` and the DB bucket of `codefit-scan-all`.

### Declared limits — stated, not hidden

- **Role detection is snake_case only.** PascalCase Kimball naming (Microsoft's
  `FactInternetSales`, `DimCustomer`) classifies as `unclassified`, so the DW family
  yields **no value** on it. Test-locked, not silent.
- **DW-002 fires on a UUID/GUID surrogate** — it types as a string in the neutral model.
  The emitted facts are what the agent needs to dismiss it in one step.
- **DW-002 also fires when the parser did not reconstruct the primary key's column**
  (`primary_key_column_resolved=false`) — not-provably-a-surrogate rather than assumed
  fine. Reachable through the known SQL-DDL limit where a column named after an index
  keyword with an unrecognized type is dropped while a table-level PK naming it survives.
- **DW-005 sees an integer `yyyymmdd` smart key only by name**; that key is structurally
  indistinguishable from any other surrogate.
- **DW-010 and DW-011 share one history vocabulary**, so a dimension using a different
  one (`StartDate`/`EndDate`/`Status`) reads as SCD-1.
- **Zero value on Prisma.** A `schema.prisma` expresses no warehouse concept.
- **Dogfood status.** All five rules' fire and trap paths are proven by constructed,
  declared-synthetic schemas (ADR 0028). Microsoft's AdventureWorksDW **is** vendored
  (MIT) and yields no DW finding, for two independent test-locked reasons: its PascalCase
  names, and a pre-existing T-SQL reducer gap.

### Known issues

- **A pre-existing T-SQL reducer gap became visible** while vendoring AdventureWorksDW:
  three shapes of `ALTER TABLE … ADD CONSTRAINT` are dropped, so that script's three real
  primary keys and all eight real foreign keys never reach the model. The worst
  consequence is that **DB-050 — a deterministic affirmation at certainty 1.0 — reports
  three tables as having no primary key over DDL that plainly declares one for each.**
  Not introduced here and not fixed here; documented in the coverage manifest and locked
  by tests written to go red once the reducer is fixed.

### Not yet covered (Phase 2.5 remainder)

- **DW-021** (fact table without a columnar/analytic index) and **DW-020** (fact table
  without partitioning). Both need the neutral model and the SQL-DDL reducer enriched
  first, not just a new rule. **DW-022** (materialized views without refresh) is
  permanently dropped — refresh cadence lives in scheduler state absent from static DDL.

### Fixed

- **A binary downloaded from a release reported the wrong version.** goreleaser's
  `{{ .Version }}` strips the tag's leading `v`, so the `v0.2.4` release shipped a binary
  reporting `0.2.4`. The ldflag now uses `{{ .Tag }}` and the binary reports the tag
  verbatim. Archive names keep the goreleaser convention, so existing download URLs are
  unaffected.
- **The coverage manifest denied capabilities codefit actually ships.** The manifest that
  `codefit-coverage` serves to agents claimed the DW family was "not yet built" (it was —
  the same file contradicted itself twice) and that index-vs-query analysis was "not
  covered" (DB-010/DB-013 shipped in `0.2.4`). Both corrected at the source, then
  mirrored. `CONTRIBUTING.md` likewise still said `scan-all` ran only security.

## [0.2.4] — 2026-07-24

**Phase 2.4 — index-vs-query.** codefit pays the index-vs-query DB debt deferred in
`0.2.0` (OLAP remains the open Phase-2 debt): the first rules that cross the code's
queries against the schema, not the schema alone. A query's `WHERE` columns and the schema's indexes are matched through
new neutral infrastructure, so the core never imports a provider. Both rules are
**surface** — a missing index may or may not matter, the agent judges. Prisma only,
in `scan-all`. ADRs 0029–0032.

**Signal, measured.** On umami (a real analytics schema with 77 `@@index`), of **94
query filters extracted, 7 surface** — the rest fall on columns that are already
indexed, and the cross stays silent. That is the honest signal/noise: it speaks only
where the schema left a filtered column uncovered. Calibrated across real schemas of
different conventions, with **zero undeniable false positives**:

| schema | models | DB-010 | DB-013 | what it validated |
|---|---|---|---|---|
| node-express-prisma (RealWorld) | 4 | 0 | 0 | stays silent on a well-keyed schema |
| umami | 18 | 2 | 5 | coverage READS existing indexes — a wide index is not a cover of a trailing column |
| a private production SaaS | ~40 | 4 | 17 | drove the four noise corrections |
| papermark | 77 | 4 | 53 | the unique-subset short-circuit, against real composite `@@id` keys |

Output scales with the schema (7 / 21 / 57 items on 18 / ~40 / 77 models) — proportional, not runaway.

### Added

- **DB-010 — filtered column without a covering index.** A single column the code
  filters on that no index, `@unique`, or primary key covers as a leading column.
  Emits **surface** (whether it matters depends on cardinality/size/write-load).
- **DB-013 — multi-column filter without a composite index.** A `WHERE a AND b` with
  no composite index whose leading columns are that **set** (order-insensitive —
  `[b,a]` covers `(a,b)`). The same set recurring across many models is **grouped**
  into one item naming every affected model — one architectural observation (e.g.
  tenant scoping + soft-delete), not *N* findings.
- **The cross infrastructure**: `internal/core/query` (the neutral `QueryFilter`),
  `providers.QueryExtractor` with a Prisma/TypeScript implementation (the `WHERE`
  columns of every query call-site), and `internal/core/crossrules` (a two-input rule
  family with an exact-match reconciliation floor that abstains rather than point at a
  column that is not there). Wired into `scan-all`; the merge is byte-identical when
  the rule set is empty, locked by a mutation-tested seam gate.

### Fixed

- **Cross noise correction** (from dogfooding, ADR 0032): a filter constraining a
  **unique key** (PK / `@unique` / `@@unique` subset) is a single-row lookup and no
  longer flags a missing index (killed the tenant-scoped `where: { id, salonId }`
  false positive); DB-010 **skips `Boolean`/`enum`** columns (low-cardinality by
  declared type); DB-013 **abstains on 4-or-more-column** filters (unknowable subset);
  and the repeated-pattern **grouping** above. Declared limits — each fixed by a test,
  not left latent — are range predicates, cross-naming-space, cross-table joins, and
  **a declared type that hides real cardinality** (a `String` used as an enum, a
  `DateTime` used as a flag: both observed in the dogfood, both resolved by the same
  future slice — literal values in the query model).

## [0.2.3] — 2026-07-20

**Phase 2.3 — routine-body rules.** codefit closes the last of the Phase-2 DB debt that
needs a routine's *body*, not just its signature: dynamic SQL construction, missing
exception handling, trigger cross-table cascades, and trigger external calls — across
PostgreSQL, MySQL, and SQL Server. The prerequisite was a parser fix (T-SQL body
de-truncation); the four rules all read the **complete** body as surface, gated on
completeness, never affirmations. ADRs 0027–0028.

### Added

- **DB-031 — routine without exception handling** — a stored procedure or function whose
  body has no exception-handling construct (T-SQL `BEGIN TRY`, MySQL `DECLARE ... HANDLER`,
  PL/pgSQL `EXCEPTION WHEN`). PG `RAISE EXCEPTION` (a throw, not a handler) is excluded — the
  trap, test-locked. Surface: the adequacy of a present handler is the agent's judgment.
- **DB-040 — trigger cross-table cascade** — a trigger whose body writes DML to a table
  other than the one it fires on. Resolves a PostgreSQL trigger to its function (ADR 0026);
  excludes the T-SQL `UPDATE(column)` function and the trigger's own event clause; a
  same-table write is not a cascade. Exposes a `documented_by_comment` fact.
- **DB-041 — trigger external-effecting call** — a trigger that reaches outside the database
  (T-SQL `xp_cmdshell`/`sp_OA*`/`sp_send_dbmail`/`OPENROWSET`; PostgreSQL `dblink`/`NOTIFY`/
  `COPY ... PROGRAM`; MySQL `sys_exec`/`sys_eval`). **Strict vocabulary:** a plain `EXECUTE`/
  `CALL` of an internal stored procedure is not external — the trap.
- **DB-030 — dynamic SQL construction** — a procedure or function that builds and runs SQL
  from a string at runtime (`sp_executesql`, `EXEC(<expr>)`, `EXECUTE format`/`quote_*`,
  `PREPARE ... FROM`). A static `EXEC` of a named procedure is not dynamic — the trap.
  Injectability is the agent's judgment (no taint analysis).
- **Real + constructed routine fixtures** — the vendored Sakila / Pagila / AdventureWorks DDL
  extended with real routines (byte-diffed, license-attributed), plus constructed
  positives/negatives where the real corpora are clean, each declared `SYNTHETIC` in its
  header.

### Changed

- **T-SQL routine bodies are de-truncated (ADR 0027)** — a multi-statement T-SQL routine body
  is captured complete to the `GO` batch separator (or EOF), not cut at its first internal
  `;`. Expressed as a per-dialect descriptor datum, scoped to routine heads, without
  reintroducing the `BEGIN`/`END` guard ADR 0022 removed. Closes ADR 0022 known-limit (a) — a
  `CREATE TABLE`-shaped fragment inside a routine body no longer surfaces as a phantom table.
  PostgreSQL and MySQL output stays byte-identical.
- **Coverage doctrine (ADR 0028)** — the coverage manifest gains a third state,
  *detectable-without-dogfood* (distinct from covered and not-covered; e.g. DB-041 on MySQL,
  where an external-calling trigger is non-idiomatic), plus a fixture gap policy (real →
  constructed-declared-synthetic → not-covered only for structural impossibility).

### Security

- **Go toolchain bumped to `go1.25.12`** (go.mod + all four workflow pins) to close
  **GO-2026-5856** — an Encrypted Client Hello privacy leak in the standard library's
  `crypto/tls`, reached transitively by the OSV.dev HTTP client and the file hasher. The
  `0.2.3` binaries are built against the patched stdlib; a toolchain recompile, no code change.

### Not yet covered (declared)

- **Index-vs-query analysis** stays deferred — it needs the code↔schema crossing (the
  `AuditContext` does not yet carry it); the DB dimension is still schema-only.
- **DB-012 (never-used index)** remains **permanently** not covered: it needs runtime query
  telemetry (e.g. `pg_stat_user_indexes`) a static, DB-less model cannot read.
- **OLAP / data-warehouse schemas** are out of scope; the DB dimension audits OLTP structure
  only. The routine-body rules add zero value on Prisma-only projects (`schema.prisma` has no
  stored-procedure/trigger block).

## [0.2.2] — 2026-07-17

**Phase 2.2 — DB debt Slice A.** codefit closes part of the Phase-2 debt declared in
`0.2.0`: the N+1 antipattern (RF-04), view sensitive-column exposure (DB-020), and
prefix-redundant indexes (DB-011b), plus the DB coverage prose moved out of a language
provider into a neutral source. The routine-body rules (DB-030/031/040/041) stay deferred
to `0.2.3` — they are blocked at the T-SQL parser layer, not the rule layer. ADRs 0023–0026.

### Added

- **N+1 surface (RF-04)** — `codefit-surface-nplus1` enumerates query calls that sit inside a
  loop (`for`/`while`/`forEach`/`map`/…), as per-endpoint **surface**, never a deterministic
  finding: whether a query-in-a-loop matters is contextual, so codefit states the structural
  fact and the agent judges. Reuses the IDOR/authz frontier verbatim (`auditTargets`,
  `isPrismaCall`, `isServiceCall`), so it applies uniformly across Next.js / Express / Fastify /
  NestJS. ORDERED, never FILTERED: a loop over three literal elements is enumerated exactly like
  one over an unbounded result. (ADR 0023)
- **DB-020 — view sensitive-column exposure** — a schema-only rule mapping columns a `CREATE
  VIEW` exposes whose names look sensitive, as surface (never an affirmation). Runs clean on the
  real Pagila / AdventureWorks / Sakila views. (Unit C)
- **DB-011b — prefix-redundant index** — an index whose columns are a strict leading prefix of
  another index (or the primary key as an implicit index) on the same table. A separate rule
  and category from DB-011a (exact-duplicate); UNIQUE never fires; PK counts as an implicit
  index, matching DB-001. (Unit E)
- **Routine body capture with a `Complete` flag** — the neutral model gains `Body{Text,
  Complete, Note}` on views/procedures/triggers. `Complete` is derived from tokenizer state,
  never by re-scanning `BEGIN`/`END` (the unsound guard ADR 0022 removed); a rule reading a
  body marked incomplete must abstain, never affirm over unread text. (ADR 0025)
- **PostgreSQL trigger→function link** — a PG trigger carries no inline body; its logic lives in
  the executed function. `Trigger.ExecutesFunction` records the link, resolved once in the
  neutral model (`Schema.ExecutedProcedure`), driven by the `TriggerHasInlineBody` dialect
  datum rather than a dialect branch. (ADR 0026)
- **Real vendored routine fixtures** — real upstream Pagila (PostgreSQL) indexes, and the
  AdventureWorks / Sakila real-object DDL, so the index and view rules are exercised against
  genuine schemas rather than hand-written approximations.

### Changed

- **DB-011 split into DB-011a / DB-011b** — the shared DB-011 id is suffixed: DB-011a is the
  existing exact-duplicate rule, DB-011b the new prefix-redundant one. The two share no logic
  and structurally cannot double-report the same index pair.
- **DB coverage prose moved to a neutral source** — the DB dimension's coverage text left
  `internal/providers/typescript/coverage.go` (where it lived as declared location debt) for
  `internal/core/dbcoverage/`, composed into each provider's manifest by `append`, never
  duplicated. `dbcoverage` imports no provider (leaf-pure). `COVERAGE.md` and the `CLAUDE.md`
  documental map are corrected to match; the DB rule count is the real 10.

### Not yet covered (declared)

- **Routine-body rules (DB-030/031/040/041)** — deferred to `0.2.3`, not abandoned. A
  multi-statement T-SQL routine body is captured truncated at its first internal `;`; a rule
  like DB-031 ("is exception handling present?") over a truncated body would falsely affirm an
  absence that is really unread text past the cut. Shipping it would trade an honest gap for a
  rule that lies with confidence. (ADR 0025)
- **DB-012 (never-used index)** — **permanently** not covered, not deferred: detecting an unused
  index needs runtime query telemetry (e.g. `pg_stat_user_indexes`), which exists only inside a
  live database. codefit's model is static and never connects to a database, so DB-012 is
  structurally incompatible with how it operates. (ADR 0024)
- **Index-vs-query analysis** stays deferred (needs the code↔schema crossing, `0.2.3`). The
  view/N+1 rules add zero value on Prisma-only projects (no view block, no raw query surface in
  `schema.prisma`).

## [0.2.1] — 2026-07-08

**Phase 2.1 — multi-dialect SQL-DDL (MySQL, T-SQL).** The database dimension is no longer
PostgreSQL-only: codefit parses MySQL and SQL Server (T-SQL) DDL, selected by `database.type`.
The core stays untouched — a per-dialect DATA descriptor feeds one shared tokenizer and one
dialect-free reducer; PostgreSQL output is byte-identical. ADR 0022.

### Added

- **MySQL SQL-DDL** — `database.type: mysql`: backtick identifiers, `#`/`--` comments,
  `UNSIGNED`/`AUTO_INCREMENT`/`ENUM`/`SET` and the MySQL type vocabulary. (ADR 0022)
- **SQL Server (T-SQL) SQL-DDL** — `database.type: sqlserver`: `[bracket]` identifiers,
  `IDENTITY`, and `nvarchar`/`bit`/`datetime2`/`uniqueidentifier`/`money` mapping. (ADR 0022)
- **Per-dialect `Dialect` descriptor** — dialect differences are DATA (comment / quote / type
  vocabularies), consumed by one shared tokenizer and one dialect-free reducer; adding a
  dialect touches no dialect-agnostic code. Identifier quoting is canonicalized to ANSI `"` at
  tokenization, so the PostgreSQL path is byte-identical. (ADR 0022)
- **Dialect golden fixtures** — MySQL (Sakila) and T-SQL (AdventureWorks) neutral-schema
  goldens, alongside the PostgreSQL (Pagila) regression lock.

### Changed

- **`database.type`** accepts `sqlserver` (alongside `postgresql` / `mysql`). `sqlite` and any
  unrecognized value return an explicit "not supported" note — never a silent PostgreSQL parse.
  Every dialect type maps onto the existing neutral `db.Type` (no core enrichment); an unmapped
  keyword is an honest `TypeUnknown`.

### Not yet covered (declared)

- A T-SQL `GO`-batched stored-procedure/trigger body containing a `CREATE TABLE`-shaped fragment
  may surface as a spurious table (routine bodies are not modeled); MySQL `DELIMITER //`
  bodies are unaffected. A word-based `DELIMITER` (e.g. `DELIMITER GO`) is not recognized. One
  dialect per project; MySQL parsing assumes `ANSI_QUOTES` is off.

## [0.2.0] — 2026-07-05

**Phase 2 — the database dimension.** codefit now audits database structure from the
schema, standalone and inside `scan-all`, scored beside security. Schema-only OLTP
rules; query-driven rules (N+1, index-vs-query), view/procedure/trigger rules, and
OLAP are deferred. ADRs 0014–0021.

### Added

- **Neutral schema model** (`internal/core/db`) — a format-agnostic
  Tables/Columns/Indexes/ForeignKeys (plus Views/Procedures/Triggers) model the rules
  reason over, filled by a provider parser (ADR 0014).
- **Prisma schema parser** — a hand-written, two-pass `schema.prisma` parser behind the
  provider-owned `SchemaParser` capability, resolved by input, not language (ADR 0014).
- **SQL-DDL parser** (`internal/providers/sqlddl`) — a hand-written,
  dollar-quoting-aware incremental reducer over ordered Flyway PostgreSQL migrations
  (ADR 0018).
- **DB sensor + `codefit-scan-db`** — a standalone database-structure audit, with honest
  "not measured" states (no schema / no parser / disabled), never a false clean
  (ADR 0015).
- **Eight schema-only OLTP rules** — structural: table without a primary key (DB-050,
  affirmed), FK with no covering index (DB-001), duplicate index (DB-011), multivalued
  column (DB-002); name-heuristic surface: text-typed FK (DB-051), missing audit
  timestamps (DB-052), sensitive column in the clear (DB-053), repeating groups (DB-003)
  (ADRs 0015, 0017).
- **DB dimension in `codefit-scan-all`** — a parallel, non-endpoint `db` section, plus
  `by_dimension` scoring (`scoring.Compute` wired in) with a global and a per-dimension
  breakdown (ADRs 0020, 0021).
- **Dimension lifecycle doctrine** (ADR 0016) recorded, with a pointer in `CLAUDE.md`.

### Changed

- **Unified multi-sensor baseline** — the baseline diff and prune are scoped by the
  categories of the sensors that ran, so a single-sensor run never marks another
  dimension's items gone (ADR 0019). No committed-format change.

### Not yet covered (declared)

- Query-driven DB rules (N+1, index-vs-query), view/procedure/trigger rules (the SQL-DDL
  parser populates the model, but there are no rules yet), and OLAP.

## [0.1.5] — 2026-06-29

**Phase 1.5 — NestJS surface.** Coverage expansion within Phase 1 (stays in the
`0.1.x` band; `0.2.0` is reserved for Phase 2 — see [VERSIONING.md](VERSIONING.md)).

### Added — Phase 1.5: NestJS surface

- **codefit now maps the IDOR, broken-authorization, and over-fetching surface for
  NestJS**, completing the TypeScript framework set (Next.js, Express, Fastify,
  NestJS). NestJS routes are decorated class methods, so the discovery and three
  decorator-driven readers are new; everything downstream (Prisma detection, the
  indirect/option-C frontier, the surface item builders) is reused unchanged.
- **Handlers detected by HTTP-verb decorator, never by `@Controller`** (ADR 0005). A
  method carrying `@Get`/`@Post`/`@Put`/`@Patch`/`@Delete`/… is a route handler; a
  class without `@Controller` can still expose routes, and a method without a verb
  decorator is not enumerated.
- **Client inputs from parameter decorators** — `@Param('id')`, `@Query()`,
  `@Body()` each bind the parameter as an id-input the downstream follows. A
  `@User`-style injected principal is not treated as a resource id.
- **Guards detected by presence of `@UseGuards`** — on the method or inherited from
  the class. Guard class names are arbitrary, so codefit reports the guard's
  presence (the decorator is the mechanism) and names it, rather than matching a
  known set; a class-level guard applies to every method.
- **Over-fetching sink is the return value** — NestJS serializes the returned value
  to the client (there is no `res.json`), exactly like a Server Action. A service
  call in another file is the cross-file frontier (option C).

### Changed — Phase 1.5

- **`Frameworks()`** for the TypeScript provider now lists `nestjs`.
- **`COVERAGE.md` / coverage manifest** — NestJS moves from *not covered* to the
  reasoning (surface) section with its honest limits; only JS frameworks **beyond**
  Next.js/Express/Fastify/NestJS remain declared as not covered.

### Notes

- Validated against the real RealWorld NestJS + Prisma backend
  (`lujakob/nestjs-realworld-example-app`, branch `prisma`, vendored verbatim as a
  test fixture): codefit surfaces the article controller's IDORs — the
  service-delegated `@Param` slug handlers (`findOne`, `findComments`, …) as
  `indirect_access` (option C).
- Known limit (declared in [COVERAGE.md](COVERAGE.md)): a NestJS service method
  whose name collides with a Prisma method (`create`/`update`/`delete`/…) is
  reported as a *local* Prisma access rather than an indirect call — still surfaced
  with the real callee named (the accepted Prisma-by-shape over-enumeration).

## [0.1.4] — 2026-06-29

**Phase 1.4 — dependency CVEs via OSV.dev (RF-09).** Coverage expansion within
Phase 1 (stays in the `0.1.x` band; `0.2.0` is reserved for Phase 2 — see
[VERSIONING.md](VERSIONING.md)).

### Added — Phase 1.4: `codefit-check-cves`

- **codefit now checks project dependencies for known vulnerabilities** via
  OSV.dev (free, no API key, aggregating the GitHub Advisory Database, the Go vuln
  DB and more). codefit keeps **no vulnerability database of its own** — the data
  is always fresh. The skeleton `core/cve` (types + `Client` contract) is now a
  working OSV.dev client and the `codefit-check-cves` MCP tool is registered.
- **Exact versions only, from lockfiles — never guessed** (RF-09 decision). npm
  versions come from `package-lock.json` (lockfileVersion 1/2/3), Go versions from
  `go.mod` (`require` graph, direct + `// indirect`). The ranges in `package.json`
  (`^1.2.0`) are **not** resolved: when a manifest is present without its lockfile,
  codefit reports an honest note and checks nothing for that ecosystem rather than
  guess an installed version.
- **Reports OSV's severity, does not recompute CVSS.** Per vulnerable dependency:
  the OSV/GHSA/CVE id, summary, severity (the GHSA label or the CVSS vector OSV
  provides, else `UNKNOWN`), first fixed version, and references.
- **Parsers live in `core/cve`, not in `LanguageProvider`** — dependency manifests
  are ecosystem-scoped (package-lock.json, go.mod), not an AST/provider concern, so
  the audit engine and the provider interface are untouched. The tool input is just
  `{root}`; manifests are auto-detected.

### Notes

- Validated against the real `api.osv.dev` (an opt-in `osvlive` test, not run in
  CI): npm `lodash@4.17.4` and Go `github.com/dgrijalva/jwt-go@v3.2.0+incompatible`
  both return their advisories with severity and fixed version — confirming the
  wire format and the Go version normalization (the `v`-prefix strip). CI tests
  mock OSV and never hit the network.
- Known limits (declared in [COVERAGE.md](COVERAGE.md)): only `package-lock.json`
  and `go.mod` at the project root (no yarn/pnpm, no monorepo nesting); a Go
  `replace` is not applied; CVSS is surfaced, not recomputed.

## [0.1.3] — 2026-06-29

**Phase 1.3 — Express & Fastify surface.** Coverage expansion within Phase 1
(stays in the `0.1.x` band; `0.2.0` is reserved for Phase 2 — see
[VERSIONING.md](VERSIONING.md)).

### Added — Phase 1.3: non-Next.js framework surface

- **codefit now maps the IDOR, broken-authorization, and over-fetching surface for
  Express and Fastify**, alongside Next.js App Router route handlers and Server
  Actions. The deterministic security rules already ran on any TypeScript file (no
  framework gate); this slice extends the **surface mapping**, which was the only
  Next.js-specific layer.
- **Discovery is by shape, never by path** (ADR 0005). An Express
  `router.<verb>('/path', …middleware, handler)` call and Fastify's options-object
  form `.<verb>('/path', { handler, preHandler })` are recognized; a same-named
  non-route call (`map.get('/k', v)`, `arr.get(0, cb)`) is rejected because a route
  needs a **string-literal path AND an inline function**.
- **Inputs and sinks are keyed off the handler's parameters, not hardcoded names.**
  The client id-input is read from `req.params`/`query`/`body` (keyed off the
  **first** parameter, so `request` works as well as `req`, and a non-standard route
  param like `slug` is not a blind spot); the over-fetch sink is `.json()`/`.send()`
  on the **second** parameter (`res`/`reply`).
- **The authorization guard is read as route middleware**, not a body call —
  Express positional middleware (`router.post('/x', auth.required, handler)`) and
  Fastify `preHandler`/`onRequest` — so `known_authz_detected` reflects a guard
  applied at the route, and the signal states honestly whether it looked in the body
  or also the route middleware.
- **Cross-file accesses are signalled, not followed (option C).** When a handler
  reaches the resource through a service in another file (the common
  controller→service split), codefit emits the queryable fact `indirect_access=true`
  and names the callee in the new `indirect_call` field — it does **not** follow the
  call across files; the agent reasons over the named function.

### Changed — Phase 1.3

- **`Frameworks()`** for the TypeScript provider now lists `fastify` and no longer
  lists `nestjs` — codefit does not declare a framework it does not cover (NestJS
  routes via decorators, not yet read).
- **`COVERAGE.md` / coverage manifest** updated: Express and Fastify move from
  *not covered* to the reasoning (surface) section with their honest limits; only
  **NestJS** (and frameworks beyond it) remain declared as not covered. A new limit
  is declared: an Express/Fastify handler passed by reference (not inline) is not
  enumerated.

### Notes

- Validated against the real RealWorld Express + Prisma backend
  (`gothinkster/node-express-prisma-v1-official-app`, vendored verbatim as a test
  fixture): codefit surfaces both confirmed IDORs — `PUT` and `DELETE
  /articles/:slug`, which reach the article by a client slug through a service with
  no ownership check — as `indirect_access` items naming `updateArticle` /
  `deleteArticle`.

## [0.1.2] — 2026-06-28

**Phase 1.2 — custom authz helpers + the IDOR/authz decoupling.** Coverage
expansion and a model correction within Phase 1 (stays in the `0.1.x` band;
`0.2.0` is reserved for Phase 2 — see [VERSIONING.md](VERSIONING.md)).

### Added — Phase 1.2: project authz helper registry

- **codefit learns a project's own authorization helpers.** It knew only
  NextAuth-style names (`getServerSession`, `auth`, …), so every project with a
  custom helper (`requirePermission`, `getAuthenticatedUserSalonId`, …) hit a
  false-negative: `known_authz_detected: false` on handlers that ARE checked. Now
  the **agent reasons** which functions are the project's authz helpers, a
  **human approves**, and codefit **persists** the helper in the committed
  `.codefit-baseline` and recognizes it on later scans — without the agent
  re-reasoning. The fix is neither a config list (rots) nor a heuristic
  (name-fragile, ADR 0005) but human-approved project knowledge (ADR 0013).
- **New tools** `codefit-baseline-register-authz-helper` and
  `codefit-baseline-unregister-authz-helper` (`{root, language, helper_name,
  reason}`; reason mandatory, recorded `by: "human"`), with the registered helpers
  surfaced read-only in `codefit-baseline-list`. **Safeguard:** registering
  silences the authz gap on EVERY item that calls the helper (far more reach than
  accepting one item), so it is a human decision — the agent proposes, never
  registers on its own (ADR 0011). The `codefit-surface-*` file-level tools keep
  the built-in set; the registry applies to the project-scan tools.

### Changed — Phase 1.2

- **IDOR is decoupled from authz (a model correction, ADR 0006 amended).** The
  endpoint classifier cleared BOTH the authz and the IDOR access gap on
  `known_authz_detected=true`, conflating two questions: **authz** asks "is the
  caller *permitted*?" (a known helper answers it), **IDOR** asks "does the caller
  *own this resource*?" — which codefit cannot verify from structure. An IDOR with
  a local access now stays **actionable** even when an authz helper is present;
  `known_authz_detected` gates only the authz gap. This fixes a **latent bug** that
  affected built-in helpers too (`getServerSession` on an IDOR was wrongly cleared)
  — a false `resolved_clean` on an unverified IDOR is worse than an honest
  `actionable` (ADR 0005). This is also what makes the helper registry safe: a
  registered helper clears the authz gap, never the IDOR/ownership one.
- **codefit's skill is more imperative.** It now states plainly that to audit you
  MUST call `codefit-scan-all` first and must not audit by reading files manually —
  density of signal, same length, so even a small model triggers the tool instead
  of hand-auditing. It also teaches the human-approved helper registration.
- **`COVERAGE.md` / coverage manifest** updated: the recognized authz helper set is
  now built-in **plus** project-registered; IDOR's actionability is independent of
  authz presence.

### Notes

- Validated on a real Next.js app (salonpro): registering its two helpers flips
  **+119 authz items** to recognized and moves **22 endpoints** to `resolved_clean`,
  while keeping all **89 IDOR items** guarded by the helper **actionable** — under
  the previous (buggy) model those 89 would have been silenced. Not one real IDOR
  was hidden.

## [0.1.1] — 2026-06-28

**Phase 1.1 — Next.js Server Actions surface.** Coverage expansion within Phase 1
(stays in the `0.1.x` band; `0.2.0` is reserved for Phase 2 — see
[VERSIONING.md](VERSIONING.md)).

### Added — Phase 1.1: Server Actions

- **codefit now audits Next.js Server Actions** (`"use server"`) for **IDOR**,
  **broken authorization**, and **over-fetching** — alongside App Router route
  handlers. Server Actions are POST endpoints in disguise (they receive client
  input and reach resources) but have no `route.ts`, so the previous path gate
  missed them entirely.
- **Detection is by shape, never by filename** (ADR 0005). An async function under
  a `"use server"` directive is enumerated whether the directive is **file-level**
  (every exported async function in the module) or **function-level** (an inline
  action in a Server Component, or a non-exported one) — in `actions.ts`, `lib/`,
  or inline, no blind spot. `isNextRouteFile` is untouched; no filename was added
  to any list.
- **The client input is the action's arguments** (or a `FormData`), mapped to the
  same id-input mold as a route handler. An **object-shaped argument is covered**:
  the parameter binding is seeded as the id-var, so a nested `data.id` flows to the
  access. For over-fetching the serialization sink is the action's **return value**
  (the framework serializes it; an action has no `Response.json`).
- **New queryable structural fact `server_action`** (true when the entry is a
  Server Action, not a route handler) — a fact, not a judgment.

### Changed

- **`COVERAGE.md` / coverage manifest** — Server Actions move from
  implicitly-not-covered to **covered**. The honesty contract now declares two
  things explicitly: the **non-Next.js JS frameworks** (Express, Fastify, NestJS)
  are **not yet covered** (a known gap, not silent), and the one Server Action
  edge — an inline `formData.get('key')` passed directly into a service with no
  intermediate variable and no local access — may not link.

### Notes

- Validated against a real Next.js/Prisma app (salonpro): **297 Server Action
  surfaces across 29 files** that were previously invisible (they live in
  `lib/actions/`, not `route.ts`), with **0 parse errors over 223 files**. The
  shape detection surfaces custom auth helpers honestly — an action guarded by a
  project-specific `getAuthenticatedUserSalonId()` is reported as "no known authz
  helper detected, verify" rather than a false positive.

## [0.1.0] — 2026-06-27

**Phase 1 complete.** The pieces below close Phase 1; the earlier `v0.1.0-alpha.*`
entries cover the rest of the phase (security sensor, surface mapping, MCP server,
`scan-all`, `init`).

### Added — Phase 1: baseline (RF-08)

- **Baseline** — a committed `.codefit-baseline` (repo root, shared like
  `.codefit.yaml`) recording codefit's view of the audited surface so a re-scan only
  surfaces what changed. Identity is **by content** (category + file + normalized
  snippet, no line), robust to code moves; the content is hashed, never stored, so a
  secret's text never reaches the committed file (ADR 0009).
- **`codefit-scan-all` is baseline-aware** — it reads the baseline, reports the delta
  (`new` / `changed` / `known` / `gone`), persists the updated baseline, and filters
  the buckets to what is not yet tracked. `known` surface is silenced but counted; a
  **deterministic affirmation is never auto-silenced** — it shows on every scan until a
  human accepts it with a reason (the certainty-graduated safeguard, ADR 0011).
- **`codefit-baseline-list`** — read-only view of tracked items (fingerprint, file,
  category, state, and reason/date if acknowledged); `filter: known | acknowledged`.
  Lets the agent reference items for accept/prune without reading the file.
- **`codefit-baseline-accept`** — record a human's decision to accept an item (false
  positive / accepted debt) with a mandatory reason; recorded as a human decision.
- **`codefit-baseline-prune`** — drop items a refactor resolved (re-scans to confirm
  they are gone first). codefit never edits code — only its baseline (ADR 0010, 0012).

### Fixed

- **The scan path no longer swallows an invalid `.codefit.yaml`.** `runSecurity`
  did `cfg, _ := config.Load(...)`, so a *present but invalid* config (e.g.
  `framework: "nextjs"` instead of `"next"`) was silently dropped to `nil` and
  scans ran with **no `path_criticality`** — a false "all good", the very
  swallowed-error anti-pattern codefit exists to catch. New `config.LoadOptional`
  distinguishes the three states: **absent** → defaults (no error), **present but
  invalid** → a loud, located, field-level error (`invalid framework "nextjs"
  (allowed: …)`), **valid** → loads. The single swallowing call site
  (`internal/mcp/scan.go`) now fails loudly on an invalid config.

## [0.1.0-alpha.2] — 2026-06-25

### Added — Phase 1: `codefit init`

- **`codefit init` is now functional** (was scaffolding). It does three jobs, all
  deterministic and LLM-free:
  - **Detect** the stack from marker files — language (`go.mod`, `package.json` /
    `tsconfig.json`), framework (Next/React/Express from `package.json` deps), ORM
    and database (Prisma schema + its datasource provider) — into a `.codefit.yaml`.
  - **Generate** codefit's own **thin** `SKILL.md` (Anthropic Agent Skills spec:
    `name` + `description`), with the detected language baked into the example
    commands. It triggers and points at the MCP tools; it does not restate what
    codefit already knows.
  - **Place** the skill where each detected agent finds it — Claude Code
    (`.claude/skills/codefit/`), OpenCode (`.opencode/skills/codefit/`), Codex
    (`.agents/skills/codefit/`). Agents are detected by file **or** dir markers
    (`CLAUDE.md` / `.claude`, `opencode.json` / `.opencode`, `.codex`). With no
    agent detected it falls back to `.agents/skills/codefit/` and says so.
- The agent → skill-path table lives in one place (`scaffold.AgentTargets`). The
  existing `.codefit.yaml` is never overwritten without confirmation (`--force`),
  and codefit never touches the user's `AGENTS.md` / `CLAUDE.md`. Every file
  created is reported — nothing is written silently.
- README gains a **Connect codefit** section with per-agent MCP server blocks
  (Claude Code, OpenCode, Codex).
- Validated end-to-end on a real Next.js/Prisma backend (Bitácora): detected
  TypeScript / Next / Prisma / PostgreSQL and 27 route handlers, and placed the
  skill for Claude Code and OpenCode.

## [0.1.0-alpha.1] — 2026-06-24

### Added — Phase 1: MCP stdio server

- **MCP stdio server** — `codefit mcp serve` exposes the engine over the Model
  Context Protocol (stdio), built on the official **MCP Go SDK** (`v1.6.1`,
  audited in ADR 0007). Tools registered: `codefit-scan-security`,
  `codefit-scan-all`, `codefit-scan-endpoint`, `codefit-surface-idor` / `-authz` /
  `-overfetch`, `codefit-confirm-surface`, `codefit-coverage`. Each is a thin adapter to the
  existing core handlers; no audit logic in the MCP layer. Verified by a
  client↔server protocol integration test (initialize → tools/list → tools/call).
  The HTTP/SSE transport (`--port`) is deferred.
- Go toolchain pinned to `go1.25.11` (the SDK requires Go 1.25+); minimum Go
  bumped from 1.24 to 1.25.

### Changed — Phase 1: scan-all actionable summary

- **`codefit-scan-all` returns a three-bucket summary instead of the full item
  dump** (ADR 0008). The response was large enough on a real backend (~101 surface
  items, ~80 KB) that MCP clients truncated it across 4 models. Now, split by facts
  codefit already computes: `actionable` — endpoints resolved locally with a gap
  (full detail); `resolved_clean` — endpoints resolved locally with no gap, controls
  present (named with a verification fact, **not** flattened with frontier, because
  a positive check is epistemologically opposite to a non-conclusion);
  `frontier_pending` — endpoints whose data left the handler body (named). When
  nothing resolved locally the frontier note states it is **not** a clean result. On
  the Bitácora backend: 80 KB → 24 KB (29.7%), 10 / 11 / 24. Breaking output change
  (`Endpoints` → `actionable` + `resolved_clean` + `frontier_pending`), acceptable
  pre-release.
- **New tool `codefit-scan-endpoint`** — re-analyses one file on demand and returns
  its endpoints' full concerns; stateless (re-runs the static analysis, stores
  nothing). Used to fetch the detail of a `frontier_pending` endpoint.
- **Frontier surface signals reworded as unresolved candidates.** The IDOR, authz,
  and over-fetching frontier signals (data left the handler body) were phrased
  around what codefit did *not* find ("No direct Prisma access detected", "could not
  check", "may be in a service/repository layer (follow … to confirm)"), which read
  as a negative result — in real dogfooding the agent discarded them as probable
  false positives. They now state the limit as an affirmation ("codefit does not
  follow calls across functions, so this is NOT verified here") and make following
  the data its own instruction. Detection is unchanged — the same items are
  enumerated; only the wording changed.

### Added — Phase 1: TypeScript security + surface mapping

- **TypeScript `LanguageProvider`** backed by gotreesitter (pure Go, no CGO —
  ADR 0002), behind the parser-agnostic `core/syntax.Node` AST boundary
  (ADR 0003).
- **Deterministic TypeScript security rules** — five categories asserted with
  certainty 1.0: hardcoded secrets, weak crypto (MD5/SHA-1, insecure
  `Math.random` for tokens), dangerous `eval`/`new Function`, inline SQL
  injection, inline XSS via `dangerouslySetInnerHTML`. Declarative YAML in a
  Semgrep-format subset, matched by a pure-Go engine (`internal/core/ruleengine`)
  — no OCaml/OpenGrep embedded. Scope and known limits in `COVERAGE.md`. (ADR 0004)
- **Surface mapping framework** (`internal/core/surface`) — the product
  differentiator: `SurfaceItem` with a stable id and queryable `StructuralFacts`,
  the `Query` interface, and the stateless confirmation flow (the agent's verdicts
  become probabilistic findings, confidence < 1.0).
- **Three surface categories for TypeScript / Next.js / Prisma**, validated
  against a real backend: **IDOR** (id→resource endpoints), **broken
  authorization** (sensitive handlers), and **over-fetching** (domain-object
  serializations). Detection is by structural shape, never by name; the
  finite/infinite frontier is declared (ADR 0005).
- **`scan-all` synthesis** (`report.AggregateEndpoints`) — the complete picture
  aggregated per endpoint: deterministic findings and surface concerns of the same
  handler together, with three certainty levels (deterministic → surface-confirmed
  → frontier), the affirm/ask distinction preserved, ordered by actionable
  structural gap (never by severity). Agent-first JSON; a human renderer
  (`export-report`) is registered pending (PRD §27). (ADR 0006)
- **Coverage manifest** per provider (`COVERAGE.md`, derived from the in-code
  manifest) — declares what is audited deterministically vs reasoned over surface
  vs not covered, including the known limits.

### Added — Phase 0: foundations

- Three-layer architecture: `core/` (universal engine), `sensors/` (audit
  logic), `providers/` (per-language), so adding a language never touches the
  core.
- Project config parser (`.codefit.yaml`) with located validation errors and
  `path_criticality` support (RF-11).
- Core engine: filtering pyramid, content-hash cache, scoring, and the canonical
  JSON report (`schema_version` 1.0).
- **Go `LanguageProvider`** backed by `go/ast` (no CGO): static security and
  best-practice detectors.
- **Security sensor** (regex + AST layers) with severity adjustment by path
  criticality.
- Plumbing CLI built on cobra: `mcp serve`, `status`, and `version` work;
  `init` and `update` are scaffolding.
- Self-audit: codefit scans its own code in CI as a Go integration test.
- CI/CD: GitHub Actions for test/lint/build, goreleaser config, and weekly
  dependency vulnerability scanning (`govulncheck` pinned to v1.1.4).

### Changed

- **Architecture is MCP-first, pure.** codefit has no audit CLI and never calls
  or manages any LLM; the binary exposes only plumbing commands. The deterministic
  layers run in-process and the surface is returned to the agent, which reasons
  with its own model.

### Fixed

- Deduplicate overlapping layer-1 (regex) and layer-2 (AST) secret findings in
  the security sensor — closing a latent double-report that affected the Go
  provider too.
- `.gitignore` `coverage.*` was swallowing `coverage.go` source; narrowed the
  rule and added a CI guard against `.gitignore` swallowing source files.

### Notes

- `CGO_ENABLED=0` everywhere; cross-compiles to linux/amd64, linux/arm64,
  windows/amd64, darwin/arm64.
