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

## [0.2.6] — 2026-08-03

**A PATCH release, and the smallest kind: no PRD phase closes here.** Phase 3 (code
review, best practices, tests, regression risk) has not started, so this stays on the
`0.2` line — see [VERSIONING.md](VERSIONING.md). **No audit rule changed.** Every
security, DB and DW rule behaves exactly as it did in `v0.2.5`: no finding, no surface
item and no baseline fingerprint moves. What lands is one **agent-facing** correction —
the skill `codefit init` generates was teaching a codefit two phases old — and the
portability work that makes `go test ./...` pass on a Windows checkout of this
repository.

**The skill `codefit init` generates had fallen two phases behind the MCP server.** It
described only endpoint security — the Phase-1 scope — while the database dimension
shipped across `v0.2.0`–`v0.2.5`, so it never named `codefit-scan-db`, `codefit-coverage`
or `codefit-check-cves`. The consequential half was the frontmatter `description`, which
**gates progressive disclosure**: with trigger words naming only "API endpoints or route
handlers", an agent starting a database task never loaded the skill at all — it did not
read an incomplete skill, it read none. **This reaches an existing install only by
re-running `codefit init`**: after upgrading, `init` regenerates the skill with the
database content, and a skill file already written stays stale until it is regenerated.

**`go test ./...` did not pass on a Windows checkout.** Eleven tests across `internal/cli`
and `internal/providers/sqlddl` failed on a clean clone of `main`, while the identical tree
was green on Linux CI. None of it was a fault in what codefit *audits*: the causes were a
missing `.gitattributes`, path separators, and a missing `.exe`. Two were test defects and
are not listed below; the separator one was not, and is the first `Fixed` entry.

**Three things to do after upgrading**, each with its own entry below:

1. **Re-run `codefit init`.** It is the only way the skill fix reaches an existing
   install — a skill file already on disk stays stale until it is regenerated.
2. **In an existing checkout of this repository, run `git add --renormalize .` once**
   (or take a fresh clone). `.gitattributes` does not apply retroactively.
3. **Rebuild if you built with `make build` on Windows.** It wrote `bin/codefit` with no
   suffix — a file Windows will not resolve as an executable.

### Fixed

- **The skill's frontmatter `description` now triggers on database and schema work too**,
  not only on endpoints and route handlers. The trigger set is part of the contract, not
  decoration — it decides whether the skill is loaded at all — so it is locked by its own
  test rather than left to prose.
- **A new "Audit the database schema" section teaches `codefit-scan-db`**: that it reads a
  Prisma `schema.prisma` **or** a directory of SQL-DDL/Flyway migrations (PostgreSQL,
  MySQL, SQL Server), that it classifies the schema as transactional or warehouse and adds
  the star-schema/SCD, columnar-index and partitioning checks on a warehouse, and that
  `codefit-scan-all` runs it **only** when `database.schema_paths` is set in `.codefit.yaml`.
  It states the honest-abstention contract in the agent's own terms: a table with no primary
  key is deterministic, **everything else is surface** (foreign keys with no covering index,
  duplicate/redundant indexes, sensitive columns in the clear, risks in procedure/trigger
  bodies), and **`measured: false` is NOT a clean result** — it is codefit saying it could
  not read the schema. The section renders **unconditionally**, which is a decision and not
  an oversight: `Detect()` recognizes a database only through `enrichTypeScript`
  (Prisma/Drizzle/TypeORM) and does not detect SQL-DDL/Flyway at all, so gating the section
  on detection would hide the dimension on exactly the projects the SQL-DDL parser was
  built for.
- **A new "Dependencies and declared limits" section teaches `codefit-check-cves` and
  `codefit-coverage`.** `codefit-scan-all` does **not** run the CVE check, so an agent that
  knew only `scan-all` had no path to it. `codefit-coverage` is how the agent learns what
  codefit does and does not audit before telling a human something is out of scope, instead
  of assuming the boundary.
- **`codefit init` now reports paths with forward slashes on every OS.** On Windows the
  report printed `.claude\skills\codefit\SKILL.md` and `prisma\schema.prisma` while writing
  `prisma/schema.prisma` into the `.codefit.yaml` it generated **in the same run** — the
  report and the config disagreed about the spelling of the same file. Slash is codefit's
  canonical spelling for a path it emits: `RenderConfig` documents that paths are
  slash-normalized "so the committed file is portable across operating systems", and every
  path codefit puts in a finding is `filepath.ToSlash`'d before an agent sees it. This was a
  **product** defect, not a test expectation: the test already fed native separators and
  already asserted forward slashes, but `filepath.FromSlash` is the identity on Linux, so
  the contract was written down and never actually exercised.
- **`make build` produces a runnable binary on Windows.** It wrote `bin/codefit` with no
  suffix; `go build -o` appends nothing, and Windows resolves executables by extension. The
  suffix now comes from `go env GOEXE`, so it stays empty on Linux and macOS.
  `make cross-compile` is unchanged — its targets are per-`GOOS` and already spell their own.
  **Declared, not fixed:** under a *native* Windows `make` driving `cmd.exe`, the
  `$(shell date -u …)` and `$(shell git rev-parse … 2>/dev/null)` recipes still misbehave,
  so `Commit` and `BuildDate` can be injected as garbage. Which `make` and which shell a
  developer has is not observable from here, and rewriting those recipes blind would be an
  unverifiable change; building from Git Bash — where this was verified — is unaffected.

### Added

- **A drift lock between the two agent-facing sources** — `TestSkillNamesEveryRegisteredTool`
  (`internal/mcp/skill_tools_test.go`) reads the tool names from a **live** MCP session
  rather than from the constant block (the constants already declare Phase-3 names
  `NewServer` does not register), and fails unless every registered tool is either named in
  the rendered skill or carries a declared reason in the `deliberatelyNotInSkill` allowlist.
  The skill is deliberately thin, so omitting a tool stays a legitimate choice — it just has
  to be a **declared** one. `TestSkillOmissionAllowlistHasNoGhosts` guards the reverse
  direction: an entry for a tool the server no longer registers would silently excuse a
  future tool that reuses the name. The lock forces the **decision**, not the content — what
  the skill teaches still has to be verified true. Both tests were mutation-proven.

- **The skill `codefit init` generates is now committed** at
  `.claude/skills/codefit/SKILL.md` — so the copy in git is a promise about what the
  generator produces *today* — and it lands **with its lock, never before it**.
  `TestCommittedSkillMatchesRenderedSkill` (`internal/scaffold/skill_committed_test.go`)
  renders through the **real** pipeline (`Detect()` on the repository root, then
  `RenderSkill()`) and asserts the committed bytes are identical to the render; a second
  test asserts the committed path is still where `PlacementTargets` says `init` writes it.
  A missing file **fails** rather than skipping, because a skipped test protects nothing.
  Without that lock a committed generated artifact is one more mirror free to drift from
  its source — which is precisely what the skill drift above *was*. Mutation-proven: a
  stray appended line, and a template edit left unregenerated, each turn it red.
