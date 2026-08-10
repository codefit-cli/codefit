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
| `0.2.5`  | Phase 2.5 | RF-03 OLAP closure (the last Phase-2 debt): paradigm/table-role detection + 3NF-suppression on OLAP, star-schema/SCD rules (DW-001/002/005/010/011), columnar index (DW-021) and partitioning (DW-020). Paradigm/role architecture in ADR 0033. DW-022 permanently dropped — recorded in the coverage manifest; its ADR is still owed, unlike the structurally identical DB-012 exclusion (ADR 0024) *(superseded 2026-08-10 — [ADR 0063](docs/decisions/0063-materialized-view-refresh-is-surface-not-a-permanent-exclusion.md) pays the owed ADR by REVERSING the exclusion, not confirming it: not built, still declared, see the row below and the coverage manifest)* |
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

- **In flight: the `0.3.0` line. Phase 3 has started.** The next tag is a
  **`v0.3.0-alpha.N`**, not a `v0.2.8` — per the Pre-releases rule above, work *toward* a
  MINOR carries the pre-release suffix on the **target** version, and `0.3.0` is Phase 3's
  target in the table. `v0.2.7` was the last tag on the `0.2` line: it shipped Phase 3's
  prerequisite thread (**H0**) as a PATCH because it closed no phase, changed no audit rule
  and fixed live defects in already-released code — defensible under the PATCH rule, and it
  is history now, not a precedent. From **H1 onward the work is new capability**, so it goes
  on `0.3.0-alpha.N`. What is on `main` and unreleased today: `practices` is a weighted
  dimension in `scoring.DefaultWeights()` (5 points, funded by `complexity`; ADR 0055) — and
  **that is all of it**. There is still no practices sensor, no practices rule and no
  `codefit-check-practices` tool, so `by_dimension.practices` is `null` on every response.
  The MINOR ↔ phase table gains its `0.3.0` row only when Phase 3 is **complete and usable
  end-to-end from `main`** — review, practices, tests and regression risk, not one of them.
  See the [CHANGELOG](CHANGELOG.md) `[Unreleased]` section for the itemized list.
