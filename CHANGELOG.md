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

### Added
- **⚠️ `scan-all`'s baseline delta now reports what agents already reasoned** — a
  response-shape change. Affirms [ADR 0081](docs/decisions/0081-agent-verdicts-persist-in-the-baseline-and-never-silence.md)
  (H4, slice 2 of 3, R4/R5). `baseline` gains `reasoned_by_agent` and
  `in_conflict_count` (both ALWAYS present: 0 means codefit looked and found none,
  never that it did not look), plus `reasoned_items`, `in_conflict`,
  `reasoned_withheld`, `in_conflict_withheld` and `agent_reasoning_note` (all
  omitted when empty, matching `gone_candidates` in the same block). Same kind of
  additive key change as `by_dimension`'s sixth key; tolerant consumers are
  unaffected.

  **Why the read-back matters more than it looks.** A surface item an agent has
  answered goes `known` on the next scan and stops appearing in the endpoint
  buckets — the baseline's own safeguard, which predates agent verdicts and which
  recording does not change in either direction. Without this delta the response
  gave an agent no way to learn that anything had ever been reasoned, so the next
  audit reasoned it again. This is the read half of the closing protocol; slice 1
  was the write half.

  `in_conflict` is a SEPARATE list, not a flag folded into the counts: one agent
  saying `vulnerable` while another says `not_vulnerable` is a question for a
  HUMAN, and averaging it into new/changed/known/acknowledged would bury exactly
  the thing that needs attention. Nothing here accepts anything — only
  `codefit-baseline-accept` does, and only a human runs it.

  Both lists are capped at 20 entries by construction, and `agent_reasoning_note`
  says whether you are holding a complete list or a prefix and how many were cut.
  The cap is measured, not chosen: a worst-case rendered item is 170 bytes, so both
  lists full cost 4,526 — 11.3% of the 40,000-byte budget. It is a build-time cap
  rather than the usual budget step because `fitToBudget` can only withhold
  endpoints from the three buckets and cannot see this block at all; an unbounded
  list here would push a response over budget with nothing the budget step is able
  to withhold. No reasoning PROSE is carried in the delta — `codefit-baseline-list`
  entries now carry it instead, truncated by the existing list-time idiom.

- **New tool `codefit-baseline-record-verdict` persists an agent's reasoning about a
  surface item across audits.** Affirms [ADR 0081](docs/decisions/0081-agent-verdicts-persist-in-the-baseline-and-never-silence.md)
  (H4, slice 1 of 3): until now `codefit-confirm-surface` validated a verdict and
  returned a probabilistic finding, but persisted nothing — its request carries no
  `root`, so the next audit reasoned the exact same items from zero. The new tool
  re-validates each verdict against a FRESH re-analysis before persisting (a verdict
  whose item no longer exists there is refused and named, never silently dropped;
  reasons: `surface_id_mismatch`, `no_surface_item_at_anchor`, `unknown_verdict`,
  `analysis_failed`), then appends it to `.codefit-baseline`'s new
  `items[].agent_verdicts` field.

  **Recording a verdict never ACCEPTS the item** — it creates no acknowledgement and
  moves the item's visibility in neither direction; only a human accepts, via the
  existing `codefit-baseline-accept`. Two agents disagreeing on the same item is not
  an error: both verdicts are kept and the item can be checked for conflict. The
  generated skill now teaches this tool and the safety discipline around it.

  ⚠️ **Cross-version note**: an older codefit binary does not know about
  `agent_verdicts` and silently drops it if it re-saves `.codefit-baseline` (ADR
  0081, D6) — recoverable via `git diff`/`git revert` since the baseline is
  committed, but real on repos shared across binary versions. `.codefit-baseline`'s
  own header now carries this warning, and unrecognized top-level fields
  round-trip through `Load`/`Save` unchanged so a FUTURE format addition does not
  repeat this for binaries that already have this fix.

- **`codefit-scan-all` and `codefit-scan-security` now declare which project-registered
  authz helpers codefit recognized for the language.** Affirms ADR
  [0013](docs/decisions/0013-custom-authz-helper-registry.md) (no new ADR): the registry
  and matching were already there, but a run showing many `known_authz_detected: false`
  items gave no way to tell "codefit never learned this project's helper" from "this
  project has no equivalent guard" — the exact ambiguity issue #155 named.

  Both responses gain `recognized_authz_helpers` (the exact names
  `baseline.RecognizedAuthzHelpers` returns for the language) and
  `recognized_authz_helpers_note`. On `codefit-scan-all` the pair sits under
  `security` and is present only when `security.measured` is true (absent, not
  empty, mirroring `security.surface_coverage`); on `codefit-scan-security` it is
  always present. The zero-registration case renders `[]`, never `null` — codefit
  looked at the baseline and found no registration, which is not the same as never
  having looked. The note states a fact about codefit's own knowledge ("codefit
  recognized no project-registered authorization helper for this language"), never
  a judgment about the project's authorization — a built-in helper match
  (e.g. NextAuth-style `getServerSession`) can still clear `known_authz_detected`
  with zero registered helpers, so the two counts make no claim about each other.
  Both tool descriptions and the generated skill now point the agent at the new
  fields before it reads `known_authz_detected: false` across many endpoints.

## [0.2.9] — 2026-08-18

### Changed
- ⚠️ **`db.surface` (in both `codefit-scan-all`'s `db` bucket and standalone
  `codefit-scan-db`) is now a light INDEX, not the full question. Its shape
  changed — read this if anything of yours reads `db.surface`.**
  (ADR [0080](docs/decisions/0080-db-surface-is-an-index-served-by-a-different-tool.md))

  Each item used to carry the full question: `snippet`, `structural_signals`,
  `reason_to_review`, `indirect_call`, plus `id`/`category`/`file`/`line`/
  `fingerprint`. It now carries only the light half —
  `{id, category, file, line, fingerprint, structural_facts}` — and the prose
  half is served on request. Measured over a real 5-item, 4-category corpus
  (Pagila excerpt, one sample — the ratio is structural, driven by field
  size, but the count is small): **609.6 B/item full → 182.6 B/item index,
  a 3.3x reduction.** `fingerprint` and `structural_facts` stay in the index
  on purpose: `fingerprint` because `codefit-baseline-accept` takes it
  directly, and `structural_facts` because it is the only filterable axis
  across db.surface's 18 disjoint categories (no severity field).

  **Nothing is withheld, ever.** Both sections gain `count` (the total
  classified) and `withheld` (always `0`, with `withheld_note` saying why:
  there is no ranking axis across db.surface's categories to withhold BY —
  a different reason than `codefit-coverage`'s, which authorizes nothing
  from a fixed manifest).

  **Full detail is one `codefit-scan-db` call away.** `{root, language,
  detail: [ids]}` returns each requested item byte for byte the old flat
  shape, mirrors `codefit-coverage`'s `detail` idiom. An id that matches
  nothing is named in `unrecognized` with a note — codefit is stateless and
  cannot tell whether the id never existed or the schema changed between
  calls, never an empty success. `codefit-scan-all`'s `db` bucket carries no
  detail of its own; fetch it through `codefit-scan-db`.

  **`codefit-scan-db` now declares its own response size**, wired together
  with `detail` (the shape that made `codefit-coverage` under-declare its
  size before it carried `bytes`/`index_bytes` — see the coverage entry
  below): `bytes` covers the index plus any detail asked for, measured LAST;
  `index_bytes` is the index's own share; a detail request large enough to
  cross the response budget says `over_budget: true` and still comes back
  complete.

  **This does not close the structural size problem** (roadmap P0-4's
  remaining half). At the measured 182.6 B/item, a 200-item db.surface is
  still ~36 KB on its own — over the 40,000-byte response budget with
  nothing yet to rank it by. What changes is how far this pushes the
  problem out: a real ~62-item dogfood case drops from ~38 KB (full) to
  ~11 KB (index).

- ⚠️ **`codefit-coverage` answers with an INDEX of named entries and serves full
  prose on request. Its response shape changed and 20 entry ids were renamed —
  read this if anything of yours reads that tool's output.**
  (ADR [0076](docs/decisions/0076-a-coverage-golden-locks-identity-not-prose.md),
  [0077](docs/decisions/0077-a-coverage-entry-is-keyed-by-the-rule-it-describes.md),
  [0078](docs/decisions/0078-a-coverage-answer-is-found-by-id-and-declares-the-size-of-what-it-returns.md))

  It used to return every declaration as full prose in one payload: **143,293
  bytes** for TypeScript, one string of it 34,080 characters — a serialized ADR
  inside a JSON list, large enough that an agent reading it had spent its context
  before it audited anything. It now returns `{id, claim, status, has_detail}`
  per entry and the full prose only for the ids you ask for:
  `{language: "typescript", detail: ["DB-050", "db.sqlddl-dialect-limits"]}`
  takes many ids in one call and returns each entry byte for byte as authored.

  **Nothing is withheld, ever.** The index carries every entry the manifest
  holds; `withheld` is `0` and a note says so in words, because silence and
  "nothing was dropped" are not the same bytes. The response budget authorizes
  withholding for `scan-all`; for coverage it authorizes nothing.

  **Every rule id is now an entry of its own.** A rule you saw in a finding is
  askable by name — `DB-011a`, `DB-053`, `DW-010` — where before it existed only
  *inside* another entry's prose: in the payload, but not in the index, so an
  agent could not name it and asking for it came back unrecognized. Five
  multi-rule prose blobs were cut into 21 entries and 17 single-subject entries
  were re-keyed to their rule id; the DB block went from 32 to 48 entries and the
  TypeScript index from 52 to 68. Every cut is a contiguous, gap-free partition
  of the blob it came from — nothing was paraphrased, summarized or invented, and
  that was proved by tiling the pre-change corpus rather than asserted.

  **BREAKING for anything that pinned an id.** 20 ids are GONE and 36 are NEW.
  No released tag carried the old ones (probed: `git tag --contains` on the first
  commit of the change is empty), so nothing in a release is affected.

  **An id that matches nothing is NAMED back to you**, never answered with an
  empty success: "there is no such entry" and "that entry has nothing to declare"
  are different answers.

  **The response declares the size of what it is actually returning.** `bytes`
  covers the index plus any detail you asked for and `index_bytes` is the index's
  share. A `detail` request big enough to cross the response budget says
  `over_budget: true` **and still comes back complete** — asking for all 68
  entries returns a 182,848-byte response that declares 182,152 bytes rather than
  reporting the index's 21,951 and calling itself within budget.

  Measured over a real client/server transport pair by the committed integration
  test: **143,293 B → 22,249 B** structured payload, 68 entries, 0 withheld.

- **`COVERAGE.md` carries each entry's id next to its full prose**, so a citation
  from an agent and a paragraph a human is reading can be pointed at each other.
  The prose is untouched — a human has no token limit. The correspondence is
  mechanically checked in both directions, and writing that check found two
  declared entries the mirror had never carried at all (the "assembled through an
  intermediate variable" halves of the SQL-injection and XSS splits, which two
  bullets pointed at with "is **surface** (below)"). Both are now mirrored.

- ⚠️ **SEC-001 (Go) now identifies a credential by NAME COMPONENT, not by raw
  substring and not by value length. Findings change in BOTH directions — read
  this before you re-baseline.** (ADR 0075)

  The old name gate had a second arm: any name containing `key` as a substring,
  with a value of 16+ bytes. Driving the security sensor over codefit's own tree
  through the real `go/ast` parser found **4 of its 5 name-gate findings were
  false** — enum constants and descriptive names reported at `Confidence: 1.0`
  as "looks like a hardcoded credential". The length guard was inverted in
  practice: descriptive kebab/snake values pass 16 bytes *because* they are
  descriptive, while a real credential has no length floor, so the gate admitted
  the false-positive class and rejected short real credentials
  (`SIGNING_KEY = "s3cr3t"` was 6 bytes and was rejected).

  **Fires that STOP**, measured on the real analyser over both trees. There are
  two groups and they stop for different reasons:

  - *Only when the value was 16+ bytes* — names carrying `key` as a substring or
    as a non-credential component, which never fired any other way: enum and
    category constants, and names such as `keyboard`, `keyword`, `monkeyId`,
    `textKey`, `publicKey`, `turnkey`, `donkey`, `sessionKey`.
  - *At ANY value length* — a credential word buried inside a longer lowercase
    run, where no boundary exists to tokenize on: `tokenizer`, and the
    all-lowercase concatenations `secretkey`, `dbpassword`, `mypassword`,
    `authtoken`, `clientsecret`. The delimited and camelCase spellings of those
    (`secret_key`, `db_password`, `myPassword`, `auth_token`, `clientSecret`)
    keep firing.

  PLURAL AND INFLECTED SPELLINGS ARE NOT IN THAT LIST. `passwords`, `secrets`,
  `tokens`, `apiKeys`, `apikeys`, `privateKeys`, `refreshTokens`, `mySecrets`
  and `userPasswords` fired before and fire after: the name gate folds the
  regular `+s` plural of every vocabulary entry. Component matching alone would
  have dropped all nine — eight of them at any value length — which is a
  narrowing nobody asked for and the false-positive fix never required.

  **Fires that START.** `pwd` and `pwds`, `accessKey`, `SIGNING_KEY`,
  `encryptionKey` (and their plurals), and every credential name whose value is
  shorter than 16 bytes. `API_KEY`, `api_key`, `apiKey`, `privateKey`,
  `accessToken` and `refreshToken` keep firing: the adjacent-pair join
  (`api`+`key` → `apikey`) is what made removing the old arm safe rather than a
  silent false negative, since `lower("API_KEY")` never contained `apikey`.

  **What to do with your baseline.** Fingerprints are unchanged for anything
  that keeps firing. A finding that stops leaves a STALE baseline entry, which
  is harmless — clear it with **`codefit-baseline-prune`**. A finding that
  starts is NEW and will appear as such.

  SEC-050 adopts the same matching convention over its OWN crypto-material
  vocabulary — it was strictly looser, with no guard at all, so
  `monkeyIndex := rand.Intn(n)` used to fire. `nonce`, `salt`, `iv`, `session`
  and a bare `key` component still fire there, because SEC-050 additionally
  requires a `math/rand` call.

  **Declared limit, now readable from `codefit-coverage`:** component matching
  cannot split an all-lowercase concatenation, so `secretkey`, `dbpassword`,
  `mypassword` and `authtoken` are NOT reported while `secretKey`, `secret_key`,
  `SECRET_KEY`, `db_password`, `myPassword` and `auth_token` are. Only the
  regular `+s` plural is folded — no other inflection is recognised. This is
  stated on SEC-001's own line in the Go coverage answer, not in a separate
  list.

  DB-053 and DB-020 are UNCHANGED: they consume a separately frozen vocabulary,
  proven identical name-for-name and verdict-for-verdict against the previous
  implementation.