- **The repository's own `.gitignore` now covers `**/.claude/settings.local.json`.** That
  file holds per-machine agent settings — tool permissions, absolute local paths — and was
  never tracked, but the only thing keeping it out was a rule in the developer's
  **personal** global gitignore; on any other machine `git add .claude/` would have
  committed it. Verified with the global excludes file disabled. The generated skill beside
  it is deliberately **not** ignored, and the rule's comment now records that as settled
  rather than describing it as an open question.
- **A `.gitattributes` pinning LF.** The repo had none, so on a checkout with
  `core.autocrlf=true` every tracked text file materialized with CRLF — 485 of 487 files on
  the machine this was found on — and codefit's fixtures are parsed and compared as **bytes**.
  Nine `sqlddl` tests failed as a result: statement terminators went uncounted, `ALTER TABLE`
  statements stopped reducing, and `StructureProven()` flipped to false. `gofmt` also
  considered all 337 Go files unformatted; after the fix, one (which is behind a build tag
  and outside the lint run). The stored blobs were already LF, so this changes checkout
  behaviour only and produced no content diff.

  **This does not repair a checkout that already exists.** Run `git add --renormalize .`
  once, or take a fresh clone.

### Changed

- **`internal/scaffold/skill.go` is now registered in the `CLAUDE.md` documentation map** as
  an agent-facing source, alongside `internal/mcp/server.go` — and as the *first* thing an
  agent reads, since its `description` decides whether the tool descriptions are ever
  reached. A doc that declares capability and is not in the map falls out of the
  documentation close, which is exactly how this one drifted for two phases.

## [0.2.5] — 2026-08-02

**Phase 2.5 complete — RF-03 OLAP closure, and with it the last Phase-2 coverage debt.**
The pieces below close the phase; the earlier `v0.2.5-alpha.*` entries cover the rest of
it (paradigm and table-role detection, 3NF-suppression on OLAP tables, the star-schema
and slowly-changing-dimension family DW-001/002/005/010/011, and the neutral model's
structural completeness contract). What lands here is the **columnar/analytic index**
check **DW-021** and the **fact-table partitioning census** **DW-020**, each with the
parser floor it reads (`db.Index.Method`, `db.Table.Partitioning`) — taking
`dwrules.All()` to **seven** rules with **no DW-0xx rule left unbuilt**. DW-022
(materialized views without refresh) stays permanently dropped: refresh cadence lives in
scheduler state that static DDL does not carry.

**Not in the plan, and the phase's most consequential structural change: the schema
gate** (ADR **0037**, which inverts ADR 0033). The schema is judged *before* any table is
given a warehouse role, so one table named `dim_status` can no longer silence its own
DB-002/DB-003 1NF findings inside an otherwise transactional schema. The verdict is
**measured, not reasoned**: of six schema-wide signals computed over 26 public corpora,
the three that measured zero false positives are the three that vote. ADRs **0035–0047**.

**The PRD's Phase-2 acceptance criterion is met.** The PRD asks that `codefit-scan-db`
produce *verified real findings on a real project*. Measured through the real handler
over an untouched UTF-16LE `pg_dump` of a production Postgres backend: `measured: true`,
9 tables, structure proven for 9 of 9, **12 surface items and 0 deterministic findings**,
paradigm `oltp`. Every one of the 12 was hand-verified against the DDL — **0 false
positives** — and the run holds a verified *true negative*: the schema declares 11
foreign keys, the FK rule fires on 10 and stays correctly silent on the eleventh, whose
column a `UNIQUE` constraint already covers. Before the encoding fix below, that same
file audited as `measured: true`, **score 100, empty note, 0 tables** — a silent false
all-clear.

**Read that number honestly** — four caveats, stated rather than smoothed over:

- That project exercises a **narrow slice**: 9 tables, no views, procedures or triggers,
  nothing analytic, so only **3 of the 21 DB/DW rule families fired at all**. Breadth is
  evidenced by the 26-corpus survey, not by this one project.
- Its `score` is **100** *alongside* those 12 items. That is correct by design — surface
  is a question for the agent, so it is never scored — but it reads as "clean" to anyone
  who looks at the score first.
- **PII coverage is partial and an open design question, not a settled exclusion.** A
  column named `email` does not fire DB-053, because *"is this secret in plaintext?"* is
  not a question an email address answers — yet `ssn`, `dni` and `creditcard` are already
  in that same vocabulary, so the boundary is not clean today. A separate personal-data
  category is a candidate, **not decided and not scheduled**.
- **⚠️ BREAKING for committed baselines.** Anchoring `CREATE TABLE` body items on their
  own source line changes the fingerprint of every column-anchored DB item (DB-002,
  DB-003, DB-051, DB-053), so existing `.codefit-baseline` entries for those categories
  re-appear as `new` on the first scan after upgrading, until they are re-accepted. See
  the anchoring entry under *Fixed*.

### Added

- **DW-020 — this schema's fact tables, censused for declared table partitioning**, the
  seventh rule in `dwrules.All()` and the close of slice S4. **The DW-0xx family is now
  complete: no OLAP rule remains unbuilt.** It emits **one item for the whole schema**,
  never one per table — a decision made from a measurement, not from taste: across the
  26-corpus survey, no analytic corpus declares table partitioning at all, so a per-table
  rule would fire on essentially every fact table of every warehouse. **Surface, never an
  affirmation** (ADR 0017): whether partitioning is worth anything depends on each table's
  real row count, growth rate and retention policy — runtime facts absent from static DDL —
  so codefit hands over the census and the question, never a verdict. It fires when at
  least one fact table declares no partitioning, **including the mixed case**, where the
  already-partitioned siblings are named as a fact rather than used to suppress: letting
  one partitioned fact table mute the question for the rest would be a silent false
  negative. **Declared partition children are excluded from the census** — a partition is
  not a fact table, and counting a `PARTITION OF` child would restate one partitioned fact
  as two — and they are **exempt from the completeness gate** for the same reason: a child
  is unproven *by construction*, so gating on it would abstain the rule on exactly the
  warehouses that do partition. With the schema gate closed, no table holds a warehouse
  role, the census is empty and the rule says nothing. Every fire and non-fire path is
  proven through the **real parser and the real classifier** on genuine PostgreSQL DDL.
  Measured over 26 public corpora: **8 emit exactly one item each**, covering 16 fact
  tables between them — full AdventureWorksDW's 8 fact tables collapse into **one**
  question instead of 8. Zero of the analytic corpora partition a fact table; that zero is
  positively controlled against four transactional corpora that genuinely do partition,
  and against the `OVER (PARTITION BY …)` window-function false positive a naive grep
  would have produced. Known limit, inherited from the model: an `ATTACH PARTITION` child
  (what `pg_dump` emits) carries no back-reference and is indistinguishable from an
  ordinary table.