- **`v0.2.7` — a PATCH: no phase closes here.** *As of that tag*, Phase 3
  (code review, best practices, tests, regression risk) had **not started**; its dimensions
  — review, practices, tests — did not exist yet. (Superseded by the bullet above: Phase 3
  has since started on `main`.) This is the prerequisite thread (H0) that
  unblocks the regression-risk half of RF-06, which cannot exist without a notion of *what
  changed*. So the line stays `0.2` and the MINOR ↔ phase table above gains **no row** —
  that table maps phase *closures*, and this closes none. **No audit rule changed**: for a
  full scan no finding, surface item or baseline fingerprint moves, and `COVERAGE.md` and
  the `codefit-coverage` manifest are untouched — so, unlike `v0.2.5`, this release is
  **not breaking for a committed baseline** and needs no re-accepting. **⚠️ It IS breaking
  for a consumer that parses the `codefit-scan-all` response**, and that is the one
  user-visible contract change in this PATCH: `actionable` no longer carries per-concern
  detail (see the ADR 0054 paragraph below). What lands are the **two cheap layers of the
  filtering pyramid that were still missing** — layer 0 (which files get audited) and the
  content-hash finding cache (which results get recomputed). First, **layer 0**: an optional
  `changed_files` on `codefit-scan-security` and `codefit-scan-all`
  narrows the audit to the paths the agent passes. The scope is an **input**, never derived
  from git — codefit has no power over the user's git, and the calling agent already knows
  what it touched; absent or empty means a **full** audit, never "audit nothing". Because a
  partial audit indistinguishable from a full one is a lying auditor, every response now
  carries a `scope` block (`mode`/`requested`/`audited`/`auditable_total`/`unmatched`/`note`,
  the note enforced in the handler in both directions), a partial `blocked: false` is
  declared as the narrower claim it is (*no critical in the audited slice*), the DB dimension
  reports **`null` (not measured)** rather than `100` when no configured schema path is in
  scope, and the baseline's `gone` scope becomes **two-dimensional** (category **and** file)
  so a partial pass can no longer prune the audit memory of files it never opened —
  `codefit-baseline-prune` accepts no scope at all. The dead `AuditContext.Since` field, which
  promised a git-ref `--since` mode and never had a reader or a writer, is **removed**.
  Second, the **content-hash finding cache is wired** into the security sensor and consulted
  per file inside the walk: the same analyzer, over the same bytes at the same path, no
  longer re-analyses them. It is **opt-in** (a project with no `cache:` section in
  `.codefit.yaml` has it off) and it exists so the **honest** full scan stays affordable —
  if the full scan were expensive and the narrowed one cheap, every caller would narrow and
  codefit could never prune the baseline again. **A warm scan and a cold scan are
  byte-identical**, and every cache failure is a miss, never a failed audit. The key is
  `sha256(analyzer identity ‖ project-relative path ‖ content)`, where the analyzer identity
  is the **SHA-256 of the running executable** rather than a version string — `version.Version`
  is the constant `"v0.1.0-dev"` for any plain `go build`, so a version key would go stale
  first for the rule author, while the binary's own bytes cover every input that can change a
  verdict. That key has an arithmetic — **every new analyzer binary orphans a whole
  generation of entries** — so the **store bounds itself**: entries live under a generation
  directory, and opening the cache keeps the current generation plus the two most recently
  modified others, drops entries in the current generation unwritten for 30 days, and
  removes the flat entries of the previous layout. It is a **retention** bound, not a size
  one: codefit measures no bytes and evicts nothing by size, and the prune only ever touches
  the two filename shapes it writes itself, so nothing else in `.codefit/cache` is at risk at
  any age — the flip side being that a `.entry-*.tmp` a crashed write leaves in the *current*
  generation is not entry-shaped either, so it waits for that generation to be superseded.
  `rm -rf .codefit/cache` stays safe and stays the escape hatch. **Both are now exercised on
  real projects rather than on fixtures alone**: a committed, build-tagged dogfood harness
  (`internal/mcp/dogfood_cache_test.go`, excluded from the gate, skipping clean without a
  gitignored per-machine `dogfood.local.json`) runs the real security sensor cold and warm over
  four real TypeScript projects, read-only, and drives layer 0 over the same corpus with
  non-canonically spelled paths. Cold and warm came out **byte-identical**, the warm runs
  re-analysed **0 files**, and the narrowing kept its denominator whole with nothing unmatched.
  It also produced codefit's **first measured milliseconds** — 317/147/309/14 files audited,
  cold 5989/2473/5023/465 ms against warm 514/168/265/11 ms, on **one Windows machine on
  2026-08-03**. Read that as a measurement and not as a property: cold runs varied about **±2x**
  across repeats (5989–11627 ms on the largest project) through the OS's own filesystem caching,
  the warm figures were stable, **two of the four projects produce zero findings and zero
  surface** (a Vite SPA and a project whose only route handler is a static healthz — ADR 0005's
  frontier is right to emit nothing), and four projects one person had clones of are no
  representative sample. ADRs **0050**, **0051** and **0052**.
  **And a false all-clear was fixed before release, which is why this PATCH is not
  cosmetic:** the reader treated **any valid JSON at an entry's path as a hit**, so a stray
  `{}` or a backup artifact in `.codefit/cache` unmarshalled into an empty entry and was
  served as *analysed, nothing found* (reproduced: score 100 and no SEC-001 on a file leaking
  a credential). The entry is now **self-describing** — `Set` stamps its key and `Get`
  verifies it — so anything that cannot prove it belongs to the key being read is a miss.
  Two limits are declared rather than fixed: the cache barely warms under concurrent tool
  calls on **Windows** (`os.Rename` cannot replace a file another reader holds open; the
  failure is safe but noisy), and a separate, unproven **empty-read** hole. ADR **0053**.
  (**Narrowed after this tag** — the empty-read hole's cache half was reproduced and disproved,
  not left unproven; see ADR 0053's superseding note and `docs/roadmap.md` P0-3.)
  The contract is
  `docs/specs/finding-cache.md`. Also removed: the
  **LLM-era scaffolding**. `internal/core/pipeline`
  was designed around an early exit before a paid layer-3 LLM call; the MCP-first pivot
  deleted that layer on the package's second day and all three implemented layers went on to
  bypass it, so it goes, along with the `NoLLM`, `FailOn` and `Interactive` fields of
  `AuditContext` from the same extinction (ADR **0049**). The pyramid itself is doctrine and
  stays. ADR **0048**; the contract is `docs/specs/change-scope.md`.
  **Also fixed before release, and the reason this PATCH is blocking rather than optional:
  `codefit-scan-all` did not RETURN on a mid-sized real project.** Over a 317-file TypeScript
  repository it produced **313 368 bytes** and exceeded the MCP client's output limit — the
  tool codefit's own skill tells an agent to call first. 99.3 % of it was one section:
  `actionable` inlined 367 concerns at ~794 bytes each while `frontier_pending` named its
  endpoints in ~122 bytes apiece — an old decision (ADRs 0006/0008, PRD §21) applied to half
  the response. `actionable` now **names** its endpoints with what it takes to rank them and
  the concern text is fetched on demand with `codefit-scan-endpoint`; deterministic findings
  stay in full, because a fact codefit already concluded must not depend on the agent choosing
  to look. Measured through the dogfood harness: **salonpro 313 368 → 42 012 bytes** with all
  160 actionable endpoints still named. Naming is a constant factor and not a bound, so every
  response now declares a byte `budget` and, when the lists do not fit, states how many
  endpoints it withheld and what ordering they are a prefix of — never a silent cut. **Only
  the rendering narrows**: `score`, the baseline delta, the summary and the `scope` block are
  still computed over the complete analysis, locked against a pre-change golden and by
  comparing two runs at different budgets. ADR **0054**; the contract is
  `docs/specs/scan-all-response-budget.md`. See the
  [CHANGELOG](CHANGELOG.md) for the itemized list.
- **`v0.2.6` — a PATCH: no phase closes here.** Phase 3 (code review, best practices,
  tests, regression risk) has not started, so the line stays `0.2` and the MINOR ↔ phase
  table above gains **no row** — that table maps phase *closures* to versions, and this
  release closes none. **No audit rule changed**: every security, DB and DW rule behaves
  exactly as it did in `v0.2.5`, and no finding, surface item or baseline fingerprint
  moves. Two things land. First, an **agent-facing correction**: the skill `codefit init`
  generates had fallen two phases behind the MCP server — it described only endpoint
  security while the database dimension shipped across `v0.2.0`–`v0.2.5`, and its
  frontmatter `description` **gates progressive disclosure**, so an agent starting a
  database task never loaded the skill at all. It now names `codefit-scan-db`,
  `codefit-coverage` and `codefit-check-cves`, teaches the DB dimension and its
  honest-abstention contract, and is held by a drift lock against the live MCP session
  (`TestSkillNamesEveryRegisteredTool`) plus a committed copy at
  `.claude/skills/codefit/SKILL.md` locked byte-for-byte to `RenderSkill`
  (`TestCommittedSkillMatchesRenderedSkill`). **This reaches an existing install only by
  re-running `codefit init`** — a skill file already on disk stays stale until it is
  regenerated. Second, **Windows checkout portability**: `go test ./...` failed eleven
  tests on a clean Windows clone of a tree that was green on Linux CI. The causes were a
  missing `.gitattributes` (now pinning LF repo-wide — an existing checkout needs
  `git add --renormalize .` once, since it does not apply retroactively), `codefit init`
  printing native separators while writing forward slashes into the `.codefit.yaml` of the
  same run, and `make build` writing a suffix-less `bin/codefit` that Windows will not
  resolve as an executable. **Declared, not fixed:** under a *native* Windows `make`
  driving `cmd.exe`, the `date` and `git rev-parse` shell-outs still misbehave, so
  `Commit` and `BuildDate` can be injected as garbage; building from Git Bash is
  unaffected. See the [CHANGELOG](CHANGELOG.md) for the itemized list.
- **`v0.2.5` — Phase 2.5 complete (RF-03 OLAP closure), and with it the last Phase-2
  coverage debt.** Usable end-to-end from `main`: the DW-0xx family is closed. **DW-021**
  (a fact table with no columnar/analytic index) and **DW-020** (a schema-level census of
  the fact tables' declared partitioning — one item per schema, never one per table) land
  here together with the parser floors they read, `db.Index.Method` and
  `db.Table.Partitioning`, taking `dwrules.All()` to **seven** rules
  (DW-001/002/005/010/011/020/021) with **no DW-0xx rule left unbuilt**; the two alpha
  entries below carry the rest of the phase. DW-022 stays **permanently** dropped —
  refresh cadence lives in scheduler state that static DDL does not carry.
  *(Superseded 2026-08-10, kept append-only as the record of the original call:
  [ADR 0063](docs/decisions/0063-materialized-view-refresh-is-surface-not-a-permanent-exclusion.md)
  reverses the exclusion — codefit cannot AFFIRM staleness, but it can ENUMERATE the
  materialized views as surface for the agent to resolve. Decided and recorded, not
  built: `db.View` still has no way to say a view is materialized. `DB-022`, the OLTP
  twin, takes the same reversal.)* The phase's
  largest structural change was not in the original plan: the **schema gate** (ADR
  **0037**, which inverts ADR 0033) judges the whole schema **before** any table is given
  a warehouse role, so one table named `dim_status` can no longer silence its own
  DB-002/DB-003 1NF findings inside an otherwise transactional schema — and the verdict is
  measured, not reasoned (of six schema-wide signals computed over 26 public corpora, the
  three that measured zero false positives are the three that vote). Most of the rest was
  **parser correctness, largely removing false positives rather than adding coverage**:
  run-on `CREATE TABLE` statements separated at the tail boundary; a missing comma before
  a table-level key constraint no longer fabricating a single-column key; a delimited type
  name (`[int]`, backticks, ANSI quotes) resolving instead of falling back to
  `TypeUnknown`; `CREATE SEQUENCE` and views no longer materializing phantom tables; body
  items anchored on their own source line; an honest-abstention floor for the
  `CREATE TABLE` family, with `UNLOGGED` modeled and session-scoped TEMP forms withheld
  under a trace; BOM-marked sources decoded and a source that contributes nothing to the
  model reported rather than silently treated as clean; and DB-052 reading a measured
  verb+affix+type audit-stamp rule instead of a two-name list. Across the 26-corpus survey
  the net effect was **938 → 873 items** with **no rule gaining items anywhere**. ADRs
  **0035–0047**. **The Phase-2 "done" criterion is satisfied:** the PRD asks that
  `codefit-scan-db` produce verified real findings on a real project, and over an
  untouched UTF-16LE `pg_dump` of a production Postgres backend the real handler reports
  `measured: true`, 9 tables, 9 of 9 structurally proven, **12 surface items and 0
  deterministic findings**, paradigm `oltp` — **0 false positives** on hand-verification of
  all 12, plus a verified **true negative** (11 FKs declared, the FK rule fires on 10 and
  stays correctly silent on the one a `UNIQUE` constraint already covers); before the
  encoding fix that same file audited as `measured: true`, score 100, empty note, **0
  tables**. **Read honestly**, four caveats rather than one number: that project is a
  **narrow slice** — 9 tables, no views, procedures or triggers, nothing analytic — so
  only **3 of the 21 DB/DW rule families fired**, and the 26-corpus survey, not this
  project, is what evidences breadth; its `score` is **100** *alongside* those 12 items,
  correct by design (surface is a question for the agent, so it is never scored) but it
  reads as "clean" to anyone who looks at the score first; **PII coverage is partial and an
  open design question, not a settled exclusion** — a column named `email` does not fire
  DB-053, while `ssn`/`dni`/`creditcard` already do, so the boundary is not clean today and
  a separate personal-data category is a candidate, not decided and not scheduled; and
  **⚠️ BREAKING for committed baselines** — anchoring body items on their own source line
  changes the fingerprint of every column-anchored DB item (DB-002, DB-003, DB-051,
  DB-053), so existing `.codefit-baseline` entries for those categories re-appear as `new`
  on the first scan after upgrading, until they are re-accepted. See the
  [CHANGELOG](CHANGELOG.md) for the itemized list.
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
  all), not the rule, so the fix landed at that layer first, past this tag; a
  rule-only DW-021 was then rebuilt from scratch and **has since merged**, closing
  slice S3 and taking `dwrules.All()` to **six** rules. **DW-020 (partitioning) has
  since been built too** — a schema-level census over the `db.Table.Partitioning`
  floor, closing slice S4 and taking `dwrules.All()` to **seven** rules, with no
  DW-0xx rule left unbuilt. Both shipped in **`v0.2.5`** — this bullet is history,
  not current status.
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
  since moved, past this tag and shipped in **`v0.2.5`**: `db.Index.Method`
  landed — see the `alpha.2` entry above — the `PARTITION BY`/`PARTITION OF`/T-SQL
  partition-scheme capture DW-020 was waiting on landed too (`db.Table.Partitioning`),
  **and the DW-020 rule that reads it has since been built as well**, and the role
  vocabulary now recognizes
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
