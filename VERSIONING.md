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