- **The SQL-DDL reducer now reads table partitioning** into a new neutral
  `db.Table.Partitioning`, the parser floor DW-020 was waiting on and now reads. Per
  dialect: PostgreSQL and
  MySQL `PARTITION BY <strategy> (<key>)`, PostgreSQL's `PARTITION OF <parent>` child, and
  T-SQL's `ON <partition scheme> (<column>)` — the last resolving its strategy word through
  the scheme's own `CREATE PARTITION FUNCTION` when that statement is in the DDL read, and
  never defaulting when it is not. A partition **child** is modelled as its own table plus
  a back-reference to its parent, and is marked structurally unproven (a new
  `db.ReasonPartitionChildInheritsStructure`): that statement declares the child's bounds
  and nothing else, so its columns and keys live on the parent. Before this change the
  child statement matched **no dispatch branch at all** and the entire table vanished from
  the model without a trace.
- Two fabrication guards come with that read, both mutation-proven. An **expression**
  partition key (`PARTITION BY RANGE (YEAR(sold_on))`) leaves `Partitioning.Key` empty and
  reports the clause verbatim instead — running it through the ordinary column-list
  splitter invents the column `YEAR("sold_on")`, which exists in no table and which
  `db.Table.Complete` cannot catch (it covers drops, not fabrications). And the tail is
  searched at **top level only**, outside parens and string literals: `PARTITION BY` is
  also window-function syntax, and `CREATE TABLE s (a, b) AS SELECT … OVER (PARTITION BY
  c)` is valid PostgreSQL that this very path dispatches. T-SQL's `ON [PRIMARY]` filegroup
  clause — which all three vendored AdventureWorksDW tables carry — is likewise never read
  as a partition scheme; only the parenthesized column distinguishes the two.
- Reading partitioning **never demotes a table**. Measured through the real parser across
  all 17 vendored corpora: table counts and structure-proven counts are identical before
  and after. No vendored corpus declares table partitioning at all (every `PARTITION`
  under `testdata/` is inside a comment), so the read is proven by constructed DDL plus
  real-corpus negative controls.

- **DW-021 — a fact table with no columnar/analytic index**, the sixth rule in
  `dwrules.All()` and the close of slice S3. A fact-role table with no index using a
  recognized columnar/analytic access method is emitted as **surface, never an
  affirmation** (ADR 0017): whether the absence matters depends on the table's real size
  and query pattern, which codefit cannot see from static DDL. The vocabulary lives in
  exactly one place (`dwrules.columnarIndexMethods`) and the agent-facing prose is derived
  from that same map rather than restated: PostgreSQL contributes `brin`, T-SQL
  contributes `columnstore`, MySQL contributes **nothing** (its only methods, `btree` and
  `hash`, are ordinary row-store methods). PostgreSQL's `gin`/`gist`/`spgist` are
  **deliberately excluded** — they are specialized lookup structures, not column-store or
  analytic-scan ones, and admitting one while rejecting its two siblings would have been
  an arbitrary cut. Gated per table on `db.Table.StructureProven()` (ADR 0034), which is
  the whole mechanism that makes the rule dialect-agnostic: any statement that *could*
  have declared the very columnar index the rule asks about already marks its table
  incomplete, so the single gate abstains with no per-dialect branch in `dw021.go`.
  **Prisma is not zero-value here**, unlike its DW-001/002/005/010/011 siblings:
  `@@index([col], type: Brin)` genuinely suppresses the rule, proven end to end through the
  real Prisma provider. **Measured yield:** across 22 real public corpora (463 tables, 427
  FKs, 771 indexes) DW-021 fires unmodified on **3** — the only DW-0xx rule that fires on
  unmodified real third-party DDL. It also fires on the vendored AdventureWorksDW
  (`FactInternetSales`, one item), which declares no index beyond its primary key.
- **The neutral index model carries its declared access method** (`db.Index.Method`), the
  parser floor DW-021 reads. Captured, lowercased at every site for one convention across
  dialects: PostgreSQL's `USING <method>` before the column list; MySQL's `USING
  BTREE|HASH` in the **different** post-column-list position, and the same clause on the
  **inline** and `ALTER TABLE ADD` table-constraint forms in either grammar position;
  T-SQL's `CLUSTERED`/`NONCLUSTERED` ordinary-index kind; and T-SQL's
  `CREATE [CLUSTERED] COLUMNSTORE INDEX`, parsed by its **own** dedicated regex (a
  genuinely different statement shape carrying no column list) and captured as
  `columnstore` with `Columns` left **empty**, never synthesized — that statement names no
  column in its own grammar, and inventing one would misrepresent the source. Prisma's
  `@@index(..., type: X)` is captured the same way, verbatim and only lowercased,
  deliberately **not** validated against any codefit-maintained vocabulary. **Empty means
  "no access method declared in source"** and is never defaulted to a guessed `btree`.
  **Declared boundary:** a PostgreSQL **expression** index (`ON t (lower(email))`) stays
  out of scope — making the index name optional widened the grammar enough to also match
  an expression index's outer parens, which without a guard would have truncated at the
  first nested `)` and **fabricated** a phantom column from the truncated expression text.
  The column-list span is now verified against `balancedParen`, and a mismatch routes the
  statement to honest abstention rather than silent fabrication.
- **`CREATE INDEX`-family shapes the dispatch cannot recognize are now recorded** instead
  of being dropped without trace, so the completeness contract (ADR 0034) marks the
  affected table unproven rather than letting an absence-based rule conclude from parser
  silence. This closed the standalone `FULLTEXT`/`SPATIAL`/`XML`/`PRIMARY XML` forms and
  PostgreSQL's `ON ONLY` clause, and corrected a **wrong reason** previously attributed to
  phantom tables — a mis-attributed reason is its own dishonesty, since the note is what
  the agent reads to decide what was not measured.

### Fixed