### Added
- **`codefit-surface-authz` and `codefit-surface-idor` now declare the helper scope
  they audit against.** These two tools are file-level and stateless by design
  (ADR [0013](docs/decisions/0013-custom-authz-helper-registry.md)): they take files,
  not a project root, so they compute `known_authz_detected` against codefit's
  built-in authz-helper set only — a helper you registered with
  `codefit-baseline-register-authz-helper` is **not** applied there.

  That was true before and said nowhere an agent reads. Both tools now carry a
  `helper_scope` note in the response, and say the same thing in their tool
  description, including the part that matters most: **a `false` fact means "not
  seen", never "unauthorized"**. For the project-aware answer, `codefit-scan-all`
  and `codefit-scan-security` load the registered helpers as they always have.

  The field is absent — not empty — on `codefit-surface-overfetch` and
  `codefit-surface-nplus1`, which never consult the helper set, because declaring
  a limit a tool does not have is its own small lie.

- **Declared limit, measured: every tool response crosses the wire twice, and the
  client meters ONE copy — so the response budget is correct and the duplication
  stays.** (ADR [0079](docs/decisions/0079-the-client-meters-one-copy-so-the-duplicated-wire-is-a-declared-limit.md))

  `addTool` returns `nil` for the `*CallToolResult`, so go-sdk v1.6.1 copies the
  same output JSON into a `TextContent` block. Which copy a client *counts* was
  filed as unmeasured, because a wrong answer would have corrupted
  `ResponseBudgetBytes`. It was measured by driving two binaries — `main`'s and one
  whose `addTool` suppresses the copy — against a live client (Claude Code 2.1.196,
  2026-08-17) over stdio with the same content: at a ~74,968-byte payload both
  reported `74,580 characters`, identically, where metering both copies would have
  reported ~149,936. The two persisted results are byte-identical at 74,918 bytes.

  The bracket reproduces ADR 0062's (64,097/74,195 on `scan-all` content) at
  64,661/74,580 on `coverage` content — within ~1% at both ends, so the result is
  not an artifact of the method. **No behaviour changed and no number moved**:
  `addTool` at the exact commit ADR 0062 drove is byte-identical to `main`'s, so
  that calibration was always taken against a duplicated wire.

  **What stays true:** the transport still carries twice the bytes; the
  measurement covers one client, one date, one tool; and a client that reads
  `content` but not `structuredContent` — the compatibility case the MCP spec
  cites for the copy — was never exercised.
- **`codefit init` detects SQL schema directories — and writes only what it can
  PROVE.** Schema detection used to read a Prisma `schema.prisma` and nothing
  else, and it ran *behind* language detection: an unresolved language provider
  ended `Detect` before any schema enrichment, so a Java service with Flyway
  migrations — the exact project a language-independent SQL-DDL parser exists for
  — was never even looked at. Discovery is now language-independent (ADR 0018:
  a schema source is orthogonal to the application language).

  **A directory becomes a live `database.schema_paths` only when it proves**, in
  this order: its apply order is proven (every `.sql` at its own level carries an
  integer version the scan-time resolver matches — one stray filename
  disqualifies the level), the **real** SQL-DDL parser reconstructs at least one
  table from it, and it is the **only** directory that proved. The proof reads
  the directory through the literal scan-time reader and the same parser binding
  `codefit-scan-db` uses when `database.type` is unset, so init-time proof and
  scan-time behaviour cannot disagree about the same path.

  **Everything else gets the block COMMENTED, naming the real path and the
  reason** — "codefit cannot prove the apply order of these filenames" for a
  golang-migrate directory, "reconstructed no table" for one the parser read and
  made nothing of, and, when two or more proved, all of them with their table
  counts and the statement that codefit cannot know whether they are one schema
  or several. `schema_paths` entries merge into ONE reconstructed model, so a
  wrong extra entry does not add noise — it poisons the model, and every DB
  finding after it is stated at full confidence about a schema you do not have.

  **The invented `"db/migrations"` placeholder is gone from any config where
  codefit found a real path.** Nothing in the old file distinguished an example
  from a finding. When codefit genuinely finds nothing, the block now says so,
  states how deep it walked, and marks its example as an example.

  **`init --force` re-proves from disk**, and announces a demotion: a path that
  no longer proves is dropped, named, and explained, rather than disappearing
  into a later scan that suddenly measures nothing.

  Not covered, deliberately: codefit still does **not** sniff the SQL dialect, so
  a proof with no `database.type` set runs under the PostgreSQL binding. A live
  block carries a commented `type:` line directly above the key and the report
  names the dialect the proof ran under — the proof says the DDL reconstructs, it
  does not say the dialect is right. Measured and locked as a control: a
  MySQL-flavoured migration set (backticks, `AUTO_INCREMENT`, `ENGINE=InnoDB`)
  reconstructs **zero** tables under that binding, so it fails the proof gate and
  is commented rather than written. See
  [ADR 0074](docs/decisions/0074-init-writes-a-database-block-it-can-prove.md).

### Changed
- **The generated config and the init report state codefit's SEARCH RESULT, not
  a capability gap.** Both used to say SQL migration directories "are NOT
  detected" — a sentence about codefit, true of every project at once, and
  therefore silent about the one in front of you. They now report what codefit
  found in *this* project and why each candidate was left out, or that it looked
  and found none and how deep it walked. A config that says nothing about having
  searched cannot be told apart from a codefit that never searched.
- **codefit's own skill stops claiming a config it may no longer be describing.**
  The generated `SKILL.md` told every agent that "generated configs for a project
  like this carry NO such key". That is false the moment a proof succeeds on a
  project with no resolved language, so the claim is now conditional on what the
  run actually wrote: with a live `schema_paths` the skill names the key and the
  path instead. The frontmatter `description` is deliberately **not** gated —
  progressive disclosure loads the skill from the description alone, so narrowing
  it would mean a schema task never loads the skill at all.
- **`codefit init` gates the `database:` block on `schema_paths`, not on a
  detected ORM.** `database.orm` is read by **zero** production code — it is
  unvalidated free text that round-trips and nothing consumes — while
  `database.schema_paths` is what the DB sensor, `codefit-scan-db` and
  `codefit-scan-all` actually read. The block was gated on the former, with the
  latter inside it.

  **What this fixes, live today.** A drizzle (or typeorm) project's generated
  config was, in full:

  ```yaml
  database:
    orm: drizzle
  ```

  a section that looks configured and configures nothing — **and** its presence
  suppressed the schema-gap declaration in the init report, because that
  declaration keyed on the same ORM. The project with the gap was the one project
  not told about it.

  **What changes in the output.** A drizzle/typeorm project now gets **no**
  `database:` block at all, `orm:` included, and **does** get the
  "Not configured — database schema" section it never used to see. Losing the key
  is intentional: it configured nothing. The detection fact survives — the report
  still prints `orm  drizzle` under *Detected* — and a clarifier explains how
  "an ORM was detected" and "no schema is configured" are both true at once.

  A **Prisma project is unchanged** in every live key, value and order; the only
  delta is comment text, locked by a golden of the non-comment lines. Existing
  committed configs are untouched: only re-running `codefit init --force`
  rewrites anything.

- **`orm:` now says that nothing reads it.** It is still emitted for a project
  that has a schema — deleting a valid, user-visible key buys nothing — but with
  one comment stating that no sensor reads it and that `schema_paths` is what
  turns the DB dimension on.

- **The commented `database:` example names `type:` and what omitting it costs.**
  It previously showed `schema_paths` alone, which steered a MySQL or SQL Server
  user into codefit parsing their DDL as PostgreSQL **without saying so**. The
  example now shows `type: "postgresql"  # postgresql | mysql | sqlserver` and
  states the consequence; `sqlite` is named as the one value codefit refuses
  outright rather than guessing at. `type:` is **not** newly required — an empty
  value is still valid; the instruction was the defect, not the resolver. The
  same fix is applied to the copy of that example inside the generated skill,
  which is the first artifact an agent reads.

  See [ADR 0073](docs/decisions/0073-the-config-gate-follows-what-the-audit-reads.md).

### Changed — BREAKING
- ⚠️ **A configured `database.schema_paths` entry that resolves to no schema
  file now makes the scan NOT MEASURED instead of scoring 100.** The unread
  floor counted resolved *files* — a quantity the resolver itself decides — so a
  configured path that resolved to nothing was subtracted from both sides of the
  predicate and vanished from the audit. Two defects followed, both measured on
  `main`:

  1. A path naming a directory that holds one `.go` and zero `.sql` — the
     ordinary golang-migrate embed layout — returned
     `{"findings":null,"measured":true,"score":100,"surface":null}`: a clean
     bill of health over content codefit never read.
  2. With two configured paths, one real and one holding no `.sql`, the scan
     audited the real one correctly, reported `measured: true` and score 100,
     and **never mentioned the empty path anywhere**. The first case at least
     looks suspicious; this one looks completely normal.

  **What changes in the output.** For a project whose configured paths ALL
  resolve to nothing: `codefit-scan-db` reports `measured: false` with a note
  naming each path, and `codefit-scan-all` reports `score.by_dimension.db` as
  `null` with the `summary.db` block **absent** and the `db` section carrying
  `measured: false` plus the note. Nothing changes for a project whose paths
  resolve to real schema files. A project with a *partial* resolution keeps
  `measured: true` and every real finding — the note simply also names the path
  that resolved to nothing.

  **Migration.** The note states the action, and it is the whole migration:
  point the entry at the schema files it should audit, or remove it from
  `database.schema_paths` so the scan stops claiming to cover it.

  ```yaml
  database:
    schema_paths:
      - db/migrations/schema.sql   # the files, not the package holding them
  ```

  Declared cost, of the same class as the one already accepted one level down: a
  project whose paths genuinely resolve to nothing loses its db score instead of
  scoring 100. Losing a score for a schema nobody read is the correct direction.
  A directory whose `.sql` files sit one level DOWN also lands here — the
  listing is one level deep, and that limit is now declared at the resolver and
  reported out loud rather than scored. See
  [ADR 0072](docs/decisions/0072-a-configured-schema-path-always-leaves-a-trace.md).
- ⚠️ **A security finding in a test file is now `info` by default, not one
  severity level down (RF-10).** PRD v1.4 RF-10 has said since v1.3 that
  "findings de seguridad en archivos de test se degradan a `info`
  (configurable)", and §14 spelled out what configurable meant:
  `sensors.security.test_severity: info | downgrade | keep`. The code shipped
  neither half — it hardcoded the `downgrade` mode (critical→high, high→medium,
  …) and the key existed in zero Go files. `downgrade` is one of the PRD's own
  three modes, so the defect was the wrong default plus a missing key, not an
  invented behaviour. No ADR ever revised the requirement.

  **Migration.** A project that wants the previous behaviour asks for it:

  ```yaml
  sensors:
    security:
      test_severity: "downgrade"   # info (default) | downgrade | keep
  ```

  What changes without that key: test-path security findings report as `info`
  and cost the security dimension nothing (severity penalty 10→0 for a
  critical), so the security score RISES wherever such findings exist. Blocking
  is unaffected — under both `info` and `downgrade` a test path could never
  produce a blocking finding.

  `keep` applies no adjustment at all and is the only mode that can leave a
  critical security finding standing on a test path — which means it is the only
  one that can make a project block itself from its own fixtures. codefit
  accepts it (refusing a PRD-named mode would override the developer) and
  informs the consequence with one warning per run, emitted only when `keep`
  actually left a finding at critical. An unrecognised value fails `Load` with a
  located `path:line` error naming all three modes.

  `codefit init` deliberately does **not** emit the key: writing today's default
  into every generated project would mean a future change of the default never
  reaches them. Path criticality is still applied by the security sensor only —
  the DB sensor does not weight by path, and that gap is now declared rather
  than silent. See
  [ADR 0070](docs/decisions/0070-path-criticality-is-configurable-and-reaches-only-the-security-sensor.md).
