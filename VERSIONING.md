# Versioning

codefit follows [Semantic Versioning 2.0](https://semver.org/spec/v2.0.0.html) with
pre-releases, mapped to the PRD rollout phases (PRD §25). This document is the
contract for what each number means and when it moves — so the version is honest and
consistent over time, like the rest of the docs.

## The scheme

While codefit is pre-stable it stays on **`0.x`**. The version is derived from the
nearest git tag at build time (`git describe --tags`), so `codefit version` always
reflects the real commit, never a hand-set string.

```
0 . MINOR . PATCH [ -PRERELEASE ]
    │       │       │
    │       │       └─ alpha / beta / rc — see "Pre-releases" below
    │       └───────── bug fixes and small additions within the current phase
    └───────────────── one PRD phase completed (see the map below)
```

### MINOR ↔ PRD phase

Each PRD phase that **closes** raises the MINOR. The MINOR lands (no pre-release
suffix) only when the phase is **complete and usable end-to-end from `main`** — same
honesty rule as the README and CHANGELOG: we do not announce a phase as done while
pieces of it are still stubs.

| Version  | PRD phase | Meaning |
|----------|-----------|---------|
| `0.1.0`  | Phase 1   | TS provider + security sensor + surface mapping + **`init` (config + skill) and baseline** functional (`update` is Phase 4) |
| `0.2.0`  | Phase 2   | DB dimension: schema-only OLTP rules (Prisma + SQL-DDL parsers), `scan-db`, DB in `scan-all` + `by_dimension` |
| `0.2.1`  | Phase 2.1 | Multi-dialect SQL-DDL: MySQL + SQL Server (T-SQL) alongside PostgreSQL (per-dialect DATA descriptor, ADR 0022; PG byte-identical) |
| `0.2.2`  | Phase 2.2 | DB debt Slice A: N+1 surface (RF-04), view sensitive-column (DB-020), prefix-redundant index (DB-011b), neutral DB coverage source (ADRs 0023–0026) |
| `0.2.3`  | Phase 2.3 | Routine-body rules: dynamic SQL (DB-030), exception handling (DB-031), trigger cross-table cascade (DB-040), trigger external call (DB-041), over de-truncated T-SQL bodies (ADRs 0027–0028) |
| `0.2.4`  | Phase 2.4 | Index-vs-query cross (a Phase-2 debt deferred in `0.2.0`; OLAP still open): filtered column without an index (DB-010), multi-column filter without a composite index (DB-013), neutral cross infrastructure + four dogfood-driven noise corrections (ADRs 0029–0032) |
| `0.2.5`  | Phase 2.5 | RF-03 OLAP closure (the last Phase-2 debt): paradigm/table-role detection + 3NF-suppression on OLAP, star-schema/SCD rules (DW-001/002/005/010/011), columnar index (DW-021) and partitioning (DW-020). Paradigm/role architecture in ADR 0033. DW-022 permanently dropped — recorded in the coverage manifest; its ADR is still owed, unlike the structurally identical DB-012 exclusion (ADR 0024) |
| `0.3.0`  | Phase 3   | Code review + best practices + tests + regression risk |
| `0.4.0`  | Phase 4   | Knowledge packs + coverage manifest + public `v0.1.0`-class release |
| `1.0.0`  | —         | Stable API; post-1.0 brings Java (`1.1`), Python (`1.2`) |

> The Phase 4 row is where the project first cuts a public `0.x` release; the table
> maps phases to MINORs, the actual public-release milestone is tracked in the PRD.

### PATCH

Bug fixes and small additions that do not close a phase raise the PATCH within the
current MINOR line (e.g. a fix after `0.1.0` → `0.1.1`).

## Pre-releases

Before a MINOR is complete, builds toward it carry a pre-release suffix on the
**target** version, ordered `alpha < beta < rc < (final)`:

- **`-alpha.N`** — usable core of the phase, validated, but the phase is not
  feature-complete (pieces still missing or stubbed).
- **`-beta.N`** — feature-complete for the phase, stabilising; APIs may still shift.
- **`-rc.N`** — release candidate; no known blockers, final checks only.

A pre-release tag like `v0.1.0-alpha.1` means "on the way to `0.1.0`, at the alpha
stage" — it does **not** claim `0.1.0` is done.

## Current state

- **`v0.2.5-alpha.2` — still on the way to Phase 2.5; a correctness fix, not new coverage.**
  The database dimension stops concluding from parser silence. `DB-050` was affirming "no
  primary key" at confidence 1.0 over vendored DDL that declares the keys, because the
  SQL-DDL reducer discarded shapes outside its declared subset without signalling it. ADR
  **0034** generalises the `db.Body{Complete,Note}` precedent to the table's structural set:
  the eight absence-based DB/DW rules now gate on completeness — seven abstain, and `DB-050`
  routes to a `db-table-structure-unproven` surface item carrying the raw statement and
  `file:line` rather than losing the signal. Also fixed: four surface categories emitted
  since `v0.2.3` without being declared in `OwnedCategories()` (ADR 0019 — the one way to
  corrupt a committed baseline), caught by the new `dbcoverage` enforcement test on its first
  run. **Alpha, not `0.2.5`:** DW-021 and DW-020 were still open as of this tag — see the
  `alpha.1` entry below for what each needed. The `feat/olap-columnar-index` draft (PR
  #75) that a full 4R found unsound on every input path was abandoned, not merged: the
  4R traced the unsoundness to the PARSER (it could not read index access methods at
  all), not the rule, so the fix landed at that layer first, on `main`, past this tag
  and still untagged as of this writing; a rule-only DW-021 was then rebuilt from
  scratch and **has since merged to `main`** (still untagged), closing slice S3 and
  taking `dwrules.All()` to **six** rules. **DW-020 (partitioning) remains open.**
  Neither DW-021 nor DW-020 has shipped in a *tagged* release
  yet — this bullet is history, not current status.
- **`v0.2.5-alpha.1` — on the way to Phase 2.5 (RF-03 OLAP closure).** codefit tells a data
  warehouse from a transactional database and audits its modelling: paradigm/table-role
  detection as a pure core leaf (prefixes corroborated by real FK fan-out/fan-in, never by a
  lone surrogate key), `database.paradigm: auto` where detection seeds and an explicit value
  overrides, 3NF-suppression on fact/dimension/mart tables with an audit trace in the sensor's
  `Note`, and the star-schema/SCD family DW-001/002/005/010/011 — all **surface**, never
  affirmations (ADR 0017), wired into `scan-db` and the DB bucket of `scan-all`, and
  baselineable (ADR 0019). **Alpha, not `0.2.5`:** as of this tag, two of the eight items in the PRD's OLAP
  scope remained — DW-021 (columnar index) and DW-020 (partitioning) — and neither was a
  rule-only slice: DW-021 needed a `Method` field the neutral `db.Index` did not yet have, and
  DW-020 needed the SQL-DDL reducer to start capturing the `PARTITION BY` clauses it
  declared as a limit and skipped, per dialect. Role detection reached only a leading snake_case
  segment at this tag; declared, test-locked, not silent. (Both of that bullet's limits have
  since moved on `main`, past this tag and still untagged as of this writing: `db.Index.Method`
  landed — see the `alpha.2` entry above — the `PARTITION BY`/`PARTITION OF`/T-SQL
  partition-scheme capture DW-020 was waiting on landed too (`db.Table.Partitioning`; the
  DW-020 RULE is still not built), and the role vocabulary now recognizes
  underscore-delimited leading **and** trailing tokens plus separator-free PascalCase, all
  case-insensitively, so Kimball's `FactInternetSales`/`DimCustomer` spelling is recognized;
  all-caps names remain unclassified by design.)
- **`v0.2.4` — Phase 2.4 complete (index-vs-query).** The index-vs-query DB debt deferred in
  `0.2.0` is paid (OLAP remains the open Phase-2 debt): the first rules that cross the code's
  queries against the schema. A filtered
  column with no covering index (DB-010) and a multi-column filter with no covering composite index
  (DB-013), both **surface** — matched through neutral infrastructure so the core imports no provider
  (a `QueryFilter` from a `QueryExtractor`, mirroring the `SchemaParser`; the merge is byte-identical
  under a mutation-tested seam gate). Prisma only, in `scan-all`. Calibrated on four real schemas of
  different conventions (node-express-prisma, umami, a private ~40-model SaaS, papermark) with zero
  undeniable false positives; four dogfood-driven noise corrections (unique-subset short-circuit,
  low-cardinality-by-type skip, high-arity abstention, cross-model grouping). ADRs 0029–0032.
- **`v0.2.3` — Phase 2.3 complete (routine-body rules).** The last Phase-2 DB debt that needs a
  routine's body is paid: four surface rules over the captured body across PostgreSQL, MySQL, and
  SQL Server — dynamic SQL construction (DB-030), missing exception handling (DB-031), trigger
  cross-table cascade (DB-040), and trigger external-effecting call (DB-041). The prerequisite was
  a parser fix — T-SQL multi-statement routine bodies are captured **complete** to the `GO` batch
  separator (ADR 0027), closing ADR 0022's phantom-table limit as a side effect; PostgreSQL/MySQL
  output stays byte-identical. Each rule is surface, gated on `Body.Complete`, with a distinctive
  trap test-locked (RAISE EXCEPTION, UPDATE(column), internal EXECUTE). Coverage doctrine (ADR
  0028) adds a third state, *detectable-without-dogfood*, and the fixture gap policy. The `0.2.3`
  binaries compile against `go1.25.12` (GO-2026-5856 / crypto/tls fixed). **Deferred:**
  index-vs-query (needs the code↔schema crossing the `AuditContext` does not yet carry); OLAP;
  DB-012 (never-used index) is **permanently** not covered (needs runtime telemetry).
- **`v0.2.2` — Phase 2.2 complete (DB debt Slice A).** Part of the Phase-2 debt declared in
  `0.2.0` is paid: the N+1 antipattern as per-endpoint surface (RF-04, `codefit-surface-nplus1`,
  reusing the IDOR/authz frontier; ADR 0023), view sensitive-column exposure (DB-020), and
  prefix-redundant indexes (DB-011b, alongside the renamed DB-011a exact-duplicate). The DB
  coverage prose moved from the TypeScript provider to a neutral source
  (`internal/core/dbcoverage/`), composed by `append` and importing no provider. Routine bodies
  are captured with a tokenizer-derived `Complete` flag and the PG trigger→function link is
  modeled (ADRs 0025, 0026). Dogfooded on real vendored Pagila / Sakila / AdventureWorks DDL.
  **Deferred:** routine-body rules (DB-030/031/040/041) to `0.2.3` — blocked at the T-SQL parser
  layer, not the rule layer (ADR 0025); DB-012 (never-used index) is **permanently** not covered
  (needs runtime telemetry, incompatible with the static model; ADR 0024); index-vs-query stays
  deferred; view/N+1 rules add zero value on Prisma-only projects.
- **`v0.2.1` — Phase 2.1 complete (multi-dialect SQL-DDL).** The database dimension is no
  longer PostgreSQL-only: MySQL and SQL Server (T-SQL) DDL are parsed, selected by
  `database.type`, through a per-dialect DATA descriptor feeding one shared tokenizer and one
  dialect-free reducer (ADR 0022); the PostgreSQL path stays byte-identical. Config +
  MCP-adapter wiring, MySQL (Sakila) and T-SQL (AdventureWorks) golden fixtures, and `sqlite`
  as an explicit not-supported note (never a silent Postgres parse). **Deferred (declared
  limits):** a T-SQL `GO`-batched routine body with a `CREATE TABLE`-shaped fragment may
  surface a spurious table (MySQL `DELIMITER //` bodies unaffected); a word-based `DELIMITER`
  (e.g. `DELIMITER GO`) is not recognized; one dialect per project; MySQL assumes
  `ANSI_QUOTES` off.
- **`v0.2.0` — Phase 2 complete (the database dimension).** Usable end-to-end from
  `main`: a neutral schema model with a Prisma parser and a SQL-DDL (Flyway) parser,
  eight schema-only OLTP rules, the standalone `codefit-scan-db`, the DB dimension
  running inside `scan-all` as a parallel section, and `by_dimension` scoring beside
  security. Dogfooded on a real Prisma backend and a real SQL-DDL/Postgres backend.
  **Deferred (not in `0.2.0`):** N+1 and index-vs-query rules, view/procedure/trigger
  rules, and OLAP.
- **`v0.1.0` — Phase 1 complete.** Usable end-to-end from `main`: the MCP stdio
  server, deterministic TypeScript security rules, surface mapping (IDOR / broken
  authz / over-fetching), the `scan-all` three-bucket synthesis with `scan-endpoint`
  on demand, `codefit init` (config + self-discovering skill), and the **baseline**
  (committed audit memory with list / accept / prune). Validated in real use against a
  Next.js/Prisma backend. Cut from `main` as part of the documentation-sync release
  that closes Phase 1.
- On the way to it: **`v0.1.0-alpha.2`** (2026-06-25, added `codefit init`) and
  **`v0.1.0-alpha.1`** (2026-06-24, the first usable MCP core).
- `codefit update` is a Phase 4 item (`0.4.0`), not a Phase 1 blocker.

## How to tag a release

```bash
# Annotated tag on a clean main with the gate green:
git tag -a v0.1.0 -m "codefit v0.1.0 — Phase 1 complete"
git push origin v0.1.0

# Verify the build embeds it:
make build && ./bin/codefit version   # → codefit v0.1.0 (commit …, built …)
```