- **DB-052 stops asking for audit timestamps from tables that have them** (ADR 0046 for
  the shared seam, **ADR 0047** for the rule). It compared a column name against exactly
  `createdAt`/`updatedAt`, so an append-only event table whose creation time is a column
  literally named `"timestamp"` was reported as untracked — with that very column listed
  in the item's own `columns:` signal. What counts as a stamp is now **one shared
  definition** (`db.IsAuditTimestampColumn`) that DB-052 asks per table and the schema
  gate's `no_audit_timestamps` signal asks per schema, and it is a **rule, not a list**:
  a creation/modification **verb** (`create`/`created`/`creation`,
  `insert`/`inserted`/`insertion`, `add`/`added`, `update`/`updated`,
  `modify`/`modified`, `change`/`changed`), a **time affix** attached to it (the suffixes
  `_at`, `_on`, `_ts`, `_time`, `_date`, `_datetime`, `_timestamp`, or the prefixes
  `last_` and `date_`), and a **type that can hold a time** (`datetime`, or `int` for the
  epoch `BIGINT` stamps Synapse really uses). A bare `timestamp` is the one explicit
  entry, since it carries no verb.
  **A suffix alone is deliberately not enough**: across the 29 measured corpora, 80
  distinct columns end in `At` and **74 of them are business event times** (`expires_at`,
  `started_at`, `finished_at`, `last_sync_at`, `paidAt`, `bannedAt`), and a table whose
  only time column is `expires_at` genuinely does not record when its row was created —
  admitting the suffix would silence it. The affix is load-bearing the other way too:
  `created_by` is a creation verb naming a **person**, so `_by` is not a time affix.
  **Measured over 29 corpora: DB-052 goes from 424 items to 375** — 49 tables silenced,
  **zero** newly firing, every other rule's per-corpus counts unchanged — and on the
  reporting project from 3 of 9 tables to 1 of 9, the one that genuinely has no time
  column at all. Matching decomposes the **whole** normalized name with nothing left
  over, so `creator`, `update_trace_id`, `commission_created`, `ts_added_ms` and
  `dv_create_date` (real columns of firing tables) are not admitted, and it never reads a
  **type as a name**: a column named `logged_value` typed `timestamp` still fires.
  Known limits, chosen deliberately in the visible direction: a stamp with no verb
  (`recorded_at`), a **prefixed** one (`dv_create_date`), or one typed in a way no corpus
  produced (a `created_at` declared `VARCHAR`) still fires — admitting one **silences** a
  table, and a false negative is the error nobody sees. Consequence for the schema gate,
  verified per corpus rather than assumed: six corpora stop firing `no_audit_timestamps`
  (it goes from 9 W / 5 O to 8 W / 3 O over the ADR 0036 set), and **not one paradigm
  verdict moves** — the signal casts no vote.
- **A `CREATE SEQUENCE` no longer becomes a phantom table** (ADR 0045, SQL-DDL known
  limit (14)). `pg_dump` writes one sequence per serial/identity column and then, for
  every relation it dumps, an ownership statement spelled with `ALTER TABLE` — which
  PostgreSQL legally accepts for every relation kind. The reducer had **no branch for
  `CREATE SEQUENCE` at all**, so when `ALTER TABLE public.<name>_id_seq OWNER TO
  postgres` arrived the name was unknown and a table was materialized from it: zero
  columns, structurally unproven, and a routed `db-table-structure-unproven` surface
  item asking the agent whether a **sequence** declares a primary key. **Measured
  through the real DB sensor on a real Spring/Hibernate `pg_dump`: 9 sequences became
  9 phantom tables — 9 of that run's 23 surface items** — and the per-scan note
  described them as "9 table(s)" codefit could not read. The same mechanism ran for
  **views and materialized views**: on the vendored Pagila corpus, 21 of its 23
  unproven "tables" were 13 sequences, 7 views and 1 materialized view. The reducer
  now recognizes `CREATE SEQUENCE` and remembers the **name** of every sequence and
  every view it reads — reducer-internally, with no model surface of its own — and
  the four branches that can create a table from a *reference* rather than a
  *declaration* consult one predicate before doing so. A sequence declaration itself
  stays a silent declared skip: it is neither unreadable (`Schema.Unreduced`) nor a
  withheld table (`Schema.Withheld`). **A genuinely declared table whose `CREATE
  TABLE` this scan never read still materializes with `ReasonTableNeverDeclared`** —
  the simpler rule "`OWNER TO` never creates a table" was evaluated and rejected
  because it would have deleted those. Measured over 26 external corpora, both
  directions: exactly two corpora move, **23 items removed, zero added**, everything
  else identical — a zero proven sensitive with a positive control build that
  reproduces all 23.