- ⚠️ **`codefit-scan-all`'s `summary` is now PER DIMENSION.** The four
  unqualified counts (`endpoints`, `deterministic_findings`, `surface_items`,
  `certain_concerns`) were all computed from the **security** sensor's result
  while presenting themselves as the response's summary. A project whose only
  audited dimension was the database therefore received an all-zero summary
  over a `db.surface` holding dozens of items — a zero that means "nobody
  looked", read as "audited and clean". Observed in dogfood:
  `summary.surface_items: 0` beside 62 mapped DB surface items.

  **Migration.** `summary.security.*` carries the old values **verbatim**;
  `summary.totals.*` is the new cross-dimension number.

  | before | after |
  |---|---|
  | `summary.endpoints` | `summary.security.endpoints` |
  | `summary.deterministic_findings` | `summary.security.deterministic_findings`, or `summary.totals.deterministic_findings` for both dimensions |
  | `summary.surface_items` | `summary.security.surface_items`, or `summary.totals.surface_items` for both dimensions |
  | `summary.certain_concerns` | `summary.security.certain_concerns` |

  New: `summary.db` (`schema_sources`, `deterministic_findings`,
  `surface_items`) and `summary.note`. A sub-block is **`null` when that
  dimension was not measured** — the shape `score.by_dimension` already uses;
  the key is always present, never omitted, because an absent key is not the
  same statement as an explicit null. `summary.db` is null both when the
  dimension did not run and when it ran without measuring
  (`db.measured: false`). `summary.totals` sums only the units that mean the
  same thing in both dimensions — never `endpoints` (a table has no route) and
  never `schema_sources`.

  The counts are the RAW sensor population, taken before the baseline filter
  (the population the score already uses), so they can exceed what the buckets
  and `db.surface` list; `summary.note` states this in every response, so a
  reader can explain the difference without opening Go source.

  `summary.db` deliberately carries **no `certain_concerns`**: the security
  field of that name counts `Deterministic` **plus** `SurfaceConfirmed`, so it
  is not a certainty-1.0 count and a same-named DB sibling would be exactly the
  same-name-different-definition defect this change removes. Adding it later is
  additive; shipping a differently-defined one now would be breaking.

  Both agent-facing surfaces teach the new shape in the same change:
  `codefit-scan-all`'s tool description and the generated skill. The response
  floor grew 530 bytes (measured over the budget fixture: withheld-everything
  floor 4 016 → 4 546, full response 4 747 → 5 277, serialized `summary` block
  83 → 613 — the same delta three times, because the whole growth is the summary
  block: 440 bytes of always-present `note` plus 90 of per-dimension nesting),
  about 1.3% of the 40 000-byte budget — declared, not free. See
  [ADR 0069](docs/decisions/0069-the-scan-all-summary-declares-the-dimension-of-every-count.md).

### Added
- ⚠️ **`"go"` is now a resolvable language for security scanning and surface
  mapping (roadmap P4-1)**: `codefit-scan-security`, `codefit-scan-endpoint`,
  `codefit-scan-all`, and the `codefit-surface-*` family now accept `"go"`
  instead of refusing it — the registry's `Exposure.SecurityScan`/
  `SurfaceTools` for Go flipped from `false` to `true`. Go's reach is narrow
  and stated as such, never as parity with TypeScript: **6** declared
  security rules, **1 of 4** surface categories (`authz` only). Every scan
  response for an exposed language — Go and TypeScript both — gains a new
  `surface_coverage` field (on `codefit-scan-security`'s response and on
  `scan-all`'s `security` section) declaring exactly which surface
  categories were mapped and which were not, both machine-readable and in
  prose. `codefit-coverage` for `"go"` now returns a manifest DERIVED from
  its declared `Capability()` (a new `derived: true` field marks it) instead
  of erroring `"no coverage manifest for language \"go\""`; TypeScript's
  hand-written manifest is unchanged (`derived: false`), verified
  field-for-field against a pre-change golden. `codefit init`'s printed
  capability line and the generated skill both state the same N-of-4 reach
  before a user installs anything. See
  [ADR 0065](docs/decisions/0065-go-is-exposed-because-the-response-declares-what-it-lacks.md)
  and `docs/specs/declared-partial-language-exposure.md`.
- `internal/providers/registry`: the one ordered `language → provider` table.
  `LanguageProvider` gained `Capability()` (rule IDs per family, surface
  category coverage, coverage-manifest presence), and each registry `Entry`
  carries an independent `Exposure` (which resolvers currently admit the
  language) — capability and exposure are now two declared, test-checked
  facts instead of five hand-written, disagreeing ones. `scan-all`'s
  `providerForLanguage`/`surface.go`'s `providerFor`/`scaffold`'s
  `detectLanguage` all query the registry now; none of them builds a concrete
  provider on its own. No user-visible behaviour change at THIS commit —
  every resolver's answer set is byte-identical to before this change,
  verified against the pre-existing regression locks; Go stayed unexposed to
  security/surface tooling here, only `codefit init`'s detection reached it.
  (A later entry in this same `[Unreleased]` section — roadmap P4-1, ADR
  0065 — flips Go's `Exposure.SecurityScan`/`SurfaceTools` to `true`; this
  entry's registry/mechanism is what made that a one-field, checked change
  rather than a new switch.) `Exposure.InitDetect` is now actually enforced —
  `registry.ByMarkerFile` skips any entry whose `InitDetect` is false, table
  order preserved for the entries that remain eligible — closing a same-PR
  correction where the field was declared and documented but not yet
  consulted by `scaffold`'s `detectLanguage` (both registered languages
  already wanted it `true`, so no answer set changed). See
  [ADR 0064](docs/decisions/0064-language-capability-and-exposure-registry.md).
- **`providers.RuleSet` gained `Excluded []ExcludedRule`** (roadmap P1-4b),
  a declared, permanent exclusion — a different fact from `Declared`, which
  says a rule IS covered. `ValidExclusions()` (C6) checks an excluded rule id
  never also appears in `Declared`, driven through the interface with both
  real providers. Go's `Practices` RuleSet now names `PRAC-004` there with
  its [ADR 0056](docs/decisions/0056-a-practices-rule-affirms-only-what-it-checked-and-prac-004-is-dropped.md)
  reason, so `codefit-coverage` for `"go"` states the permanent gap in
  `NotCovered` instead of leaving an agent to infer it from `PRAC-004`'s
  absence from the declared list — v0.2.8 recorded this drop as BLOCKED for
  want of a landing site; `internal/providers/golang/capability.go` (ADR
  0064) is that site, so it lands here instead of in a coverage manifest
  file, which stays out of scope.