- **Every `CREATE TABLE` body item is now anchored on its own source line** (ADR 0045,
  SQL-DDL known limit (15)). The reducer counted newlines up to the **comma
  boundary**, which sits before the newline preceding the item's text, so every column
  and every table-level constraint pointed one line early — and so did the second and
  later actions of a multi-action `ALTER TABLE`. **Measured on a real `pg_dump`:
  DB-053 reported `password` at line 33, whose content is
  `lastname character varying(255),`** — an unrelated column quoted back to the agent.
  **BREAKING for committed baselines:** the baseline fingerprint is hashed from the
  *content* of the line at the anchor, so **every DB finding or surface item anchored
  on a `CREATE TABLE` body item changes fingerprint and a committed baseline entry for
  one stops matching** — the finding reappears as new until it is re-accepted. Items
  anchored on a table's own `CREATE TABLE` line (DB-052) or on a single-action `ALTER
  TABLE` (DB-001's foreign keys, the shape `pg_dump` writes) are byte-identical: on
  the real dump 13 of 14 surviving fingerprints are unchanged. Column-anchored rules
  — DB-002, DB-003, DB-051, DB-053 — are the ones that move. The three schema
  goldens were regenerated: **64 values changed, every one a `Pos.Line` going N →
  N+1, no other field touched**, and the new numbers are locked against the source
  rather than against themselves (every column of every `.sql` corpus under
  `testdata/` must sit on a line containing its own name — 195 anchors, 22 corpora).
- **A schema file codefit cannot read is no longer reported as a clean one** (ADR 0044).
  A PostgreSQL dump produced by `pg_dump` under PowerShell is **UTF-16LE with a
  byte-order mark** — not exotic, just what the tool writes when its output is
  redirected on Windows. **Measured through the real DB sensor**, a dump declaring
  **9 tables, 9 primary keys and 11 foreign keys** audited as `Measured=true`,
  **score 100**, **empty note**, **0 tables, 0 findings, 0 surface items**. No
  abstention floor fired, because nothing was *dropped*: the tokenizer saw
  `C\x00R\x00E\x00A\x00T\x00E\x00` and never recognized a statement at all, so every
  carrier ADR 0034 and ADR 0043 built stayed empty and the output was
  **indistinguishable from a clean bill of health**. The same four-row probe now
  reads 18 tables for the file as it sits on disk, identical to its UTF-8 conversion.
  - **What is decoded:** the three byte-order-marked encodings — UTF-8 (`EF BB BF`),
    UTF-16LE (`FF FE`), UTF-16BE (`FE FF`) — at the filesystem boundary, before any
    tokenizer sees the bytes. No new dependency: `unicode/utf16` from the standard
    library (`golang.org/x/text` is pure Go and would have been admissible, but it is
    not currently a dependency — checked in both `go.mod` and `go.sum`).
  - **What is never guessed:** a file with **no** mark is returned byte-identical.
    Sniffing BOM-less UTF-16 means inferring an encoding from NUL bytes at regular
    offsets, and a wrong inference silently rewrites a Latin-1 or binary-ish file —
    a corruption strictly worse than the silence, and undetectable downstream. Such a
    file is **declared unread** instead, on the positive observation that NUL bytes
    survived decoding.
  - **What makes the silence impossible** — the durable half, and the reason this is
    not just an encoding fix. A configured schema source that contributes **nothing**
    to the neutral model (no table, view, routine or trigger, and not even a statement
    recorded on `Schema.Unreduced`/`Schema.Withheld`) is now reported in the scan note,
    **whatever the cause** — a future format, a truncated file, an encoding nobody has
    written a branch for. Written against the **outcome**, not a list of encodings, so
    it closes the class rather than the cases. When **every** configured source is
    unread that way, the scan reports `Measured=false` and the db dimension **drops out
    of the weighted score** instead of contributing a fake 100.
  - A file codefit read and **declared** it could not reduce is explicitly *not* unread:
    an `Unreduced`/`Withheld` record is proof the file was read, and re-reporting it as
    blindness would undo exactly what ADR 0034 and 0043 built.
  - A genuinely **empty or comment-only** file is legitimate, does not change
    `Measured`, and is still recorded in its own wording — so "0 tables" is never
    ambiguous.
  - **The same blindness, probed rather than assumed, in two more readers**, both now
    decoding: the **security sensor** (a UTF-16 `.ts` file scanned as **0 findings,
    score 100**, where its UTF-8 twin reports SEC-001 at score 90) and the
    **code×schema cross extractor**. Probed and found **not** in the class, so
    untouched: `.codefit.yaml` loading (`yaml.v3` handles marks itself and fails loudly
    without one) and Prisma provider detection (degrades to a default, not to an
    all-clear). **Declared residual:** the security dimension and the cross have no
    unread-source floor — they walk a whole repository, where a file yielding nothing is
    the ordinary case — so a BOM-less UTF-16 *source* file stays silently unread there.
  - **28 vendored corpora, zero delta** — tables, proven counts, columns, keys,
    indexes, views, routines, triggers, every emitted item and the whole scan note are
    identical before and after; the measurement was proven sensitive by positive
    control. **No corpus could have caught this**, which is its own finding: all 28
    of them were UTF-8 with no mark. Three authored twins under `internal/sensors/db/testdata/` are
    the only control.
- **A `CREATE … TABLE` head no branch can reduce no longer evaporates** — the
  `CREATE TABLE` family gets the honest-abstention floor `reIndexShapedHead` has
  given the `CREATE INDEX` family since ADR 0034, and it closes a **class**, not a
  list of forms (ADR 0043, SQL-DDL known limit (13)). **Measured through the real
  DB sensor:** a schema whose only statement was `CREATE UNLOGGED TABLE events (…)`
  audited as `Measured=true`, empty note, **0 tables, 0 findings, 0 surface** — the
  false *"audited, 0 findings"* state over DDL codefit never read, indistinguishable
  from a clean bill of health. **Twelve** forms were confirmed silent that way:
  PostgreSQL's `UNLOGGED`, `UNLOGGED … IF NOT EXISTS`, `TEMP`, `TEMPORARY`,
  `GLOBAL TEMPORARY`, `LOCAL TEMPORARY`; MySQL's `TEMPORARY`; T-SQL's `#Local` /
  `##Global` name prefixes; plus `CREATE FOREIGN TABLE`, `CREATE TABLE … AS SELECT`
  and a quoted name outside the reducer's identifier class. (`CREATE TABLE IF NOT
  EXISTS` was never affected.) Three **different** dispositions, because they are
  different facts: an **`UNLOGGED` table is now modeled** (it only skips the
  write-ahead log — ordinary persistent storage); a **temporary table is withheld**
  from the model with its own trace (it is dropped with its session, and admitting
  it would have DB-050 affirm "table without a primary key" over scratch space at
  confidence 1.0); **everything else table-shaped is declared** verbatim on
  `Schema.Unreduced` and reaches the agent through the per-scan note. Withholding
  gets a **separate carrier** (`Schema.Withheld`, a closed `WithheldReason`
  vocabulary distinct from the completeness `Reason` set) precisely because
  reporting it as "could not be reduced" would describe a scoping decision as a
  parser failure. The catcher **declares without guessing**: it never invents a
  table name out of a grammar it does not know, since a fabricated table is the one
  class the completeness contract structurally cannot catch. Its modifier window is
  bounded to **two words**, which is what keeps `CREATE TYPE x AS TABLE`,
  `CREATE STATISTICS s ON t` and `CREATE SCHEMA s CREATE TABLE …` out. Withholding
  is **never silent** — the per-scan note states the count, the reason and up to
  five names, bounded so 200 staged temporary tables are one line, not 200.
  **Measured over 29 corpora: zero delta** on tables, proven counts, columns, keys,
  indexes, views, routines, triggers, paradigm, items and notes; the three schema
  goldens gained one additive key. A zero delta is also what a broken harness
  produces, so sensitivity was proven by positive control (a three-word window moves
  `adventureworks-oltp-pg`; a mandatory `UNLOGGED` prefix moves 22 of 29). No corpus
  could have caught a regression here — the forms have zero top-level prevalence —
  so two authored fixtures are the only control, and both are registered in the
  schema-gate corpus table.
- **Two `CREATE TABLE` statements with no separator between them are now separated
  instead of silently collapsing to the first** — closing SQL-DDL known limit (9),
  the **last silent structural loss** in this parser (ADR 0041). T-SQL makes the
  statement terminator optional, so `CREATE TABLE a (…)` immediately followed by
  `CREATE TABLE b (…)` — no `;`, no `GO` — is valid input. The reducer read `a` and
  discarded everything after it **with no trace at all**: no `Schema.Unreduced`
  entry, no completeness note, and `a` still `StructureProven`. Blindness with no
  trace is the one outcome the completeness contract (ADR 0034) exists to prevent.
  The boundary is now **derived, not guessed**: a table's body is located with
  balanced parentheses — the primitive the column loop and the partitioning reader
  already trust — and only the **tail** after it is scanned, for a
  `CREATE`/`ALTER`/`DROP` keyword at paren depth 0, outside any string literal and
  outside any quoted identifier. Those three words and no others, because they are
  the only statement kinds that can affect table structure **and** the only ones
  that cannot legally appear in a tail: `WITH` and `SET` can (`WITH
  (autovacuum_enabled = off)`, `WITH (DATA_COMPRESSION = PAGE)`), so admitting them
  would cut a table's own options away from it. The remainder is dispatched as a
  statement in its own right, recursively, so a run of *N* tables recovers all *N*,
  each with its own source line — and a residual the boundary rule **found** but no
  dispatch branch reduces (`CREATE TYPE`, `CREATE SEQUENCE`, …) is recorded
  verbatim on `Schema.Unreduced` rather than dropped: nothing is recovered while
  anything detected is lost in silence. The host table is never demoted for it —
  its own body was read in full, so demoting it would be the false demotion ADR
  0034 §2.4 warns about — and the host is **truncated** at the boundary so it
  cannot read the *next* statement's `PARTITION BY` clause as its own.
  **Measured, both directions**: on a public warehouse script that declares 7
  tables and contains zero `;`, the parse goes from **1 table to 7** (41 columns, 7
  foreign keys, all structure-proven, line numbers verified statement by statement
  against the source). Across 26 public corpora **exactly one changes** — the other
  25 are identical on tables, structure-proven count, columns, foreign keys,
  indexes, views, procedures, triggers, paradigm, every emitted item and the scan
  note — and **no golden file was regenerated**, because none of the repository's
  own 18 fixtures contains a run-on tail under any of the three dialects. Each
  fabrication guard (string literal, quoted identifier, paren depth, word boundary)
  is locked by a test that was **mutation-proven to invent a phantom table** when
  its guard is removed. The lowercase `go` batch separator was investigated and is
  **not** the cause: `GO` recognition has always been case-insensitive, and `go` was
  already accepted — the run-on statements are separated by nothing at all.
- **A `CREATE TABLE` body item missing its comma before a table-level key constraint
  no longer fabricates a single-column key** — closing SQL-DDL known limit (12),
  declared earlier in this same unreleased cycle, and narrowing the fabrication
  boundary ADR 0034 §2.6 had left open (ADR 0042). `Profit INT` followed by
  `PRIMARY KEY(Car_sid, Date_from)` with no comma between them read the constraint
  as an **inline** key on `Profit`, so the declared composite key was replaced by a
  single-column one **while the table still reported `Complete=true`** — a
  **fabrication, not a drop**, which is exactly the class the completeness contract
  structurally cannot catch, because the reducer believes it succeeded. It is
  **delimiter-independent** (reproduced on an ordinary `;`-terminated statement
  under all three dialects) and therefore pre-existing; run-on separation only made
  it *reachable* on real DDL. The boundary is **decided by the grammar, not
  guessed**: in PostgreSQL, MySQL and T-SQL alike an inline `PRIMARY KEY`/`UNIQUE`
  takes **no bare parenthesized column list** (`WITH (…)`, `INCLUDE (…)`,
  `USING INDEX TABLESPACE`, T-SQL's `ON scheme (…)` always intervene) and
  `FOREIGN KEY (…)` is not a column-constraint form at all — so
  `<column definition> <head> (` has exactly **one** legal reading, and the item is
  cut there and both halves reduced, recursively. The host column is **not
  demoted** (nothing was dropped) and a residual whose column list cannot be read
  still falls to the existing honest-abstention floor. **Measured, both
  directions**, over 29 corpora — the 26-corpus external survey (which already
  includes verbatim copies of the vendored fixtures) plus three jobs covering
  every `.sql` corpus in this repository's own `testdata`: exactly **three**
  change, all in the same
  direction — `dw-kenap`'s `Fact_Reservation` from `[Profit]` to the **six**
  columns its DDL declares, and `dw-salesmart` / `dw-ssis-salesmart`'s `dim_date`
  from `[calendar_month_name]` to `[date_key]` / `[date_sk]` — removing **one false
  `DB-001` and two false `DW-002`** items. The other 26 are identical on tables,
  structure-proven count, columns, indexes, foreign keys, views, procedures,
  triggers, column raw types, paradigm, every emitted item and the scan note, and
  **no golden was regenerated**. That zero carries a positive control: with the
  `(` requirement removed from the head rule, **all 29** corpora change. Each
  fabrication guard (string literal, quoted identifier, paren depth, the
  `CONSTRAINT <name>` backward walk, the closed optional-token set) is locked by a
  test **mutation-proven to fabricate a key** when the guard is removed. A second
  fabrication of the same class was closed with it: a column's inline-constraint
  scans now read the modifier tail **masked to top level**, so a `COMMENT` string
  reading `PRIMARY KEY` no longer declares a key and one reading `NOT NULL` no
  longer marks the column non-nullable. **Still not covered, and declared:** MySQL's
  bare `KEY`/`INDEX` shorthand (a bare `KEY` is *also* a legal inline modifier, and
  cutting on it would fabricate on a column typed `key(10)`), `CHECK`/`EXCLUDE`
  (legal inline, and identical either way), `PRIMARY KEY USING BTREE (…)`,
  `UNIQUE NULLS NOT DISTINCT (…)`, and a missing comma between two plain columns —
  each locked as a characterization test so the limit stays machine-visible
  (ADR 0034 §2.7).
- **A delimited SQL type name (`[int]`, `` `int` ``, `"int"`) is now classified instead of
  falling back to `db.TypeUnknown`** (ADR 0040) — closing SQL-DDL known limit (8) and, with
  it, a **live false positive**. Microsoft's own generated scripts delimit the type of *every*
  column (`[CustomerKey] [int] IDENTITY(1,1) NOT NULL`), so on the vendored AdventureWorksDW
  corpus **all 74** parsed columns read as unknown, and **DW-002 claimed `DimCustomer` and
  `DimDate` have no surrogate key** over DDL where `CustomerKey` and `DateKey` are plainly
  single-column `[int]` primary keys — contradicting DW-002's own doc comment, which cites
  that exact column as the shape that must *not* fire. The fix is **dialect-free and one
  seam wide**: the tokenizer already canonicalizes every dialect's quoting to ANSI `"…"`
  before the reducer runs, so the column-type lookup unwraps *that* form (`typeLookupKey`)
  and all three dialects are closed at once — no bracket strip, no per-dialect branch, no
  new dialect datum. MySQL backticks and ANSI double quotes were **probed on `main` and
  confirmed to carry the same defect**, not assumed. `RawType` still carries the source
  spelling verbatim, `typeBase` is untouched so the `KEY`/`INDEX`-vs-column discriminator
  keeps reading a delimited token as an index *name*, and an **unrecognized keyword still
  falls back to `db.TypeUnknown`** — a delimited *spelling* now maps onto the same lookup
  key, the vocabulary was never widened. **Measured over 26 public corpora, both
  directions:** only DW-002 moved anywhere — **26 items to 8**, all 18 removals belonging to
  the two AdventureWorksDW corpora, **zero** items added by any rule, and the 8 survivors
  are text-keyed dimensions the rule should fire on. Unclassified columns: the vendored
  excerpt 74/74 → 0/74, the full upstream install script 359/359 → **6**/359 (`[sysname]`,
  `[xml]` — the honest fallback still working). The schema gate's `type_profile_split`
  signal now **reaches** both corpora rather than failing closed; **no gate verdict changed
  on any corpus**, and **no golden file changed** (all three golden fixtures write their
  types undelimited, which is why this stayed invisible).
- **DW-005 and DW-011 no longer go silent on a declaratively partitioned PostgreSQL
  warehouse.** A `CREATE TABLE c PARTITION OF p` child is marked structurally unproven
  *by construction* — its columns and keys are declared on the parent, so nothing was
  dropped and no parser failed — and both rules were gating their whole-rule
  completeness abstention on it. Adding one partition child to a star therefore made
  `dw-no-time-dimension` disappear from a schema that had emitted it a moment earlier,
  and a **dimension** partition child did the same to `dw-mixed-scd-strategies`. Both
  measured through the real parser and the real classifier, before and after, never a
  hand-built table. The gate is now scoped to each rule's own census **members** through
  one shared predicate per rule (the shape DW-020 already shipped with, ADR 0038, now
  the idiom for all three census rules — ADR 0039). A child is excluded from the
  censuses as well as the gate, and that half prevents a *new* wrong answer rather than
  fixing an old one: a child declares no columns, so counting one would have reported an
  SCD-1 dimension fabricated out of a partition. **A warehouse that partitions its
  calendar is still recognized**, by the parent's name, so the fix does not trade one
  false negative for a false claim. **No affirmation changed** — all three rules remain
  pure surface — and **not one of the 26 measured corpora changes output in either
  direction**: none declares a partition child that holds a warehouse role behind an
  open schema gate. That zero is positively controlled against constructed schemas that
  do, where the two items reappear.
- **ADR 0038's third declared limit now has a test.** A partitioned parent whose foreign
  keys live on its children (the PostgreSQL ≤ 10 pattern) has fan-out 0, loses its fact
  role to the role-corroboration gate, and is invisible to DW-020 — a real limit that
  lived only in prose in `dw020.go` and `dbcoverage.go`, "verified by direct probe".
  It is now locked through the real parser, with the schema gate, the parent's proven
  structure and zero fan-out, its demotion, and the child's fact role all asserted, so a
  change to role classification cannot make those two comments quietly false.
- **The SQL-DDL reducer now reads T-SQL's `ALTER TABLE … ADD CONSTRAINT` family**, the
  shapes Microsoft's own generated scripts use to declare keys: the
  `WITH CHECK` / `WITH NOCHECK` prefix, **any** whitespace run between `ADD` and
  `CONSTRAINT` (a newline, two spaces, a tab — the ADD item is dispatched on its leading
  keyword, so the separator no longer decides anything), and comma-chained constraint
  lists whose later items repeat no verb. The SSMS tails come with it: a
  `WITH (PAD_INDEX = OFF, …)` option list and an `ON [PRIMARY]` filegroup clause after
  the column list, plus the standalone `CHECK CONSTRAINT` / `NOCHECK CONSTRAINT`
  statement, now a declared recognized skip rather than a reason to demote the table.
  Measured on the vendored AdventureWorksDW excerpt: **3 tables, 3/3 structure-proven,
  3 primary keys and 8 foreign keys in the model**, zero routed
  `db-table-structure-unproven` items, and an empty completeness note — where before it
  was 3 tables, **0** structure-proven, every key invisible and every absence-based rule
  abstaining. This is what turns codefit's only genuine Kimball warehouse corpus into
  real end-to-end evidence for the DW family.
- **A constraint whose column list cannot be read no longer becomes a silently empty
  key.** A `PRIMARY KEY` / `UNIQUE` / `FOREIGN KEY` / `KEY` / `INDEX` whose parenthesized
  list is absent, unbalanced or empty now marks the table unproven (ADR 0034) instead of
  reducing to `PrimaryKey: []` — the exact input `DB-050` reads as "declares no primary
  key", which would have affirmed an absence the reducer merely failed to read.
- **A zero-column index is rendered honestly instead of as a bare `[]`.** A T-SQL
  `CLUSTERED COLUMNSTORE INDEX` legitimately carries no columns, and DB-001's
  `existing_indexes` signal printed it as the literal `[]` — indistinguishable from a
  rendering bug or from "no index at all", while hiding the `Method`, the one fact the
  agent needs to judge whether an ordered index is still warranted. It now reads
  `(covers all columns) method=<name>`. Index **coverage** semantics are unchanged: a
  columnstore still never satisfies an ordered-prefix lookup, so DB-001 correctly keeps
  asking about an uncovered FK on such a table — only the rendering was dishonest.
  Relatedly, `DB-011a` (exact-duplicate index) no longer keys duplicate detection on an
  empty column list, which would have reported two *different* zero-column indexes as
  "duplicates another index on the same columns `[]`" — a claim with no content.

### Changed

- **The schema decides before any table gets a warehouse role.** Detection was bottom-up: each
  table earned a role from its name plus local corroboration, and the schema's paradigm folded
  out of those roles. Because 3NF-suppression reads the **per-table** role, one table named
  `dim_status` with a single inbound foreign key could silence its own DB-002/DB-003 1NF
  findings inside an otherwise purely transactional schema — the schema got no vote. It votes
  first now: codefit evaluates six schema-wide warehouse signals **before** assigning any role,
  and a schema that does not qualify gets **no** fact/dimension/staging/mart role at all.
  **The verdict is measured, not reasoned** (26 public corpora, 13 analytic / 13 transactional):
  a schema is a warehouse iff **any one** of `calendar_table`, `surrogate_key_names` or
  `type_profile_split` fires — the three that measured 9/0, 3/0 and 4/0 warehouse-to-
  transactional, zero false positives, identifying 10 of 13 warehouses. Counting all six instead
  ("any 3") identifies only 6 at the same precision, because `bulk_load_shape` fired on nothing
  at all and `no_audit_timestamps`/`star_topology` are near coin flips on the transactional side
  (9/5 and 7/5). **What that zero rests on, stated rather than rounded up:** four of the 13
  corpora in the transactional column parse to **zero tables** (three vendor only
  views/procedures/triggers; `jaffle-shop-dbt`'s dbt models are `SELECT`s), and a zero-table
  schema sits below the 3-table floor and can never qualify by construction — so the zero is
  real but its evidence base is **9** corpora, not 13. On the other side, `tpch` is filed
  analytic while its schema is TPC-H's normalized order-entry model, which presents no
  dimensional evidence at all; excluding it, shape-based analytic recall is **10 of 12**, and
  the two genuine misses are `dw-barousse` and `dw-ngthao`. All six stay
  computed and **reported**; only three vote. Measured over the same 26 corpora, this changed
  exactly **one** of them — `dw-barousse`, a **warehouse**, whose calendar is spelled
  `dim_date_month` and so misses an already-declared limit of the calendar signal. **No
  transactional corpus was affected, and not one DB-002/DB-003 item changed state anywhere**:
  under `auto` the post-gate role map is always a subset of the pre-gate one, so suppression can
  only ever decrease. See ADR 0037.
- **`database.paradigm` outranks the schema gate, in one direction only.** An explicit `olap` or
  `mixed` is the developer asserting this **is** a warehouse, so it **restores** every role a
  closed gate withheld — otherwise the whole DW-0xx family would receive an empty role map and
  run zero warehouse rules over a schema the developer just declared to be one. An explicit
  `oltp` restores **nothing**: manufacturing a role there would overrule the developer in the one
  direction that silences findings.
- **The gate is never silent.** When it closes over a schema that names warehouse tables, the DB
  sensor's note states how many roles were withheld and from which (bounded), names the three
  deciding signals it looked for and did not find, and names `database.paradigm: olap` as the
  escape hatch — otherwise the only visible consequence is 1NF items that *would* have been
  suppressed simply appearing, which looks like nothing happened. When it opens, the note names
  **which** signals opened it, or says plainly that an explicit setting did. Both stay empty when
  the gate changed nothing.
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
- **DW-005 recognizes a time dimension by the same vocabulary, instead of its own copy.** The
  time-dimension **name** test now composes on the role-name vocabulary above — strip the
  recognized role token off either end of the table name, then check that what remains is
  exactly `date`, `time` or `calendar` — so `D_DATE`, `d_date`, `date_dim`, `DATE_DIM` and
  `DimCalendar` are recognized alongside the `dim_date`/`dim_time`/`dim_calendar` that already
  were. This closes a regression the widening above opened **within this same unreleased
  cycle**: DW-005 kept a second, hardcoded three-spelling list, so two real corpora spelling
  their calendar `D_DATE`/`D_Date` began classifying as dimensions while staying invisible to
  DW-005 — which then reported "this fact table reaches no time dimension" over schemas that
  plainly declare one. A silent miss had become a **confident false claim**, which is strictly
  worse. The remainder is matched by **equality, never containment**: separators are stripped
  before comparison, so a substring test for "date" would swallow `dim_update`/`dim_candidate`/
  `dim_validate` and silence the rule on a warehouse that genuinely has no calendar.
  **Declared limit kept:** a spelled-out or qualified calendar name (`date_dimension`,
  `dim_date_full`, `dim_fiscal_date`) is still not recognized by name — a miss, never a false
  claim. DW-011's time-dimension exclusion uses the same test, so the two cannot drift.
- **The vendored AdventureWorksDW corpus is reached end to end, as vendored.** Both of the
  limits that used to keep Microsoft's real DDL silent are now closed **in this same
  unreleased cycle**: the name vocabulary above recognizes its PascalCase Kimball spelling,
  and the reducer fix above puts its three primary keys and eight foreign keys in the model,
  so the corroboration gate finally has structure to corroborate. Measured on the vendored
  excerpt: paradigm `olap`, `FactInternetSales` → fact, `DimCustomer`/`DimDate` → dimension,
  and the DW family emits **3 items** (two `dw-dimension-no-surrogate-key`, one
  `dw-fact-no-columnar-index`) where it previously emitted none. The two limit-locks that
  guarded each half are replaced by one positive lock over the real corpus
  (`TestDW_AdventureWorksDW_StarIsVisible_AsVendored`); the declared snake_case rename those
  locks needed is gone, because the star is visible under Microsoft's own names.
- **What the two closures make visible is reported, not smoothed over.** On the same corpus
  the three `db-table-structure-unproven` items disappear (their cause is gone) and the
  previously-abstaining rules now speak: 8 `db-fk-no-index` and 3 `db-no-timestamps` surface
  items. In the other direction, its one real `db-repeating-groups` (1NF) item on
  `DimCustomer` (`AddressLine1`/`AddressLine2`) is now **withheld**: the table holds a
  dimension role, so 3NF suppression engages — an intentionally denormalized warehouse
  dimension is not a 1NF defect. That withholding is never silent; the sensor note states it
  and names the escape hatch: *"3NF-suppression withheld 1 1NF surface item (DB-002/DB-003)
  on 1 OLAP-classified table (fact/dimension/mart); set `database.paradigm: oltp` to see
  them."*
- **Two SQL-DDL parser limits, measured while doing the above, are now declared** in the
  coverage manifest instead of being left silent: a **bracketed T-SQL type name**
  (`[int]`) does not match the type vocabulary and falls back to `TypeUnknown` — which is
  why DW-002 fires on AdventureWorksDW's `DimCustomer` and `DimDate` despite their genuine
  integer surrogate keys; and **two `CREATE TABLE` statements with no `;`/`GO` between them**
  lose the second one entirely, with nothing recorded. Neither is fixed here.

### Internal — no behavior change

- **Schema gate, stage 1: five schema-wide warehouse signals, wired to nothing.**
  **Superseded within this same unreleased cycle** by "The schema decides before any table gets
  a warehouse role" under Changed, above: the gate is wired now, and none of the inertness this
  entry describes still holds. It is kept because the *reason* it was built inert is the reason
  the verdict could be selected from numbers. Paradigm
  detection worked bottom-up at the time, so one table named `dim_status` with fan-in ≥ 1 inside an
  otherwise transactional schema decided its own silencing of the DB-002/DB-003 1NF surface —
  the schema got no vote. `internal/core/paradigm` computes five independently-named
  signals over the whole schema (`calendar_table`, `surrogate_key_names`, `bulk_load_shape`,
  `no_audit_timestamps`, `star_topology`) as a first step toward inverting that. **Nothing called
  them at this point.** `Detect`, `Resolve` and the sensor's 3NF suppression behaved exactly as before, and two
  tests lock that inertness — an AST scan proving no production file references the gate, and a
  behavioral test proving `Detect` does not move on a schema where the gate fires. No new
  capability, nothing to use from `main`, and `COVERAGE.md` is deliberately untouched.
  **What the measurement says** (locked over every vendored corpus through the real parser, see
  [ADR 0035](docs/decisions/0035-schema-gate-stage-1-inert-signals.md)): when this stage was
  built, the one genuine warehouse in the repository, AdventureWorksDW, fired **one** signal
  while a three-table excerpt of Sakila — a rental shop — fired **two**, so a naive
  "≥ 2 means warehouse" threshold got both backwards. The T-SQL `ALTER TABLE … ADD CONSTRAINT`
  fix above then proved that corpus's three tables, `no_audit_timestamps` stopped abstaining,
  and the two now fire **two signals each** — so no threshold separates them at any cutoff.
  The counting argument survived its own re-measurement; only its shape changed. Publishing
  those numbers before wiring anything is the entire point of building this stage inert.

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

- **Role detection matched only a leading snake_case segment, at this release.** PascalCase
  Kimball naming (Microsoft's `FactInternetSales`, `DimCustomer`) classified as
  `unclassified`, so the DW family yielded **no value** on it. Test-locked, not silent.
  (Superseded after this tag — see the role-vocabulary entry under `0.2.5`.)
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
  (MIT) and yielded no DW finding, for two independent test-locked reasons at this
  release: its PascalCase names, and a pre-existing T-SQL reducer gap. (**Both halves
  were closed after this tag, and both shipped in `0.2.5`** — the naming one
  by the widened role vocabulary and the reducer one by the `ALTER TABLE … ADD
  CONSTRAINT` fix; see `0.2.5`.)

### Known issues

- **A pre-existing T-SQL reducer gap became visible** while vendoring AdventureWorksDW:
  three shapes of `ALTER TABLE … ADD CONSTRAINT` are dropped, so that script's three real
  primary keys and all eight real foreign keys never reach the model. The worst
  consequence is that **DB-050 — a deterministic affirmation at certainty 1.0 — reports
  three tables as having no primary key over DDL that plainly declares one for each.**
  Not introduced here and not fixed here; documented in the coverage manifest and locked
  by tests written to go red once the reducer is fixed. (**Fixed after this tag, shipped
  in `0.2.5`** — see that section. Those two limit-locks did go red and were replaced by a
  single positive lock over the real corpus, `TestDW_AdventureWorksDW_StarIsVisible_AsVendored`.)

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