- **`RuleSet.ValidExclusionSource()` (C7)** — a phantom-exclusion check found
  owed by `sdd-verify` (obs #1467): C6 only proved an excluded id was not
  simultaneously `Declared`, never that it ever corresponded to a real rule;
  renaming Go's real `PRAC-004` entry to a fabricated marker left every
  existing test green. C7 closes that for `Enumerable:true` rule families
  (checked against the exact rule-id shape Control A already proves accurate
  for a real loader) and explicitly declares it not-applicable — never
  faked — for `Enumerable:false` ones, the same split
  `internal/core/dbcoverage`'s Control C draws for `internal/core/paradigm/`.
  See [ADR 0066](docs/decisions/0066-a-permanent-exclusion-is-a-typed-cross-provider-fact-and-a-phantom-one-is-still-a-lie.md),
  which also records why `ExcludedRule`/C6 itself earned this ADR after
  shipping without one.

### Fixed
- ⚠️ **`codefit init` no longer refuses a project whose language it does not
  recognize — and the message it refused with named files that could not help.**
  On a root with no `go.mod`, `package.json` or `tsconfig.json`, init exited
  non-zero and wrote nothing, with:

  ```
  no supported language detected in %q: expected one of go.mod, package.json,
  pyproject.toml/requirements.txt, or pom.xml/build.gradle
  ```

  That list is `config.allowedLanguages` (four languages), not the provider
  registry (two). Only `go.mod` and `package.json` could ever make detection
  succeed; `tsconfig.json` can too and is not named; `pyproject.toml`,
  `requirements.txt`, `pom.xml` and `build.gradle` are named and **cannot help** —
  creating one changes nothing. A Java project holding a `pom.xml` was told to
  create a `pom.xml`. Reproduced with the real binary before the fix.

  The refusal was the deeper defect. `project.language` is validated and then
  read by no production sensor, and a Python project with `database.schema_paths`
  is already fully audited by `codefit-scan-all` — so init withheld a config over
  a field the audit never reads, on a project the DB dimension could have
  audited.

  **Now:** init exits 0 on any root. It writes `.codefit.yaml` and the skill, and
  declares what it did not find in three places — the generated config, the init
  report and the README: which markers it looked for, that no code is scanned
  here, and that the DB dimension still audits the schema once
  `database.schema_paths` is set.

  The list cannot drift again, and each of the three places is held by a
  different mechanism rather than by one claim covering all of them. The config
  and the report **interpolate** the names from the registry
  (`registry.InitDetectMarkerFiles()`). README is prose and cannot interpolate
  anything, so its list is **locked against the registry by a test** that fails
  in **both** directions: a marker added to the registry and missing from README,
  and a marker README names that the registry does not have. And a hardcoded
  marker list in the code that renders those artifacts is now forbidden
  **structurally**, by a `go/ast` census of every production string literal in
  `internal/scaffold` and `internal/cli` with a per-literal allowlist that
  requires a stated reason — because a list typed today is byte-identical to the
  derived one today, and starts lying only at the next registry change.

  **Migration:** none for a project init already recognized — language,
  framework, ORM, schema paths and the capability statement are unchanged, and
  the generated skill for a detected language is byte-identical. A previously
  refused project now receives `project.language: "undetected"`, a value
  `config.Load` accepts. `""` is still rejected.

  `path_criticality` is omitted **whole** for such a project rather than emitted
  empty (an empty key renders YAML `null`, which reads as classified-and-empty),
  and the comment in its place states the consequence: no path is classified, so
  the RF-10 test-path re-weighting never fires and every finding keeps its
  natural severity.

  Adding Java and Python to the registry was rejected: it moves the line without
  removing it, since Ruby, PHP, C#, Rust and every future language stay refused.
  See
  [ADR 0071](docs/decisions/0071-init-never-refuses-over-language-it-declares.md).
- **The generated config's schema gap is now declared instead of discovered from
  an empty report.** `codefit init` writes a `database:` section only when it
  detects an ORM, and it detects a schema **only** from a Prisma
  `schema.prisma` — SQL migration directories (Flyway, golang-migrate, plain
  `.sql` DDL) are not detected at all. So a Flyway project's generated config
  audits nothing. The init report and the generated config now say so, and show
  how to point `database.schema_paths` at the migrations by hand. The
  declaration fires whenever **no ORM** was detected, not only when the language
  was undetected — a TypeScript project without Prisma has the identical gap.
  Detecting migration directories is follow-up work; this release declares the
  gap, it does not close it.
- **The db dimension reported files it had read and resolved correctly as files it
  was blind over.** The unread-schema floor asked the neutral model one question —
  "does any position name this file?" — and read "no" as "codefit did not see what
  this file declares". That inference is unsound: the same absence is produced by a
  migration whose every statement is `ADD COLUMN IF NOT EXISTS` on a column another
  migration already declares (reduced correctly; the correct reduction adds nothing,
  so it *cannot* leave a position) and by a file of pure `INSERT`/`GRANT`. Measured
  on a real 45-migration project: **13 of 45 migrations named under "codefit read
  NOTHING from this file", now 3** — and the 3 that remain are genuinely unseen
  content (`ALTER COLUMN … TYPE`, `SET DEFAULT`/`DROP NOT NULL`, and a CTE-prefixed
  statement). The neutral model gained a per-source **statement census**
  (`db.Schema.Sources`): how many statements the parser met, and how many it can
  *positively* explain away. A parser that never fills it (Prisma) degrades to
  exactly the previous behaviour — noisy, never a false all-clear. Two consequences
  are user-visible and deliberate: the **blind-file list is now enumerated in full**
  (an agent that cannot enumerate the files cannot check them; the benign lists keep
  their cap), and a project whose *every* configured schema path resolves to nothing
  structural now reports **not measured** instead of score 100 with zero tables —
  without that widening this very fix would have made a seed-only glob look like a
  clean audit. This supersedes [ADR 0044](docs/decisions/0044-source-bytes-become-text-and-an-unread-source-is-declared.md)
  §2.5's declared over-report for the DML/permission family. See
  [ADR 0068](docs/decisions/0068-a-negative-claim-needs-positive-evidence-the-statement-census-and-the-measured-budget.md).
- **`codefit-scan-all` told an agent its response fit a byte budget the response
  measurably exceeded.** The budget note derived its fit claim from the *withheld
  endpoint count* rather than from the response's size: with nothing withheld it
  returned "the complete endpoint list fit within this response's 40000-byte budget:
  NOTHING was withheld" without consulting the over-budget result `fitToBudget` had
  already computed by serializing the response. The reachable shape is ordinary — a
  project with a database and no security provider has zero endpoints and a large
  `db.surface` — and it exceeded the budget while affirming it fit. The note now
  states that the response does not fit and names *which* of the two reasons nothing
  could be withheld (no endpoints at all, or every endpoint pinned by a deterministic
  finding codefit refuses to hide). No behaviour change to the baseline write gate:
  the over-budget condition already blocked the write ([ADR 0061](docs/decisions/0061-the-baseline-write-is-gated-on-every-check-codefit-can-perform.md));
  only the prose lied. The structural per-bucket cap for `db.surface` — the only real
  lever for a DB-heavy response — stays open and is declared in the note itself.
- **`codefit-scan-security` and `codefit-surface-authz` hard-failed on any Go project
  containing an HTTP handler** — a regression shipped hours earlier in the same
  `[Unreleased]` window that first exposed Go (above): `internal/providers/golang/surface.go`
  never set `SurfaceItem.StructuralFacts`, so the Go zero-value nil map marshaled to JSON
  `null` and the tool's own output-schema validation rejected it
  (`structural_facts ... has type "null", want "object"`). On `main`, never released. Go's
  authz surface item now carries real facts from a go/ast body scan —
  `authz_denial_response_detected` (always present) and `known_authz_detected` (present only
  when the project has registered at least one authz helper, never a vacuous `false` against
  an empty searched set) — and the registry's `"go"` entry stops discarding its
  `authzHelpers` parameter, so a registered helper reaches the Go provider the same way it
  already reached TypeScript's. A new registry-driven contract test
  (`internal/providers/registry/surfacecontract_test.go`) asserts every registered
  provider's surface items marshal `structural_facts` as a JSON object, so this omission
  class cannot recur silently for a future provider. See
  [ADR 0067](docs/decisions/0067-every-surface-producer-emits-non-nil-structural-facts.md).
- **README.md's TypeScript surface-mapping reach was stated as three
  categories; it is four.** `nplus1` shipped as a fourth surface category in
  `v0.2.2` (`codefit-surface-nplus1`); two restatements — the "Works today"
  bullet and the Supported-languages table row — were never updated and
  undercounted it for roughly seven weeks, pre-existing and unrelated to any
  change in this window. Both corrected to name all four categories, and
  locked: `internal/providers/readme_surface_count_test.go` reads README.md
  and checks, per category actually in `typescript.New().Capability().Surface`,
  that a matching prose marker appears in both restatements — so a category
  added to the vocabulary with no updated README fails this test for a real
  reason instead of drifting silently again. A **third** restatement (line
  ~214, "Surface mapping — the agent reasons.") was found by `sdd-verify` as
  an already-accurate but unlocked gap; the same test now also checks it.

## [0.2.8] — 2026-08-10

**A PATCH release: no PRD phase closes here.** Phase 2 closed at `v0.2.5`; this release pays
that line's residual debt rather than opening Phase 3. So the line stays `0.2` and the MINOR
↔ phase table in [VERSIONING.md](VERSIONING.md) gains **no row** — that table maps phase
*closures*, and this closes none. The debt itself: a SQL-DDL parser fix for a real dropped
column, the DB coverage manifest closing a gap it had neither built nor declared, the DB
dimension running in `scan-all` without a security provider, the baseline write gated on
every check codefit can perform on its own output, the response byte budget calibrated by
measurement instead of chosen, and `report.score_weights` going from validated-and-ignored to
actually wired.

**It is not 100% Phase-2 work, and that is said here rather than left implicit.** Two Phase-3
pieces landed on `main` in this same window: `practices` became a weighted dimension in
`scoring.DefaultWeights()` (5 points, funded by `complexity`; [ADR 0055](docs/decisions/0055-practices-is-its-own-dimension-and-carries-the-smallest-weight.md)),
and the Go provider's own best-practice rules were audited for asserting more than they
checked, with `PRAC-004` dropped for claiming a synchronization check it never performed
([ADR 0056](docs/decisions/0056-a-practices-rule-affirms-only-what-it-checked-and-prac-004-is-dropped.md)).
**Neither reaches a product path**: `providerForLanguage` maps only TypeScript, the
TypeScript `AnalyzePractices` is still a stub, and there is no `codefit-check-practices`
tool — so `by_dimension.practices` stays `null` on every response regardless of either
change.

**Four user-visible contract changes ship in this PATCH, each measured against the code:**

1. `by_dimension` gains a `practices` key — always `null`, since no sensor exists yet.
2. `ScanAllResponse` gains an always-present `security` section (`{measured, note?}`),
   mirroring `db`'s shape but, unlike `db`, never `omitempty`.
3. `ResponseBudgetBytes` moves **60 000 → 40 000**
   ([ADR 0062](docs/decisions/0062-the-response-budget-is-calibrated-by-bisection-not-chosen.md)),
   calibrated by bisection against a real MCP client: **64 097 bytes accepted, 74 195
   rejected**. Measured consequence on a real 317-file project: **19 of 174 endpoints
   withheld** (5 actionable, 14 frontier_pending) in a 39 962-byte response — the same
   project fit entirely, nothing withheld, at the old 60 000.
4. `report.score_weights` **now actually drives the score** instead of being validated and
   silently discarded (`scoring.ResolveWeights`); a partial user map missing a measured
   dimension's weight now surfaces a named, actionable error instead of either silently
   dropping the dimension or hitting the internal-wiring-bug branch.

**The headline is honesty about what codefit already does, not new capability.** Checked
against the code rather than assumed: no rule was added to `dbrules`, `dwrules`, `paradigm`
or `crossrules`; no surface category was added; no dimension gained a sensor. What closes
here is six P0 defects (`docs/roadmap.md` P0-1 through P0-6 — an undeclared manifest gap, a
parser drop, a false all-clear risk in the cache, a response budget chosen rather than
measured, a wrongful language refusal, and a baseline written before delivery was known),
each traced during the work to the same shape — a layer asserting what the layer beneath it
never established — and named as such in
[ADR 0060](docs/decisions/0060-the-audit-protocol-five-layers-and-a-delivery-layer-that-did-not-exist.md)
and its contract, `docs/specs/audit-protocol.md`: five layers, one layering law, and six
invariants (I1–I6), each traceable to the defect that taught it. **Stated precisely, not
oversold: the protocol is a design contract targeting the `0.3.0` line, not a runtime
mechanism this release ships** — L3 (the delivery layer, `pending` state) is specified but
not implemented, and the document itself declines to assert I1–I6's control coverage from
memory. Only **I4** ("a partial result declares itself partial") carries a mechanical lock
tied to it by name as of this tag (`TestScanAllBudget_HonestyPersistsWhenTheBudgetForcesWithholding`,
`internal/mcp/scanall_budget_test.go`); the other five invariants are each addressed by
existing, older mechanisms (the completeness contract, the scope block, the coverage
manifest, the declared-limit tests) rather than by a registry that names them as such.

**Declared and not fixed:** the byte budget still guards a **token** limit with a **byte**
proxy, no matter how well the byte number is calibrated — the structural per-bucket cap that
would stop response size from depending on project size at all is P0-4's remaining half. MCP
defines no delivery acknowledgement, so the baseline-write gate
([ADR 0061](docs/decisions/0061-the-baseline-write-is-gated-on-every-check-codefit-can-perform.md))
mitigates the one reachable instance of that gap; it is not the structural cure the L3 design
above targets. `docs/roadmap.md` P1-1b (unifying the three independent language-resolution
switches) and P1-3 (deciding the Go provider's user-facing status) are deferred by explicit
architect decision, and P1-4b (`PRAC-004`'s owed coverage-manifest entry) stays blocked on
P1-3, since there is no Go coverage manifest to host it in without pre-empting that decision.

### Added

- **`codefit-scan-all` measures the DB dimension for a project whose language has no
  security provider, instead of refusing the whole scan.** A Go project with a
  configured `database.schema_paths` used to get `unsupported language "go"` from
  `scan-all` even though the DB dimension's schema parser never depended on
  `language` at all — the security sensor's unconditional hard error sat ~30 lines
  before the DB section ever ran
  ([ADR 0059](docs/decisions/0059-security-soft-dimension-in-scan-all.md)). Security
  now runs only `if secRan` (a provider resolves for `language`); a nil provider is
  non-fatal, a config-load or sensor error inside it is still a hard error, unchanged.
  When **neither** a security provider resolves **nor** the DB dimension runs,
  `scan-all` now returns an **error** naming both missing inputs (and the
  single-sourced supported-language set) instead of a 200 response with every score
  dimension `null` — indistinguishable from an impeccable project to an agent
  skimming it, and the one call shape that would have driven `scoring.Compute` with
  an empty `measured` set.
  - **⚠️ `ScanAllResponse` gains an always-present `security` key** —
    `{"measured": bool, "note"?: string}` — mirroring `db`'s shape but, unlike `db`,
    never `omitempty`: security applies to every project `scan-all` can run at all,
    so an absent section could only mean an older codefit build. A TypeScript
    response now carries `"security": {"measured": true}` beside everything it
    already carried; nothing else about the response changes value. Verified against
    the real pre-change response (captured via `git worktree` from `main` before this
    change, not re-implemented) with the new key stripped from both sides before
    comparing — the same class of change as `by_dimension.practices` below.
  - **The baseline's `scanned` set is now empty-by-default opt-in.** Before this
    change a security-owned baseline item (e.g. an `authz` item) could be wrongly
    marked `gone` and pruned by a DB-only pass, because `scanned` unioned the
    security categories unconditionally regardless of whether security ran. Proven,
    not assumed: the fix landed as two commits specifically so the corruption could
    be reproduced for real on a Go+schema fixture with a planted item (`Gone=1`) before
    the fix turned it green.
  - **The supported-language set now has one source.** `providerForLanguage`'s
    hand-written `switch` became a table (`languageProviders`); the new
    `SupportedLanguageNames()` derives the list the refusal message reads from what
    the table actually constructs. Three new regression locks
    (`internal/mcp/language_source_test.go`) keep it from silently diverging from the
    two other independent language-resolution switches in the codebase
    (`surface.go`'s `providerFor`, `scaffold/detect.go`'s `detectLanguage`) — one of
    them is the mechanical fence against wiring `golang.New()` into
    `providerForLanguage` without the open scope decision that would require
    (roadmap P4-1).
  - **`codefit-scan-security` and `codefit-scan-endpoint` are unchanged** and keep
    hard-erroring on an unresolved language — deliberate asymmetry: `scan-all`'s
    multi-dimension design lets one dimension be soft when another can still deliver
    value; the single-dimension tools have no dimension to fall back to.

- **The coverage manifest now answers for every capability the PRD promises, and a control
  enforces it.** `dbcoverage` had two mechanical controls and both looked **outward from
  the code** — one asks "is every registered rule declared?", the other "does every declared
  rule exist?". Neither looked **inward from the PRD**, which `CLAUDE.md` names as the
  project's source of scope, so a capability the PRD promises that was never built and never
  declared absent passed both: invisible from either end
  ([ADR 0057](docs/decisions/0057-the-coverage-manifest-answers-for-every-capability-the-prd-promises.md)).
  Measured: the PRD names **31** DB/DW rule ids, **23** are registered rules, and **7** were
  answered by nothing at all.
  - **The new control derives the promised set mechanically** — it reads
    `docs/PRD-codefit-v1.4.md` and extracts rule-id tokens with the same regexp the existing
    phantom-capability control uses. There is deliberately **no hand-maintained list** of
    "ids the PRD promises": a second list drifts exactly the way the manifest drifted and
    passes its own test while doing so. **Consequence, chosen rather than discovered:
    editing the PRD can now fail a test.**
  - **A third answer bucket, `DeliveredElsewhere`,** for a promised id whose capability
    ships under a different identifier. It exists because two buckets forced a lie: **N+1
    is promised as `DB-201` and has shipped since `v0.2.2`** as the provider's `nplus1`
    surface category, so calling it "not covered" is false and omitting it is the silence
    the manifest exists to prevent. `codefit-coverage` serves the new bucket, and the
    `codefit-coverage` tool description tells the agent to read it before concluding a rule
    id is uncovered.
  - **Six ids are now declared not covered,** each with what it would detect and why it is
    not built: `DB-021` (view logic that should be a function), `DB-022` (materialized view
    without a refresh), `DB-023` (view with broken references), `DB-032` (undocumented
    routine side effects), `DB-101`/`DB-102` (candidate 2NF/3NF violations). `DB-101` and
    `DB-102` are recorded as **surface candidates, never affirmations** — the PRD promises
    them *"vía razonamiento del agente"*, and a functional dependency is a fact about data
    and domain that no schema text establishes — so a future implementer does not build them
    as deterministic rules.
  - **No rule was built and no detection changed.** Every response, score, finding and
    baseline fingerprint is byte-identical; the change is to what the manifest **declares**,
    which is what the agent reads. `DW-022`'s entry is untouched — `DB-022` is declared
    against today's truth and points at the roadmap's P4-3, where reframing it as *surface*
    is decided but owes its own ADR and a `db.View` parser floor.

### Changed

- **Two manifest sentences that this change falsified were corrected, not left standing.**
  "The routine-body rule family is now COMPLETE … none deferred" enumerated `DB-030`,
  `DB-031`, `DB-040` and `DB-041` — but `DB-032` is a fifth member of that family and is
  not built. Both `dbcoverage.go` and `COVERAGE.md` now scope the claim to the four rules
  that read a body and name `DB-032` as the missing one.

- **The Go practices rules now say only what they check, and one of them was deleted for
  not doing so.** All five emitted at `Confidence: 1.0` with no `Probabilistic` flag —
  affirmations — while three of them claimed more than their code established
  ([ADR 0056](docs/decisions/0056-a-practices-rule-affirms-only-what-it-checked-and-prac-004-is-dropped.md)).
  - **`PRAC-004` is removed.** It said a goroutine was started "without a visible WaitGroup
    or channel to synchronize it" while checking only that a `go` statement existed — no
    synchronization detection existed anywhere in the file. It was **dropped rather than
    taught**: with `go/ast` alone and no `go/types`, and with synchronization free to live
    in a callee, a struct field, the caller or an `errgroup`, a sound affirmation is not
    reachable, and the practices dimension has no surface channel to demote a guess into.
    **Permanently not covered**, on the same footing as `DB-012` and `DW-022`.
  - **`PRAC-001` is retitled** `Possibly ignored error` → **`Discarded return value`**. It
    has no type information and never established the discarded value was an `error`, and
    "possibly" is a hedge at certainty 1.0. Its check is unchanged.
  - **`PRAC-003` no longer fires where `any` is unavoidable** — a generic type-parameter
    constraint (`func F[T any]`, `type S[T any]`) or a variadic parameter (`...any`,
    `...interface{}`). It still fires on an ordinary variable, field, non-variadic
    parameter, result, slice element or map value. It also now recognises the **`any`
    spelling**, which parses as an identifier rather than an `interface{}` node and which
    the rule had therefore never actually matched, despite its message naming it.
  - **`PRAC-005` fires only in a package that is not `main`**, which is the only
    library/command distinction the AST can make and the one its "library code should
    return errors instead" message rests on; retitled `panic in production code` →
    **`panic in library code`** to match. Its `strings.HasSuffix(path, "_test.go")`
    hardcode is **removed**: no rule carries its own notion of a test file — that is
    `config.PathCriticality`, applied by the sensor on the way out, as the security sensor
    already does.
  - **`PRAC-002` is unchanged** and gains a fixture that fails if its logic breaks.
  - **No user-visible detection changes.** No `scan-all`, `scan-security` or `check-db`
    response moves and no baseline fingerprint moves, because no product path reaches these
    rules: `providerForLanguage` returns `nil` for `go`, and the only caller of the Go
    `AnalyzePractices` in the repository is its own test file. The change is to the rules'
    honesty, not to what any user sees.

- **`practices` is a dimension of its own and carries the smallest weight.**
  `scoring.DefaultWeights()` is now security 35 · review 20 · db 20 · **complexity 10** ·
  tests 10 · **practices 5**, still summing to 100
  ([ADR 0055](docs/decisions/0055-practices-is-its-own-dimension-and-carries-the-smallest-weight.md)).
  It is the smallest weight by doctrine: codefit audits what the developer never sees, and
  `any` / `console.log` / a missing `catch` are the *most* visible defects there are — a
  linter underlines them in the editor. The dimension belongs in the score; it does not
  belong weighing like `db`.
- **No score moves. Not the global, not any dimension, on any project.** The 5 points come
  from `complexity` specifically, because it is post-v1.0 with no sensor, and `Compute`
  accumulates `totalWeight` over the **measured** dimensions only — a weight that is never
  in the denominator cannot change a quotient. Verified, not asserted: the pre-change
  golden captured at `79e34b0` still matches a live `scan-all` in **every** field, and a
  test freezes the pre-re-balance map as a literal and compares the whole `ScoreSummary`
  over every measured set `scan-all` can produce.
- **⚠️ `by_dimension` gains a sixth key — a response-shape change.** Every `scan-all`
  response now carries `"practices": null` beside the existing five. Nothing a consumer
  already read changed value, but anything that enumerates `by_dimension`, counts its keys
  or round-trips it through a strict schema will see the new entry. `null` is the honest
  value and follows [ADR 0021](docs/decisions/0021-by-dimension-scoring-wired-into-scan-all.md):
  the dimension is weighted but has no sensor, so it is *not measured* — never a fake `100`.
- **No rule changed.** No finding, surface item or baseline fingerprint moves; no baseline
  action is needed. `rules/`, `dbrules/`, `dwrules/`, `paradigm/`, `crossrules/`,
  `internal/core/dbcoverage/`, the per-language `coverage.go` manifests, `COVERAGE.md`, the
  MCP tool descriptions and the generated skill are all untouched. This change is one map
  of integers.

- **README.md described codefit's reach per PROJECT ("TypeScript is supported / Go is
  not") when, since P0-5, it is per DIMENSION: security and surface mapping run on
  TypeScript only, but the database dimension runs for any project that declares
  `database.schema_paths`, regardless of its language.** A reader with a Go or Python
  backend and a SQL schema could not tell from the README that codefit audits their
  schema. The `## Status` section now opens with the per-dimension statement before
  Install, and the "What codefit covers today" `Languages` bullet no longer reads as a
  whole-product language list. No code changed — the `## Supported languages` table's
  Go row was already corrected in `817070e` (P0-5); this closes the roadmap's P1-1a and
  P1-1c. No rule, response or baseline fingerprint moves.

### Fixed

- **⚠️ `report.score_weights` is now actually used by `codefit-scan-all` — a behaviour
  change, not just a bug fix.** `config.Validate` rejected a map that did not sum to 100
  and then `scan-all` discarded it: `scoring.MissingWeights` and `scoring.Compute` were
  both called with `scoring.DefaultWeights()` at every call site, so a re-weighted audit
  was validated and silently ignored (roadmap P1-2). `scoring.ResolveWeights` now decides
  which map `scan-all` uses: the user's `cfg.Report.ScoreWeights`, converted to
  `findings.Dimension` keys, when it names at least one entry; `DefaultWeights()`
  otherwise — an absent key is byte-identical to before this change (locked against a
  golden response captured via `git worktree add --detach` at this branch's base,
  `cfd1ad7`, not re-implemented by hand).
  - **⚠️ A partial map that used to be silently ignored can now produce an error.**
    `scoring.MissingWeights` has existed since [ADR 0021](docs/decisions/0021-by-dimension-scoring-wired-into-scan-all.md)
    specifically to catch a measured dimension with no weight, but it could never fire in
    practice: `DefaultWeights()` names every dimension `core/findings` declares. A
    user-supplied map is not guaranteed to — `{security: 100}` validates (it sums to 100)
    but names nothing for `db`, and a scan that also measures `db` now surfaces an
    **actionable, worded-for-the-user** error (`report.score_weights in .codefit.yaml has
    no weight for measured dimension(s) [db] — add them to score_weights (the map must
    still sum to 100), or remove score_weights entirely to use codefit's defaults`)
    instead of either silently dropping the dimension from the global or (the old,
    unreachable path) reading `codefit internal: ...`, which is reserved for a genuine
    codefit wiring bug (`DefaultWeights()` itself missing an entry), never a user config
    mistake.
  - **The sum-to-100 validation stays, unchanged, and is now defended in its own doc
    comment** (`internal/config/validate.go`): `scoring.Compute` normalizes by the weight
    sum of the *measured* dimensions, not by a hardcoded 100, so sum-to-100 is not
    load-bearing for the arithmetic — it is kept so the numbers mean what they look like
    they mean (an 80/20 split reads as percentage points only if 80 and 20 already are
    one), and so validation has one fixed target instead of an open-ended "just be
    positive" that would need its own new rules. Deliberately **not** required to name
    every one of the six declared dimensions: validation cannot know in advance which
    dimensions a given project will measure (`db` only runs when `schema_paths` is
    configured and in scope), so that completeness check stays at scan time
    (`scoring.MissingWeights`), where the actually-measured set is known.
  - No rule, finding, surface item or baseline fingerprint changes; the only response
    field this can move is `score` (`global` and, indirectly, nothing else — per-dimension
    scores are unaffected by weights).
- **⚠️ The `scan-all` response byte budget is now calibrated by measurement, not chosen —
  and it moves down: `ResponseBudgetBytes` 60 000 → 40 000, a user-visible behaviour
  change.** The old 60 000 was picked from a derivation (Claude Code's 25 000-token default
  at ~3 bytes/token, ~75 KB, minus margin) plus two single data points, never a measured
  ceiling (roadmap P0-4). Bisected live against a real MCP client (Claude Code, 2026-08-09),
  driving controlled-size responses cut from a real 317-file project over stdio: **64 097
  bytes ACCEPTED, 74 195 REJECTED** ("exceeds maximum allowed tokens") — the real ceiling
  sits in a narrower bracket than the old derivation assumed, with only 6–24% margin under
  it at 60 000, not the ~49% previously believed. 40 000 is 62% of the largest observed
  acceptance, chosen to tolerate roughly a 60% increase in token density before approaching
  the rejected end of the bracket. See
  [ADR 0062](docs/decisions/0062-the-response-budget-is-calibrated-by-bisection-not-chosen.md)
  for the full arithmetic and the stated assumption the number rests on: the client's limit
  is in **tokens**, this budget counts **bytes**, and the ratio between them is
  content-dependent, so the margin is not fixed — this is one client, one date, one content
  shape, not a guarantee about other MCP clients (Cursor, VS Code, OpenCode) or other project
  shapes.
  - **Measured consequence, verified directly against the real corpus, not assumed:** the
    same 174-endpoint project that fit entirely at 60 000 (0 withheld) now withholds **19 of
    174 endpoints** (5 actionable, 14 frontier_pending) at 40 000, in a 39 962-byte response.
    Real mid-sized projects will start seeing non-zero `withheld` counts they did not see
    before this change. This is honestly declared, not hidden: each bucket's `count` remains
    the complete number codefit classified, `withheld` says exactly how many are missing, and
    `codefit-scan-endpoint` still fetches any named endpoint's full detail on request — but it
    is a real behaviour change for real projects, not a free tightening.
  - **What this does NOT fix, stated so it is not mistaken for closed:** a byte budget cannot
    guarantee a token limit no matter how well the byte number is calibrated. The structural
    answer — a hard cap on entries per bucket, so response size stops being a function of
    project size — remains roadmap P0-4's open follow-up.
  - No rule, finding, surface item or baseline fingerprint changes; this is the same class of
    change as [ADR 0054](docs/decisions/0054-actionable-endpoints-are-named-and-the-response-declares-its-budget.md)
    (which this change supersedes only in its number, not its reasoning — naming instead of
    inlining is what made a byte budget worth calibrating at all).
- **`codefit-scan-all` no longer writes `.codefit-baseline` before it knows the response can
  reach its reader.** Reproduced live against a real MCP client: a fresh project's `scan-all`
  response was REJECTED by the client ("result (312,692 characters) exceeds maximum allowed
  tokens"), and `.codefit-baseline` had already been written in full — 373 items recorded as
  seen by a reader who received nothing; the retry reported "0 new, 373 known". The write ran
  ~100 lines before every check codefit performs on its own output
  ([ADR 0061](docs/decisions/0061-the-baseline-write-is-gated-on-every-check-codefit-can-perform.md)).
  The save now happens only after `scoring.MissingWeights`, `ScopeBlock.Validate()`, and
  `fitToBudget`'s `stillOver` have all passed — a response that fails any of them leaves the
  baseline exactly as it found it, byte-for-byte, and the next scan re-observes everything
  honestly.
  - **R2's symmetry, closed on the `known` side too:** the `gone` direction was already
    guarded by a two-dimensional scope (category AND file, ADR 0048) so a pass cannot prune
    what it never opened. `known` had no equivalent guard — concretely reachable through the
    code×schema cross rules (DB-010/DB-013), whose fingerprint anchors to the always-fully-read
    schema file, letting a narrowed pass re-confirm `known` (and duplicate the item in the
    saved baseline) under a category it had itself excluded from scope. One guard, the same
    shape as the existing `gone` guard, closes both.
  - **Declared, not solved:** MCP defines no delivery acknowledgement, so a well-formed,
    in-budget response can still be lost after codefit returns it — the Two Generals problem,
    unclosable by this or any bounded protocol. This change is a mitigation of the reachable
    instance (a response codefit itself already knows will not arrive intact), not the cure —
    the cure is deriving "seen before" from confirmed delivery, a larger structural change
    (invariant I3, `docs/decisions/0060-*.md`).
  - **Not a fix by atomicity:** `Baseline.Save` was already atomic (temp file + rename) before
    this change, and it did not help — the write was atomic, complete, and wrong. Recorded so
    "we made the write atomic" is not mistaken for closing this defect.
- **SQL-DDL: a real column named `key`/`index`/`fulltext`/`spatial` with an unmapped type
  and no parenthesized column list is now read as a COLUMN, not silently dropped.**
  `isInlineKeyIndexForm` (the MySQL inline-secondary-index-shorthand discriminator, shared
  dialect-free code, `internal/providers/sqlddl/reduce.go`) treated *any* unmapped type-like
  token after the keyword as the index form — parens or not. A real column whose type is
  simply outside the dialect's vocabulary (PostgreSQL's own `tsvector`, as in real Pagila's
  `film.fulltext` column) was misread as the shorthand and dropped (`Complete=false`, an
  honest abstention — the `tsql-alter-add-constraint` FABRICATION GUARD landed 2026-07-31
  and had already stopped the worse, zero-column-index *fabrication* four days earlier as a
  side effect; this closes the remaining drop). The fix splits off the type expression's
  trailing modifiers (the same split a column definition already uses) and asks whether
  exactly one bare, unmapped token remains with no `(` in it — a positive column test, not
  bare paren-presence, which would have regressed T-SQL's paren-less inline index into a
  fabricated column. Covered by a 24-cell matrix (4 keywords × 3 dialects × 2 call sites)
  and Pagila's `film` table, previously omitted from the fixture entirely because it tripped
  this exact bug — restored verbatim from upstream (commit `5ba5a57`) and now parses with
  all 14 columns and `Complete()==true`.
  - **Declared limit, not silently left behind:** the same unmapped-type token *with* a
    parenthesized argument list after it (`fulltext tsvector(10)`, `spatial
    geometry(Point,4326)`) is structurally identical to a named inline index (`KEY idx(a)`)
    and stays undecidable without reserved-word knowledge — it still fabricates an index
    from the type's own arguments. Locked as a characterization test
    (`internal/providers/sqlddl/limits_test.go`) so a future change cannot make it worse
    without the lock going red first.
  - **Two manifest sentences this bug's history had left stale are corrected in the same
    change** ([ADR 0058](docs/decisions/0058-a-declared-limit-can-go-stale-and-nobody-re-verifies-it.md)):
    `dbcoverage.go`/`COVERAGE.md` limit (5) still described the drop as a *fabrication*
    ("not yet fixed") four days after the FABRICATION GUARD had already closed that specific
    consequence as a side effect — the manifest was lying in the *safer* direction, but
    lying nonetheless. And `dbcoverage.go`'s DW-002 documentation claimed a specific
    warehouse shape (a dimension keyed by `PRIMARY KEY (fulltext)`) was "reachable, not
    hypothetical" for DW-002's surrogate-key check — measured directly in a `git worktree`
    of the pre-fix tip, it was **never** reachable: DW-002 abstains on an unproven table
    *before* reaching that check, and the same drop this fix closes was what made the table
    unproven in the first place. Post-fix, that exact shape becomes newly reachable and
    correctly fires — closing the drop did not just stop a silent loss, it also restored
    this shape's visibility to DW-002.

### Declared limits — stated, not hidden

- **The empty-read hole ADR 0053 declared against the finding cache is narrowed: the cache
  half is reproduced-and-disproved, not merely undisproved.** `v0.2.7` shipped that ADR's own
  words, verbatim: "this is not proven to occur" (`docs/roadmap.md` P0-3). Run against the
  real sensor and the real cache on unmodified `main`: a file observed empty (`findings=0`)
  and then holding real leaking content at the same path produced `findings=1` (`SEC-001`),
  byte-identical to an uncached control — **not reproduced**. The cache key is
  `sha256(analyzer ‖ path ‖ content)`, so empty and real content at one path are two different
  keys and a poisoned empty entry can never be served for non-empty content there: not
  "did not happen", **cannot happen**, by the key formula ADR 0053 already specified. Locked
  with `TestCache_EmptyReadNeverPoisonsLaterRealContentAtSamePath`
  (`internal/sensors/security/cache_test.go`), mutation-proved against a key edited to ignore
  content. **What remains true is not a cache defect**: `os.ReadFile` observing a file
  mid-write as empty is real, present with or without the cache, and transient — the next scan
  reads the real bytes. No sound fix exists (a legitimately empty file is common and
  indistinguishable from one mid-write), so none is attempted, and nothing about the cache or
  the walk changed. See ADR 0053's superseding note and `docs/roadmap.md` P0-3 (closed).
- ~~**`report.score_weights` in `.codefit.yaml` does nothing, and did nothing before this
  change either.** The key is parsed, and `config.Validate` rejects it when it does not sum
  to 100 — and **nothing ever reads it**. `scoring.DefaultWeights()` is hardcoded at both
  call sites in `internal/mcp/scanall.go`, and the field has no reference anywhere in the
  repository beyond its declaration and that validation. A user who re-weights their audit
  today gets their map validated and then ignored. Pre-existing, not introduced here, and
  deliberately **not fixed here**: making it real means deciding what a partial user map
  means and how it interacts with the `measured ⊆ weights` guard. Declared so it stops
  being a silent one.~~ **Fixed** (`p1-config-and-owed-entries`, still within this same
  `[0.2.8]` release — kept struck through rather than deleted so the history of this entry
  stays legible): see the `⚠️ report.score_weights is now actually used` entry under
  `### Fixed` above for the resolution, including the `measured ⊆ weights` interaction this
  entry named as the open question.
- **The PRD still reads `complexity: 15`** in its defaults line and its `.codefit.yaml`
  sketch. The PRD is exempt from the reflect-today rule, so this is recorded, not corrected.
- **DW-022's owed ADR is written — and it reverses the "permanently dropped" call it was
  expected to confirm.** `VERSIONING.md` recorded a materialized-view-refresh exclusion
  (`DW-022`, and its OLTP twin `DB-022`) as permanent, same lineage as `DB-012`
  (never-used index), and said the ADR was still owed. Per the decision recorded in
  `docs/roadmap.md` P4-3 (2026-08-04),
  [ADR 0063](docs/decisions/0063-materialized-view-refresh-is-surface-not-a-permanent-exclusion.md)
  pays that debt by reversing it: codefit still cannot **affirm** that a materialized view is
  stale (refresh cadence lives in scheduler state no DDL carries), but it *can* **enumerate**
  the materialized views a schema declares as surface and let the agent — which can read the
  cron, the migrations and the CI pipeline codefit never sees — resolve freshness. `DB-012`
  is unaffected: it has no equivalent smaller, DDL-provable claim to fall back to, so it stays
  exactly as [ADR 0024](docs/decisions/0024-db-012-never-used-index-permanently-not-covered.md)
  left it. **Decided and recorded, not built:** verified directly against the struct, not
  assumed — `db.View` (`internal/core/db/db.go`) carries only `Name`, `Pos` and `Body`, with
  no way to say a view is materialized, the same parser-floor shape `DW-021`'s `Index.Method`
  and `DW-020`'s `Table.Partitioning` each needed before their own rules; the eventual rule is
  planned as a **schema-level census** (one item per schema, never one per view, following
  `DW-005`/`DW-011`/`DW-020`), not a per-view affirmation.
  - `VERSIONING.md`, `COVERAGE.md` and `internal/core/dbcoverage/dbcoverage.go` each carry an
    append-only superseding note pointing at ADR 0063 — the original "permanently dropped"
    prose stays legible as the record of what Phase 2.5 decided and why it changed, rather
    than being rewritten to erase it.
  - **No rule, finding, surface item or baseline fingerprint changes.** `dwrules.All()` stays
    seven rules, `dbrules.All()` stays fourteen — this closes a decision debt, not an
    implementation one.
- **PRAC-004's owed coverage-manifest entry is recorded as BLOCKED, not faked.** Its
  permanent drop is recorded in
  [ADR 0056](docs/decisions/0056-a-practices-rule-affirms-only-what-it-checked-and-prac-004-is-dropped.md)
  and the CHANGELOG, and it owes a manifest entry — but there is no Go coverage manifest to
  put it in (`internal/providers/golang/coverage.go` does not exist), and creating one just to
  host this single entry would pre-empt the still-open architect decision on the Go
  provider's status (roadmap P1-3/P4-1). `docs/roadmap.md` P1-4b now names this blocker
  explicitly instead of leaving the debt unqualified. No code changes.

## [0.2.7] — 2026-08-04

**A PATCH release: no PRD phase closes here.** Phase 3 (code review, best practices,
tests, regression risk) has **not started** — its dimensions do not exist — so this stays
on the `0.2` line and the MINOR ↔ phase table in [VERSIONING.md](VERSIONING.md) gains
**no row**, exactly as `v0.2.6` did: that table maps phase *closures*, and this release
closes none. What lands is the prerequisite thread (**H0**) that unblocks the
regression-risk half of RF-06, which cannot exist without a notion of *what changed*.
**No audit rule changed.** Every security, DB and DW rule behaves exactly as it
did in `v0.2.6`: for a full scan no finding, no surface item and no baseline fingerprint
moves, and `COVERAGE.md` and the `codefit-coverage` manifest are untouched. What lands are
the **two cheap layers of the filtering pyramid that were still missing**: **layer 0**
(the agent can now tell codefit *which files it changed*, and the audit narrows to them)
and the **content-hash finding cache** (the same analyzer, over the same bytes at the same
path, no longer re-analyses them). They are orthogonal — the first decides *which files get
audited*, the second decides *which results get recomputed*.

**⚠️ NOT breaking for a committed baseline.** Stated because the last release that *was*
carries the same marker: `v0.2.5` moved the fingerprint of every column-anchored DB item
and existing `.codefit-baseline` entries re-appeared as `new` until re-accepted. Nothing
here does that. `findings.Fingerprint(category, file, content)` is byte-for-byte the
function `v0.2.6` shipped, no sensor's `OwnedCategories()` changed, `baseline.Item` and the
committed file's shape are untouched, and `rules/`, `dbrules/`, `dwrules/`, `paradigm/`
and `crossrules/` have no diff at all against `v0.2.6`. **No baseline action is needed on
upgrade.** What *did* change around the baseline is a guard, not a fingerprint:
`baseline.Diff` now takes a file scope beside its category scope, so a partial pass can no
longer prune files it never opened — and it fails in the under-reporting direction.

**⚠️ BREAKING for a consumer that parses the `codefit-scan-all` response.** This is a
user-visible contract change inside a PATCH, and it belongs at the top rather than in a
footnote. **`actionable` no longer carries per-concern detail.** Each entry now *names* its
endpoint with what it takes to rank it — `file`, `line`, `method`, how many concerns and
of which `categories`, `highest_certainty`, `has_affirmation` — and the per-concern
`signals` / `reason_to_review` text is fetched on demand with `codefit-scan-endpoint`.
Deterministic findings are the exception and still come back in full. Every response also
gains two new blocks, `scope` and `budget`. Anything that read `actionable[].concerns[]`
must now make a second call for the endpoints it pursues. What justifies a shape change on
a `0.x` PATCH is that this is what made `scan-all` **return at all** on a mid-sized real
project — see the first `Fixed` entry. Nothing codefit *concluded* moved: `score`,
`by_dimension`, the baseline delta, the summary and `scope` are still computed over the
complete analysis.

**Two defects were found and fixed INSIDE this release cycle; neither ever reached a
tagged version.** They are the two entries most worth reading before upgrading — a cache
that could serve a **false all-clear**, and a `scan-all` that **did not return** — and
both are recorded at full size below. But they are not emergency fixes to shipped code:
`v0.2.6` predates the cache and the scope entirely. At that tag nothing outside
`internal/core/cache` imported the package (it was inert, wired for the first time here)
and `internal/core/scope` did not exist, so **no released version of codefit was ever
exposed to either.** Recorded plainly in both directions: neither buried, nor dressed up
as a field incident.

**The scope is an INPUT, never derived from git.** codefit does not shell out to git, does
not read `.git`, does not diff refs and assumes no branch model. Two reasons, both standing
doctrine: it has **no power over the user's git**, and the MCP caller is an agent that
already knows which files it touched — it just wrote them. So `changed_files` is an
optional list of project-relative paths on the request, and **absent or empty means a FULL
audit** — an empty list is never read as "audit nothing".

**Narrowing is only safe if the narrowing stays visible.** A partial audit indistinguishable
from a full one is a lying auditor, so every scan response now carries a `scope` block, and
the two places a partial scan could quietly overstate itself are both closed:

- **`blocked: false` from a partial scan is a narrower claim.** `scoring.IsBlocked` is
  unchanged and stays non-configurable, but under a partial scope it means *no critical in
  the audited slice*, not *no critical*. The response says so in prose; `blocked: true`
  needs no caveat. The same applies to `score` and to `by_dimension`.
- **A partial scan cannot prune the baseline.** This was a live corruption risk, not a
  hypothetical: a security finding in a file the pass never opened still belongs to the
  `security` category, which *did* run, so the existing category-scoped `gone` guard
  (ADR 0019) would have marked it gone and `codefit-baseline-prune` would have deleted the
  audit memory of every file the scan did not look at — silently, in the direction of going
  blind. The baseline scope is now **two-dimensional**, and `codefit-baseline-prune` accepts
  **no scope at all**: a deliberate asymmetry, because scanning may be cheap and partial but
  forgetting may not.

**The cache exists so that the HONEST scan stays affordable.** That is its justification,
not speed for its own sake. A full scan is the only one that can prune the baseline and the
only one whose `blocked: false` means what it appears to mean; if the full scan is expensive
and the narrowed scan cheap, every caller narrows and codefit degrades into a tool that
permanently looks through a slit and can never forget anything. **A warm scan and a cold
scan are byte-identical** — not equivalent, identical: a cache that can change the output is
not a cache, it is a blind spot. The cache is **off unless a project asks for it**, and
every cache failure is a miss, never a failed audit. **The store also bounds itself**:
entries are grouped by the analyzer generation that wrote them, and opening the cache
collects the generations this build superseded, so a rule author rebuilding several times an
hour no longer accumulates a full copy of the project's entries per build. ADRs
[**0050**](docs/decisions/0050-the-cache-key-is-the-analyzers-own-bytes.md) and
[**0051**](docs/decisions/0051-the-finding-store-is-bounded-by-generation-and-pruned-on-open.md);
the contract is `docs/specs/finding-cache.md`.

**Both are now exercised on real projects, not on fixtures alone.** A committed, build-tagged
dogfood harness runs the scope and the cache over four real TypeScript projects, read-only,
and it is what lifts the "covered by tests and the CI self-audit but never exercised on a real
project" limit both of them shipped with. It also produces codefit's **first measured
milliseconds** for the cache — read them as a measurement on one machine and not as a property
of codefit, and read the corpus's honest limits with them (ADR
[**0052**](docs/decisions/0052-optimizations-are-validated-by-a-committed-dogfood-harness-over-real-projects.md)).

### Added

- **`changed_files` (optional, a list of project-relative paths) on `codefit-scan-security`
  and `codefit-scan-all`.** Only those files are analysed. It is deliberately **not** on
  `codefit-scan-db` (its inputs are the configured `database.schema_paths`, not a repository
  walk) and **not** on any `codefit-baseline-*` tool. Paths are canonicalized on both
  construction and lookup, platform-independently, so `src\a.ts`, `./src/a.ts` and
  `src/a.ts` are the same file on every OS — the separator drift `v0.2.6` paid to remove is
  not reintroduced by the scope.
- **A `scope` block on every scan response** — `{mode, requested, audited, auditable_total,
  unmatched, note}` — present unconditionally, including on a full scan (`mode: "full"`,
  empty note), so a consumer never has to infer the mode from an absence. `auditable_total`
  counts the **whole project**, never the scope: narrowing must not shrink the denominator,
  or "3 of 412" collapses into a self-flattering "3 of 3".
- **`unmatched` — the requested paths the audit never reached** (deleted, not an auditable
  extension, outside the project, inside a skipped directory). Without it an agent that
  passes three wrong paths receives "0 findings" and reads it as clean; `unmatched` is the
  difference between *audited and clean* and *never looked*.
- **The `note` is enforced in production, not just asserted in tests.** `ScopeBlock.Validate()`
  runs in the handler and fails the call in both directions — a `partial` scan with an empty
  note, and a `full` scan carrying a caveat it has no basis for. An unlabelled partial result
  is exactly what an agent would read as a full one, so it is a loud error rather than a
  silent one.
- **`internal/core/scope`** — a pure leaf (it imports nothing from codefit) holding the file
  scope, with one exported canonical form (`scope.Canon`) so a caller comparing a path
  against a scope uses the same rule instead of re-deriving it.
- **The skill `codefit init` generates now teaches the scope**: how to pass `changed_files`,
  that a partial `blocked: false` and `score` are narrower claims to be reported as such,
  that `unmatched` is not "clean", that `by_dimension.db` `null` is not `100`, and that a
  partial scan must **never** be followed by a baseline prune. **This reaches an existing
  install only by re-running `codefit init`** — a skill file already on disk stays stale
  until it is regenerated.

- **The content-hash finding cache is WIRED into the security sensor**, consulted per file
  inside the walk, on the raw bytes, before anything is parsed. A hit reuses the whole
  analysis; the file is still opened, still counted and still reported either way — the
  cache decides what is **recomputed**, never what is audited. It is **opt-in**:
  `config.Cache.Enabled` has no default, so a project with no `cache:` section has it off,
  and `codefit init` does not write one. Turn it on by adding to `.codefit.yaml`:

  ```yaml
  cache:
    enabled: true
    # dir: .codefit/cache   # the default; a relative dir resolves against the project root
  ```

  An empty `dir` defaults to `.codefit/cache`, which is already gitignored and already
  skipped by the walk.
- **The key is `sha256(analyzer identity ‖ project-relative path ‖ content)`, and the
  analyzer identity is the SHA-256 of the running executable** (`os.Executable()`, hashed
  once per process and memoized). Keying on file content alone — what the inert package did
  — would make codefit lie in a specific way: you upgrade, the new binary ships new rules,
  the file did not change, so codefit returns findings computed under the OLD rules and
  reports "clean" under rules it never ran. **A version string is not the fix, and fails
  exactly where it matters most:** `version.Version` is the constant `"v0.1.0-dev"` for any
  plain `go build`, `go run` or `go test`, so during rule development every build would
  present the same key and the rule author is the first person the stale cache bites.
  Hashing the binary's own bytes covers **every** input that can change a verdict — the YAML
  rules, the Go-coded detectors, the parser, the surface queries — because all of them are
  in the binary, and under `go run` / `go test` a fresh temporary build changes the identity
  automatically. Two builds of identical source miss: wasted work, never a stale verdict.
- **The PATH is in the key** — a defect in `docs/specs/finding-cache.md` R2, which named only
  analyzer + content, found and corrected while implementing. Two files with identical bytes
  are ordinary (this repository's own fixtures contain them), and under an analyzer+content
  key they would share one entry, so the second file would be reported carrying the *first*
  file's path in every finding and surface item, and therefore a colliding baseline
  fingerprint. Locked by a test.
- **An unresolvable analyzer identity DISABLES the cache for that run** — the scan completes,
  fully, analysing everything, and says so through `slog`. An unknown key input means do not
  reuse; falling back to a content-only key would be the stale verdict above.
- **An entry holds the findings AND the mapped surface, stored BEFORE path criticality is
  applied.** Both halves, because an entry with only the findings would serve a warm scan
  that silently lost the surface. Pre-criticality, because criticality is applied on the way
  out to a cached entry exactly as to a fresh one — so **editing `path_criticality` in
  `.codefit.yaml` re-weights severities on the very next scan without invalidating a single
  entry.** Caching the adjusted findings would serve stale severities after every config
  edit. Locked in both directions.
- **A file that produces zero findings and zero surface is cached as an empty entry and is
  not re-analysed.** Clean files are the majority in a healthy repository; treating "nothing
  found" as "not cached" would leave the cache doing nothing exactly where most of the work
  is, while appearing to work.
- **Every cache failure degrades to a MISS, and the write is ATOMIC.** A missing, unreadable
  or corrupt entry is a miss and the file is analysed normally; a failed write is reported
  through `slog` and never appears in the JSON, because the audit already happened and all
  that is lost is the saving on the next run. The write is a temp file plus a rename — the
  same shape the committed baseline uses — because codefit is an MCP server and two tools
  over one project (an agent firing `scan-security` and `scan-all` together) can reach the
  same entry path at once, while `os.WriteFile` truncates before it writes.
- **The cache store is BOUNDED: entries live under a generation directory, and `Open` prunes
  it.** Keying on the analyzer's own bytes has an arithmetic: **every codefit build mints a
  fresh generation of entries for the whole tree and orphans the previous one entirely** —
  one generation per upgrade for a user on release binaries, one per `go build` for anyone
  developing rules, several times an hour. Two smaller growths ride along inside a single
  generation and do not stop even for someone who never rebuilds: every edit to a file mints
  a new key and orphans the old entry, and a file deleted from the project leaves its entry
  behind forever. Stored flat, nothing collected any of it — the store grew for the life of
  the project and only `rm -rf` ever shrank it. So entries move from `Dir/<key>.json` to
  `Dir/<gen>/<key>.json`, and a superseded build becomes **one directory to drop** rather
  than a set of files to identify one by one (the key is a hash, so nothing in an entry's
  name says which analyzer wrote it). `<gen>` is 16 hex chars of the analyzer identity — a
  *label*, not the boundary that separates two analyzers, since the full identity is still
  inside the key hash; 16 rather than 64 keeps the entry path clear of Windows' `MAX_PATH` on
  a deep project root. An identity that is not the expected 64-hex SHA-256 is **hashed** into
  that shape rather than truncated as-is: the label is a path element, and `../../x` would
  otherwise address a directory outside the cache. On `Open`, once per process per generation
  directory: the **current generation is kept ALWAYS**, plus the **2 most recently modified**
  others — three in all, not one, because a developer alternating between an installed
  codefit and a dev build would otherwise have each run destroy the other's generation and
  never see a hit again; entries in the current generation **not written in 30 days** are
  removed; and the flat entries the previous layout left behind are removed once, as a
  migration. A hit does not rewrite its entry, so a live entry ages out too — that costs one
  re-analysis, the safe direction. ADR
  [**0051**](docs/decisions/0051-the-finding-store-is-bounded-by-generation-and-pruned-on-open.md),
  which supersedes the "no eviction" consequence of ADR 0050.
- **The prune only ever recognises the two shapes codefit writes itself.** This code deletes
  files from a directory the user can also write to, so a generation directory must match
  `^[0-9a-f]{16}$` and an entry file `^[0-9a-f]{64}\.json$`. **Anything else under the cache
  directory is never touched, at any age** — another tool's file, a note, a directory nobody
  here created — and it is test-locked over a fixture holding a `README.md`, a `notes/`
  directory and a `keep-me.json`, all of which must survive a prune that really deletes
  generations around them. The prune is **best effort** and reports nothing: an unreadable
  directory, a file it may not remove, a race with a second codefit process are all
  swallowed, because a cache that cannot clean itself still has to work. Maintenance is never
  the reason an audit does not happen.

- **A committed dogfood harness that runs the scope and the cache over REAL projects**
  (`internal/mcp/dogfood_cache_test.go`, `//go:build dogfood`). Fixtures prove a contract;
  they cannot show that the contract survives 300 files of somebody's real TypeScript, and
  they cannot produce a single measured millisecond. `TestDogfoodCache` runs the **real**
  security sensor cold and then warm over each project and asserts the two `SensorResult`s
  are **byte-identical** (wall-clock zeroed, since a working cache necessarily changes it).
  `TestDogfoodChangeScope` drives layer 0 over the same projects, with the requested paths
  spelled **non-canonically** — a `./` prefix plus OS separators, which is how an agent hands
  over a git diff on Windows — so "nothing went unmatched" exercises `scope.Canon` instead of
  being a tautology, and it asserts that the denominator stays the whole project (5 of 317,
  never 5 of 5) and that the narrowed pass reaches the same verdict on those files as the full
  pass did. Every number is guarded against passing by vacuum: 0 audited files, a provider
  never called, a cache holding fewer entries than files audited, or a warm run that
  re-analysed anything all fail loudly.
- **The harness is read-only over the clones, and costs a contributor who has none nothing.**
  Nothing is written inside a dogfood project: the config is synthesized **in memory** and the
  cache directory is an absolute `t.TempDir()` — which is exactly why these tests drive the
  sensor rather than the MCP handler, since `runSecurity` loads `.codefit.yaml` *from the
  project root*. It is behind the `dogfood` build tag, so the normal gate never compiles it,
  and it **skips clean** when `dogfood.local.json` is absent. That file is **per-machine and
  gitignored**: reproducing any of this requires your own clones and your own config, listing
  their absolute paths (the format is in `dogfood_cross_test.go`'s header). ADR
  [**0052**](docs/decisions/0052-optimizations-are-validated-by-a-committed-dogfood-harness-over-real-projects.md).
- **The first measured numbers for the finding cache.** Measured on **one Windows machine, on
  these four projects, at these file counts, on 2026-08-03** — a measurement of one run on one
  desk, never a speed codefit promises:

  | project | files audited | findings | surface | cold | warm |
  |---|---|---|---|---|---|
  | salonpro | 317 | 1 | 386 | 5989 ms | 514 ms |
  | bitacoras | 147 | 0 | 102 | 2473 ms | 168 ms |
  | plantalinda | 309 | 0 | 0 | 5023 ms | 265 ms |
  | metricasbatch | 14 | 0 | 0 | 465 ms | 11 ms |

  The warm runs re-analysed **0 files** and the cold and warm results were **byte-identical**
  on all four. **The cold column is the unstable one:** repeats varied by roughly **±2x**
  (salonpro ran 5989–11627 ms) because of the operating system's own filesystem caching — the
  warm figures were stable. Divide the two columns if you like, but the quotient is an
  observation about this machine, not a property of the tool.
- **Two of the four projects produced ZERO findings and ZERO surface** — stated here rather
  than buried under the wins, because it also bounds what the numbers above are worth.
  metricasbatch is a Vite React SPA with no route handlers, and plantalinda's only Next.js
  route handler returns a static `new Response("ok")`; **ADR 0005's frontier is correct to
  emit nothing** for either, and the harness author checked that rather than relaxing the
  guard. Those subtests still prove the walk, the store and the warm hit over hundreds of real
  files — and because they cannot prove the *payload* survives the round trip, the harness
  asserts "a warm cache preserved findings" and "a warm cache preserved surface" across the
  **corpus** after the loop instead of faking either per project. Read the whole table with
  that in mind: half this corpus exercises almost nothing. Four real projects the author
  happened to have on disk are **not** a representative sample of anything.

### Changed

- **`baseline.Diff` takes a file scope beside its category scope**, and an item is eligible
  to be `gone` only when **both** admit it (`scanned[item.Category] && files.Includes(item.File)`).
  Either dimension alone is a way to go blind, in opposite directions: a category-only guard
  prunes the files a partial pass never opened, a file-only guard prunes the dimensions whose
  sensor did not run. Both fail safe — an empty category set and the zero-value scope each
  include nothing, so a caller that forgets one **under-reports and never prunes**. The
  no-regression floor is a golden: a full scope produces a delta **byte-identical** to the
  previous behavior.
- **The code×schema cross rules (DB-010/DB-013) still RUN under a narrowed scope, but their
  categories abstain from the gone-scope.** Their items **anchor to the schema** while the
  evidence for them is **every query filter in the repository**, so a narrowed pass can leave
  the anchor in scope while the justifying query sits in a file it never opened — the one
  shape the file dimension cannot protect. Abstaining is the same posture the DW census rules
  already take: a shrunken census does not get to judge.
- **The DB dimension is reported as NOT MEASURED (`by_dimension.db: null`) when no configured
  schema path is in scope** — never `100`. A configured path may name a directory of
  migrations, so a scoped file inside one counts; the prefix test is on canonical path
  *segments*, so `db/migrations-old` is not mistaken for `db/migrations`. When the dimension
  does run it reads **all** its configured schema paths, never a narrowed subset: a schema
  judged from half its migrations is itself a shrunken census.
- **`codefit-baseline-prune` always re-scans in full**, and takes no `changed_files`.
- **`findings.SensorResult` gained `auditable_total` and `audited_files`** — a sensor's own
  account of what it looked at. A sensor that WALKS reports both (they agree under a full
  audit); a sensor whose inputs are CONFIGURED rather than walked (the DB dimension) reports
  the sources it read and leaves `auditable_total` zero, because there is no file census to
  be a denominator of. Additive for a consumer that ignores unknown fields.
- The `codefit-scan-all` MCP tool description now documents `changed_files` and what a
  partial run does and does not claim — it is the only thing an agent reads before choosing
  a tool.

### Removed

- **The dead `AuditContext.Since` field.** It was a `string` whose comment promised "a git
  ref for incremental (`--since`) mode" and it had **never had a reader or a writer**. A
  field naming a capability codefit does not have is the same class of lie as a manifest that
  over-promises, so it is **replaced** by the real scope rather than kept alongside it.
- **`internal/core/pipeline`, and the `NoLLM` / `FailOn` / `Interactive` fields of
  `AuditContext` — the LLM-era scaffolding.** This is not an unfinished wiring task being
  abandoned; the package was **correctly designed for a codefit that no longer exists**. As
  born it held one real decision — early-exit before a *paid* layer-3 LLM call
  (`if layer.Layer() == LayerLLM && meetsFailOn(all, ctx.FailOn) { break }`) — which earns a
  pipeline object when the expensive tier costs money. The MCP-first pivot deleted that layer
  on the package's **second day** (`3999505`, "drop the LLM layer"), leaving a `for` loop
  with no decision in it, and all three layers of the pyramid were then implemented without
  it: regex and AST inside `scanFile`, and layer 0 wired straight into the walk. `FailOn`'s
  own comment still named the extinct machinery. **The pyramid itself is doctrine and stays**
  — what goes is one expression of it the code never adopted (ADR **0049**).

### Fixed

- **`codefit-scan-all` did not RETURN on a mid-sized real project.** Over a 317-file
  TypeScript repository it produced **313 368 bytes** and exceeded the MCP client's output
  limit — the tool codefit's own skill tells an agent to call FIRST. **99.3 % of it was one
  section**: `actionable` inlined 367 concerns across 160 endpoints at ~794 bytes each,
  while `frontier_pending` named its 14 endpoints in ~122 bytes apiece. This was not a new
  problem, it was an old decision applied to half the response: `scan-all` returns a
  three-bucket synthesis precisely *because* the canonical dump truncated in MCP clients
  (PRD §21, ADRs 0006/0008), and `actionable` never adopted it.

  **`actionable` now names its endpoints like the other two buckets do.** Each entry carries
  what it takes to RANK and choose — `file`, `line`, `method`, how many concerns and of which
  `categories`, how many are actionable and of which `gaps` (hardest kind first),
  `highest_certainty`, `has_affirmation` — and the per-concern `signals` / `reason_to_review`
  text is fetched on demand with `codefit-scan-endpoint`, which is stateless and re-runs the
  same analysis, so what comes back is exactly what was left out. **Deterministic findings are
  the exception and stay in full** (`deterministic_concerns`): a finding at certainty 1.0 is a
  fact codefit already concluded, and hiding it behind a second call would make a scan's
  headline result depend on the agent choosing to look.

  **Measured on real projects** through the committed dogfood harness (ADR 0052), read-only:
  **salonpro 313 368 → 42 012 bytes** with all 160 actionable endpoints still named and
  nothing withheld; bitacoras 40 282 → 9 903. The harness fails if no project in the corpus
  would have exceeded the budget under the old shape, so the measurement cannot quietly
  become a claim about small projects.

  **Naming is a constant factor, not a bound, so the response declares its own budget.** Every
  `scan-all` response now carries a `budget` block (60 000 bytes; MCP clients cap tool output
  and this JSON runs ~3 bytes/token). When the endpoint lists do not fit, whole buckets are
  withheld lowest-priority first — `resolved_clean`, then `frontier_pending`, then
  `actionable` — lowest-ranked entries first, and the response states how many endpoints are
  missing and what ordering they are a prefix of. Each bucket keeps declaring the **complete**
  `count` it classified, with `withheld` accounting for the difference. `withheld: 0` still
  carries a note: "no mention of truncation" and "nothing was truncated" must not be the same
  bytes on the wire. An endpoint carrying a deterministic finding is never withheld, and a
  response that is still over budget after withholding everything says so instead of being
  clipped.

  **Only the rendering narrows — nothing codefit concluded moves.** `score`, `by_dimension`,
  the baseline delta, the summary and the `scope` block are computed over the COMPLETE
  analysis, exactly as before; the set of endpoints named in `actionable` is exactly the set
  that used to be detailed there. Locked from both sides: against a golden captured from the
  pre-change tree, and by running the same project at two wildly different budgets and
  requiring those four to agree field-for-field. **No audit rule changed** — no finding,
  surface item or baseline fingerprint moves, and `COVERAGE.md`, `internal/core/dbcoverage/`
  and the per-language `coverage.go` manifests are untouched. The `codefit-scan-all` /
  `codefit-scan-endpoint` tool descriptions and the generated skill teach the new shape in the
  same change, because they are the only thing an agent reads before choosing a tool — **an
  existing install only sees it after re-running `codefit init`**. ADR
  [**0054**](docs/decisions/0054-actionable-endpoints-are-named-and-the-response-declares-its-budget.md);
  the contract is `docs/specs/scan-all-response-budget.md`.

- **The finding cache could serve a FALSE ALL-CLEAR: any valid JSON at an entry's path was
  read as "analysed, nothing found".** `(*cache.Cache).Get` tested corruption by asking
  whether `json.Unmarshal` returned an error, and `null`, `{}`, `{"unrelated":1}` and
  `{"findings":[],"surface":null}` all parse. Each unmarshals into a **zero entry**, which
  under the cache's own rules does not mean "nothing was stored" — it means *this analyzer
  analysed exactly these bytes at this path and found nothing*. Reproduced on all four
  payloads: **codefit reported score 100 and no SEC-001 for a file that leaks a credential.**
  That is the exact failure the cache's contract (`docs/specs/finding-cache.md` R1) and ADR
  0050 exist to prevent, arriving through the component built to keep the honest full scan
  affordable.

  **Scope, stated at its real size.** The cache is **opt-in** — `config.Cache.Enabled` has no
  default and `codefit init` writes no `cache:` section — so a project that never turned it on
  was never exposed. But `.codefit/cache` is an ordinary directory inside the user's project,
  and anything that leaves valid JSON at `<generation>/<key>.json` is enough: a stray `{}`, an
  editor or sync or backup artifact, a half-restored backup, another tool. There is no race to
  win and no exotic precondition, and the result is **permanent and silent** — nothing
  re-analyses that file until its bytes or the codefit binary change.

  **The fix:** the entry is now **self-describing**. `Set` stamps the key into the entry and
  `Get` verifies it, so every payload that cannot prove it belongs to the key being read — the
  four above, any shape nobody enumerated, and a *well-formed* entry sitting at another key's
  path (a copied or restored file) — is a **miss**, which is just an ordinary analysis. This
  deliberately does not try to enumerate the malformed shapes: that approach only ever closes
  the cases someone thought of. **Entries written by an earlier build have no stamp, so each
  costs one extra analysis and is then rewritten** — no migration code, and the generation
  prune collects the rest. ADR
  [**0053**](docs/decisions/0053-a-cache-entry-names-its-own-key-and-a-hit-must-prove-it.md).

  **No audit rule changed.** The fix can only turn a hit into a miss, and a miss is a real
  analysis producing the real verdict. `rules/`, every provider `coverage.go`,
  `internal/core/dbcoverage`, `dbrules`, `dwrules`, `paradigm` and `crossrules` are untouched,
  no MCP tool gained a parameter, no response field changed, and the skill `codefit init`
  generates needs no change.

### Not yet covered (declared)

- **The cache is bounded by RETENTION, not by SIZE.** It keeps three generations and thirty
  days (see the Added entry above); it does not measure the directory, enforce a byte
  ceiling, or evict by size or least-recent-use. One generation of a very large project is
  still a full copy of that project's entries, and three of them is three. `rm -rf
  .codefit/cache` stays safe and stays the escape hatch — it costs only time, which is
  exactly what distinguishes the cache from the committed baseline.
- **What the dogfood measurement does NOT cover.** It ran on **one** machine (Windows), under
  **one** provider (TypeScript), over **four** projects one person happens to have clones of,
  and it drives the **security sensor** directly rather than the MCP handler — that is the
  price of staying read-only inside somebody's working clone. Two of the four projects produce no
  findings and no surface at all. Nobody else can re-run it without their own clones and their
  own `dogfood.local.json`, which is gitignored by design. It is evidence that both features
  survive real code and it is a real number for the cache; it is **not** a benchmark, a
  representative sample, or a performance guarantee.
- **The 30-day age is the one sweep that can remove a LIVE entry.** A hit does not rewrite
  its entry, so a file untouched for a month is re-analysed once and re-cached. Self-healing
  and in the safe direction, but still a threshold rather than a proof, and **still untuned**:
  the dogfood harness measures a cold run against a warm one inside a single session and says
  nothing about how long an entry should live.
- **One residue survives the prune by design: a stray `.entry-*.tmp` in the CURRENT
  generation.** The atomic write creates its temp file inside the generation directory and
  removes it on every path a running process can take, so this is only what a crash or a kill
  leaves behind. It is not entry-shaped, and the prune refuses to delete anything that is not
  one of the two shapes it writes — the rule that protects a user's own files protects this
  too. It is collected when that generation is superseded and the whole directory goes, so it
  is bounded by the same three-generation window rather than permanent; it is stated here
  because the alternative is someone finding an unexplained file in their cache.
- **The cache effectively stops warming under concurrent tool calls on WINDOWS**, and this was
  proven, not theorised. Go opens files with `FILE_SHARE_READ|FILE_SHARE_WRITE` and **not**
  `FILE_SHARE_DELETE`, so `os.Rename` over an entry file another reader is holding open fails
  with access denied — which is exactly the case the atomic write exists for: two MCP tools
  over one project. `Set` then fails repeatedly and logs a warning per file. **The direction
  is safe** — a failed write is a miss, nothing stale or wrong is ever served, and the audit
  is unaffected — but "degrades gracefully" would understate it: on Windows under concurrency
  the cache does not fill. It is a platform file-sharing question rather than a correctness
  one, and it is not addressed here.
- **A separate, unfixed hole in the same neighbourhood: the empty read.** `os.ReadFile`
  returns `([]byte{}, nil)` when the first read reports EOF, so a source file ever *observed*
  as zero-length — an editor's truncate-then-write, a sync tool mid-copy — would be analysed
  as empty, produce nothing, and have that nothing cached under the key for empty content at
  that path, reporting score 100 for it. **This is not proven to occur.** It is a different
  defect with a different cause (what the walk accepts as a file's content, not what the cache
  accepts as an entry), it wants its own reproduction before it gets its own fix, and it is
  written down here so it is met as a known item rather than rediscovered. See ADR 0053.
  (**Narrowed after this tag** — reproduced and disproved as a cache risk; see the
  "Declared limits" entry under `[0.2.8]` and ADR 0053's superseding note.)
- **The DB dimension is not cached** — neither the DB sensor nor the code×schema cross.
  Their inputs are the configured `database.schema_paths`, not a repository walk, and a
  schema is reconstructed from an *ordered* set of migrations, so a per-file entry is not
  obviously the right unit. Declared, not forgotten.
- **The finding cache has NO test at the MCP-handler level.** It is exercised at the sensor
  (`internal/sensors/security/cache_test.go`) and by the build-tagged dogfood harness, which
  drives the sensor too; nothing under `internal/mcp` turns the cache on — no non-dogfood
  test there so much as mentions it. So the path a real agent takes, `HandleScanAll` /
  `HandleScanSecurity` → `.codefit.yaml` with `cache: enabled: true` → the sensor, is covered
  by neither. The **change scope** is not in the same position: `changed_files` has
  handler-level tests (`internal/mcp/changedfiles_test.go`, `filescope_test.go`) driving both
  handlers, including a Windows-spelled request and a partial scan that must not prune.
- **The 60 000-byte `scan-all` budget is a chosen number, not a measured ceiling.** Two
  different things are easy to confuse here, so both are named. What was *observed through a
  real MCP client*: a **312 692**-character response was **rejected**, and a **40 282**-byte one
  **arrived** — that is the whole of the evidence, and the client's actual cap was never seen,
  only bracketed. What was *computed by a test*: salonpro serializes to **42 012 bytes** under
  the new shape; that response has never been sent through a client. So the declared budget sits
  about **49 %** above the largest response known to arrive, nothing has been measured between
  40 282 and 60 000, and no dogfood project is recorded as having withheld an endpoint (salonpro
  withheld 0), so the withholding path itself is proven only by tests at synthetic budgets.
  **The budget also
  governs only the endpoint lists:** a pathological `db` section could exceed it alone, and
  that path emits the explicit over-budget warning rather than narrowing — narrowing `db` is
  a separate decision, deliberately not made here.
- **codefit still does not know what changed on its own.** If the agent passes nothing, it
  audits everything — by design.

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
