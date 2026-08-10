# Roadmap — priorities, debts, and what is still owed

**Status:** current as of `main` @ `337f158` (2026-08-05). Every claim here was measured
against the repository, not inferred from the PRD.

This document exists because the Phase-3 thread plan lived only in a conversation. A plan
that depends on someone remembering it is not a plan.

## The ordering criterion

Priorities are **not** ordered by how much capability they add. They are ordered by this
question, which the architect set:

> Can someone who downloads codefit today use it without being misled — knowing exactly how
> far it audits in this version, and what it does not audit yet?

That puts **anything that makes codefit lie or break** above **anything that makes its limits
unknowable**, and both of those above **new capability**. A tool whose whole premise is
auditing honesty cannot ship dishonesty about itself.

Capability gaps are not urgent *when they are declared*. An honest "not covered" costs a user
nothing. An undeclared one costs them their trust.

---

## Where codefit actually stands today

**What works, from `main`, right now:**

- MCP stdio server. **Per dimension, not per project:** security and its surface mapping are
  TypeScript-only; the database dimension audits **any** language that declares a schema.
- Security: deterministic rules + surface mapping (IDOR, authz, over-fetching, N+1).
- Database: schema-only OLTP rules across Prisma and SQL-DDL (PostgreSQL, MySQL, T-SQL),
  the DW-0xx analytic family, and the code×schema cross (`scan-all` only).
- `scan-all` three-bucket synthesis, `scan-endpoint`, the baseline, CVEs via OSV,
  `codefit-coverage`, `codefit init`.
- Layer 0 (`changed_files`) and the content-hash finding cache.

**What it correctly refuses:** `codefit-scan-security`, `codefit-scan-endpoint` and
`codefit-coverage` on a non-TypeScript project. `providerForLanguage`
(`internal/mcp/scanall.go`, now a table, `languageProviders`) resolves only
`typescript`/`ts`/`tsx`; `HandleScanSecurity` returns `unsupported language %q` and
`HandleCoverage` returns `no coverage manifest for language %q` for anything else.
**codefit does not produce a false all-clear on a language it cannot audit** — it
errors. That is the intended behaviour and it is already right for those three tools.

`codefit-scan-all` is narrower than that since P0-5: it audits the DB dimension
(schema-only) for **any** language when `database.schema_paths` is configured,
because the DB dimension's schema parser never depended on the language provider —
and refuses only when **neither** a security provider resolves **nor** the DB
dimension runs, naming both missing inputs. `by_dimension.security` is honestly
`null` on that path, not a false all-clear.

> **How this section got here (2026-08-05) — kept because the mistake is the lesson.**
> It used to say codefit refuses "a non-TypeScript project", and that the refusal "is the
> intended behaviour and it is already right", full stop. **Too broad, and the architect
> caught it.** The database dimension never needed a language provider: `HandleScanDB`
> resolves its parser by the **input's shape** (`.prisma` / `.sql`), exactly as ADR 0018
> specifies. Measured at the time on a Go project with a Postgres schema, `codefit-scan-db`
> returned `measured=true`, score 90, 2 findings, 2 surface items — while `codefit-scan-all`
> on the *same* project errored `unsupported language "go"`. The capability worked and the
> front door refused it. P0-5 closed that; the paragraphs above describe today.

**What does not exist:** the review, tests, complexity and practices sensors. Phase 3 is
started, not half-done — see P2.

---

## P0 — codefit lies or breaks. Nothing ships before these.

### P0-1 — CLOSED (PR #113) — the coverage manifest omitted capabilities the PRD promises

**Seven**, not six: the survey missed `DB-201`, and it was the one that changed the design.
The manifest now answers for every rule id the PRD names, and a control derives that promised
set **mechanically from the PRD** so the next omission cannot pass silently. The answer set
has three members, not two — `DB-201` (N+1) is *delivered under another identifier*, and
forcing it into `NotCovered()` would have been a lie about a capability that ships.

The original entry, kept for its reasoning:

Six rule IDs appear in the PRD and **nowhere else**: `DB-021`, `DB-022`, `DB-023`, `DB-032`,
`DB-101`, `DB-102`. They are not built, and they are **not declared as not-covered in any
manifest**. Measured: one occurrence each in `docs/PRD-codefit-v1.4.md`, zero across
`COVERAGE.md`, `internal/core/dbcoverage/`, `internal/core/dbrules/`,
`internal/core/dwrules/` and `internal/core/crossrules/`. (Positive probe: the same search
finds `DB-020` in nine places, so it reaches all of them.)

**Why this is P0 and not a documentation chore.** The manifest is what
`codefit-coverage` returns *to the agent*. A promised capability that is neither built nor
declared absent is a hole the agent cannot see. It will not compensate for a gap it was never
told about.

**The real fix is the control, not the six entries.** `dbcoverage`'s existing guard catches
*phantom* capabilities — prose naming a rule that does not exist. It has nothing pointing the
other way: a rule the PRD promises that the manifest never mentions. That asymmetry is how
these six got through, and adding six lines without closing it just resets the clock.

This is the **only** debt in the entire inventory that was not self-declared somewhere.

### P0-2 — CLOSED (`sql-ddl-phantom-index`) — the SQL-DDL parser dropped a real column

A column named exactly `key`/`index`/`fulltext`/`spatial` whose type is outside the
vocabulary — the real `film.fulltext` column in Pagila — used to be misread as MySQL's
inline secondary-index shorthand.

The claim above ("a phantom zero-column index is fabricated") was already stale by the time
it was investigated: the `tsql-alter-add-constraint` FABRICATION GUARD (2026-07-31) had
closed that specific fabrication as a side effect four days before anyone re-verified this
entry. What remained was a silent **drop** (`Complete=false`, an honest abstention, not an
invented structure) — still a real defect, since Pagila's fixture had been shrunk to omit
`film` entirely to dodge it, making the corpus unable to measure its own known limit.

Fixed: the classifier now reads `<kw> <unmapped-type> [modifiers]` with no parenthesized
column list as a COLUMN. `film` is restored to the Pagila fixture and parses with all 14
columns, `Complete()==true`. A narrower residual remains and is declared, not hidden: `<kw>
<unmapped-type>(args)` (e.g. `fulltext tsvector(10)`) is structurally identical to a named
inline index and still fabricates one — locked as a characterization test. See
[ADR 0058](decisions/0058-a-declared-limit-can-go-stale-and-nobody-re-verifies-it.md) for the
full history, including the lesson this leaves behind: a declared limit needs its own
re-verification, or it goes stale exactly like an undeclared one.

### P0-3 — CLOSED (`empty-read-hole`) — the declared empty-read false all-clear: cache half disproved, walk half narrowed and withdrawn as a cache risk

`os.ReadFile` returns `([]byte{}, nil)` on a first-read EOF, so a file ever observed as
zero-length would be analysed as empty. codefit declared this itself and said, in its own
words, **"this is not proven to occur"** (ADR 0053), asking for a reproduction before a fix.

Run against the real sensor and the real cache on unmodified `main` @ `ac91109`:

```
PROBE pass1 (file empty)   findings=0  audited=1     <- "nothing" gets cached
PROBE pass2 (real content) findings=1  SEC-001 high leak.ts:1
PROBE control (no cache)   findings=1
*** NOT REPRODUCED: pass2 matches the uncached control ***
```

**Not reproduced, and disproved structurally.** The cache key is
`sha256(analyzer identity ‖ path ‖ content)` — empty content and real content at the same path
hash to two different keys, so a pass over real content is always a MISS. A poisoned empty
entry can never be served for non-empty content: not "did not happen", **cannot happen**, by
the key formula ADR 0053 already specifies. Locked with
`TestCache_EmptyReadNeverPoisonsLaterRealContentAtSamePath`
(`internal/sensors/security/cache_test.go`), mutation-proved (edit the key to ignore content,
the guard fails exactly as predicted; restore it, green again).

**What remains true is not a cache defect.** `os.ReadFile` reporting an empty read on a file
mid-write is real, present with or without the cache — codefit reports what it read — and it
is **transient**: the next scan reads the real bytes. There is no sound fix (codefit cannot
tell a legitimately empty file from one mid-write), so none is attempted. No behaviour
changed: this closes as a declared limit narrowed to what is true, not as a code fix. See ADR
0053's superseding note for the full record.

### P0-4 — CLOSED, in part (`response-budget-calibrated`) — the response budget is now measured by bisection, not chosen; the structural cap is the remaining half

`ResponseBudgetBytes` was 60 000, **chosen, not measured** — 312 692 bytes rejected, 40 282
accepted, and nothing measured between them.

Measured by bisection against a real MCP client (Claude Code, 2026-08-09), driving controlled-
size responses cut from a real 317-file project over stdio: **64 097 bytes ACCEPTED, 74 195
REJECTED** ("exceeds maximum allowed tokens") — the real ceiling is bracketed there, narrower
than the ~75 KB the old derivation assumed.

Fixed: `ResponseBudgetBytes` moves to **40 000** — 62% of the largest observed acceptance, with
room for roughly a 60% increase in token density before approaching the rejected end of the
bracket. See [ADR 0062](decisions/0062-the-response-budget-is-calibrated-by-bisection-not-chosen.md)
for the full arithmetic, the stated assumption the number rests on (bytes are a content-dependent
proxy for the client's real token limit), and the measured consequence: the same real project
that fit entirely at 60 000 (0 withheld) now withholds 19 of 174 endpoints (5 actionable, 14
frontier_pending) at 40 000 — a genuine, declared, user-visible behaviour change, not a free
tightening.

**Explicitly NOT done here, and the reason this closes only "in part":** a byte budget cannot
guarantee a token limit, no matter how carefully the byte number is calibrated. The structural
answer — a hard cap on entries per bucket, so response size stops being a function of project
size at all — is not built. That is P0-4's remaining item, carried forward rather than reopened
under a new number, because it is the same unresolved half this priority named from the start.

### P0-5 — CLOSED (`scan-all-db-without-language`) — scan-all refused the DB dimension over a language it did not need

`codefit-scan-all` hard-errored `unsupported language %q` for any project whose
language had no security provider — including a Go project with a fully configured
`database.schema_paths`, which the DB dimension's schema parser could measure without
ever consulting `req.Language`. The security sensor's unconditional hard error sat
~30 lines before the DB section ever ran.

Fixed: security now runs only `if secRan` (a provider resolves); the baseline's
`scanned` set became empty-by-default opt-in (a real corruption — a security-owned
baseline item could be wrongly pruned by a DB-only pass — was reproduced for real on
a fixture before being fixed); nothing-measurable (neither dimension runs) is now a
refusal naming both missing inputs, not a null-filled 200; the supported-language set
gained a single source (`languageProviders`) with three regression locks against the
three independent language-resolution switches drifting further apart; and
`ScanAllResponse` gained an always-present `security` section (`measured`/`note`)
mirroring `db`'s shape. See
[ADR 0059](decisions/0059-security-soft-dimension-in-scan-all.md) for the full
decision record, including the accepted consequence (a narrowed scan on a Go project
whose changed files exclude its schema now errors) and the deliberate asymmetry vs.
`codefit-scan-security`/`codefit-scan-endpoint`, which are unchanged.

The evidence that opened this entry, kept because it is what a measurement buys — the
architect asked why the DB dimension was refused, and the probe answered before any prose
did:

```
codefit-scan-db   → measured=true, score=90, 2 findings, 2 surface items
                    DB-050 "Table without a primary key"  schema.sql:1
                    DB-050 "Table without a primary key"  schema.sql:6
codefit-scan-all  → ERROR: unsupported language "go"
```

Filed as P0 rather than a capability gap on this document's own criterion: a user with a Go
backend and a SQL schema was told `unsupported language` and would conclude codefit could not
audit their project. It could. codefit denied a capability it had — a false statement about
itself, in the one direction this project treats as unforgivable. The generated skill made it
worse by telling the agent to call `scan-all` **first**, so the agent hit the error and never
learned `scan-db` would have worked.

**Explicitly NOT done here** (see P1-1b/c and P4-1 below): unifying the three
language-resolution switches beyond the divergence locks, resolving the
`init`-welcomes/`scan-all`-refuses contradiction for Go beyond "the DB dimension now
measures it", and wiring `golang.New()` into `providerForLanguage` (a full
user-facing Go security provider).

### P0-6 — CLOSED (`baseline-write-gate`) — the baseline was written before codefit knew the response would arrive

Found while measuring P0-4 (above): `codefit-scan-all` persisted `.codefit-baseline` while
still building its own response, ~100 lines before every check codefit performs on its own
output (`scoring.MissingWeights`, `ScopeBlock.Validate()`, `fitToBudget`'s `stillOver`).

Reproduced live against a real MCP client, on a fresh project:

```
codefit-scan-all → the client REJECTS the response:
    "result (312,692 characters) exceeds maximum allowed tokens"
codefit had already written .codefit-baseline — 66,227 bytes, 373 items
the retry reports: "0 new, 373 known"
```

373 findings recorded as seen by a reader who received nothing. The census that found this
measured its own test coverage: breaking delivery on purpose turned 20+ tests red, every one
on "the handler returned an error", and not one on "the baseline was written anyway".

Fixed: `diffBaseline` now only computes the diff; the save moved to the one caller that also
knows the budget-fitting outcome, and runs only when `MissingWeights`, `ScopeBlock.Validate()`,
and `stillOver` have all passed. R2's asymmetric scope guard (the `gone` direction was
protected, `known` was not) closed the same way, concretely reachable through the
code×schema cross rules' schema-anchored fingerprint under a narrowed scan. See
[ADR 0061](decisions/0061-the-baseline-write-is-gated-on-every-check-codefit-can-perform.md)
for the full decision record, including the declared (not solved) residual — MCP defines no
delivery acknowledgement, so this is a mitigation of the reachable instance, not the cure.

**Explicitly NOT done here:** deriving `known` from confirmed delivery instead of storing it
(the structural cure, invariant I3's full design); fixing the budget's unit mismatch (bytes
vs. the client's tokens, still P0-4); and `codefit-baseline-prune`'s same-shaped
compute-then-save-before-returning, on its own human-triggered path — recorded, not fixed.

---

## P1 — the user cannot tell how far codefit reaches

### P1-1a — CLOSED (`readme-per-dimension-reach`) — say in the README, before install, how far codefit reaches per language

The code refuses honestly for the tools that still refuse (`scan-security`,
`scan-endpoint`, `coverage`). The documentation must say so just as plainly, *before*
someone installs it and points it at a Go or Python repository — and, since P0-5, say
the more nuanced true thing for `scan-all`: DB-dimension-only, schema-only, for a
language with no security provider. The README's "Supported languages" table's Go row
was corrected for the one claim P0-5 falsified (P1-1c below owns the rest of the
table's rewrite).

**Fixed.** `README.md`'s `## Status` section — the first thing a reader sees, before
`## Install` — now opens with a "Reach is per dimension, not per project" paragraph:
security/surface mapping is TypeScript-only and `scan-security`/`scan-endpoint`/
`coverage` refuse anything else; the database dimension resolves its parser by the
schema file's shape (`.prisma`/`.sql`), never by the app's language, so a Go or Python
project with `database.schema_paths` gets it audited. The "What codefit covers today"
section's `Languages` bullet, which used to read as a whole-product language list
(`TypeScript / TSX ... and Go`), now says the same per-dimension thing and points at
the language-independent database-dimension bullet below it. Verified against
`internal/mcp/scanall.go` (`languageProviders` resolves only
`typescript`/`ts`/`tsx`) and `internal/mcp/scandb.go`'s `HandleScanDB` (resolves the
schema parser "by the INPUT's shape (.prisma / .sql), not the app language (ADR
0018)" — a comment in the code itself, and `req.Language` is never read for parser
selection). No code changed.

### P1-1b — CLOSED (`language-capability-source`) — Unify the three independent language-resolution switches

Asked by the architect (2026-08-05): *how does codefit know it lacks a capability for a
language?* It does not know. **It has it written by hand**, and there are three lists, each
deciding by a different signal:

| where | decides by | recognised, before P0-5 |
|---|---|---|
| `internal/mcp/scanall.go` `providerForLanguage` | language name | **TypeScript only** |
| `internal/mcp/surface.go` `providerFor` | file extension | **`.ts`/`.tsx` only** |
| `internal/scaffold/detect.go` `detectLanguage` | marker file (`go.mod`, `package.json`) | **Go *and* TypeScript** |

Read the last row again. **`codefit init` detected a Go project, configured it and installed
the skill** — and that skill tells the agent to call `scan-all` first, which answered
`unsupported language "go"`. codefit welcomed you at one door and threw you out of another.

The same root cause explains why the Go provider is invisible: it is complete and working
(`AnalyzeSecurity`/`AnalyzeSurface`/`AnalyzePractices`, used by codefit's own self-audit and
by `init`'s detection), and unreachable **only because nobody wrote the `case`**. A capability
that exists but is not in the hand-written list is, to a user, a capability that does not
exist.

P0-5 gave `providerForLanguage` a single-source table and three regression locks that keep the
switches' *current* agreement from drifting further in silence — measured: smuggling a `"go"`
entry into the table turns Lock A **and** Lock B red, the latter precisely because the two
switches then disagree. What stayed open after P0-5 was **convergence**: the other two adopt
that source or keep the locks permanently. P0-5 deliberately did not converge them, and did not
resolve the `init`-welcomes / `scan-all`-mostly-refuses shape for Go — that is entangled with
P4-1.

**Fixed.** `internal/providers/registry` is now the one ordered table all three switches query
(ADR 0064): `providerForLanguage`/`SupportedLanguageNames` (`scanall.go`), `providerFor`
(`surface.go`), and `detectLanguage` (`scaffold/detect.go`) each read it by their own signal —
name, extension, marker file — and none of them builds a concrete provider on its own anymore
(`scaffold/detect.go` drops its `golang`/`typescript` imports entirely). The table also carries
a `Capability()` declaration per provider (rule IDs per family, surface categories, whether a
coverage manifest exists) alongside its `Exposure` (which resolvers admit it), so capability and
exposure are two independent, checkable facts instead of one inferred from the other. Every
answer set stays byte-identical to before this change by construction — Locks A and C needed no
edit and stayed green throughout; Lock B compile-broke exactly where the deleted
`languageProviders` map was named and was fixed by editing only its iteration source. Go stays
registered with `Exposure.SecurityScan`/`SurfaceTools` both `false` (P4-1's wiring is still a
deliberate, later decision) and `InitDetect` `true`.

**Side effect, not a resolution:** `internal/providers/golang/capability.go` is now a real,
tested landing site for a per-rule declaration, which is what P1-4b was blocked on missing (a
place to put PRAC-004's owed manifest entry) — but P1-4b names a *coverage manifest*
(`golang/coverage.go`, `CoverageManifest()`), which this change deliberately does not build.
P1-4b stays open.

### P1-1c — CLOSED (`readme-per-dimension-reach`) — rewrite the README "Supported languages" table

`README.md`'s table had Go in its first row:

> **Go** | Provider + static security/best-practice detectors. codefit audits itself in CI.

Every word was true and the placement was not: under a heading that reads **Supported
languages**, a reader concludes they can audit their Go project. Since P0-5 they get the
database dimension only. P0-5 corrected the one claim it directly falsified; the table's
framing, its other rows, and its relationship to the per-tool refusal behaviour (P1-1a) were
unreviewed. The row has to say what a *user* gets, not what codefit does to itself in CI.

**Fixed, in two steps.** The Go row itself was corrected in `817070e` (P0-5, 2026-08-05):
it now says what a Go *user* gets — the DB dimension only, when they configure a schema,
with `codefit-scan-security`/`codefit-scan-endpoint` still refusing and
`by_dimension.security: null` — not what the Go provider does internally in CI. This
change reviewed the rest of the table for the same class of error and found none: the
TypeScript row names the actual frameworks (Next.js App Router/Server Actions, Express,
Fastify, NestJS) it maps surface for, with no unsupported claim, and the two "Roadmap"
rows (Java/Spring, Python/FastAPI/Django) make no capability claim at all. No table edit
was needed beyond the Go row P0-5 already fixed; P1-1a above closes the surrounding
per-dimension framing the table's heading needed.

### P1-2 — CLOSED (`p1-config-and-owed-entries`) — `report.score_weights` is validated and then ignored

`config.Validate` rejected the map when it did not sum to 100, and **nothing ever read it**:
`scoring.DefaultWeights()` was hardcoded at both `scan-all` call sites
(`scoring.MissingWeights` and `scoring.Compute`). A user who re-weighted their audit got
their map validated and silently discarded.

**Fixed.** `scoring.ResolveWeights(userWeights)` decides which map `scan-all` uses: the
user's `cfg.Report.ScoreWeights`, converted to `findings.Dimension` keys, when it names at
least one entry; `DefaultWeights()` otherwise. An absent key is byte-identical to before
this change, locked against a golden response captured from a real `git worktree` at this
change's base commit (`cfd1ad7`), not a hand-written expectation.

**The declared consequence, handled deliberately:** `scoring.MissingWeights` has existed
since [ADR 0021](decisions/0021-by-dimension-scoring-wired-into-scan-all.md) to catch a
measured dimension with no weight, but could never fire — `DefaultWeights()` names every
declared dimension. A user map is not guaranteed to: `{security: 100}` validates (sums to
100) but omits `db`, and a scan that also measures `db` now surfaces an actionable,
user-worded error naming `db` and pointing at `score_weights`, distinct from the
`codefit internal: ...` wording reserved for a genuine wiring bug.

**Decided and defended:** the sum-must-be-100 validation stays unchanged.
`scoring.Compute` normalizes by the measured dimensions' own weight sum, not by a
hardcoded 100, so the constraint is not load-bearing for the arithmetic — it is kept so a
weight reads as a percentage point, and so validation has one fixed target instead of an
open-ended "just be positive". It is deliberately **not** widened to require every one of
the six declared dimensions: validation cannot know in advance which dimensions a given
project will measure (`db` only runs when configured and in scope), so that completeness
check stays at scan time (`scoring.MissingWeights`), where the actual measured set is
known and the error can name exactly what is missing. See `internal/config/validate.go`'s
doc comment for the full reasoning and the CHANGELOG's `[Unreleased]` entry for the
user-facing behaviour change (⚠️ — a config key that did nothing now does something, and a
partial map that was silently ignored can now error).

### P1-3 — Decide and declare the Go provider's status

The PRD calls `golang/` the "provider de arranque (self-audit)" and its post-v1.0 language
list names Java and Python — **not Go**. So a user-facing Go provider is a *scope change*,
not a gap. Until that decision is made, the honest minimum is a written statement that Go is
not an auditable language today. See P4-1 for the larger question.

### P1-4 — Two owed manifest/ADR entries

- **P1-4a — CLOSED (`p1-config-and-owed-entries`) — DW-022's owed ADR, written as a
  reversal.** `VERSIONING.md` said DW-022's ADR was still owed. It is paid by
  [ADR 0063](decisions/0063-materialized-view-refresh-is-surface-not-a-permanent-exclusion.md),
  which does not confirm the original "permanently dropped" call — it **reverses** it, per
  the decision P4-3 recorded (2026-08-04): codefit cannot **affirm** that a materialized view
  is stale (refresh cadence lives in scheduler state no DDL carries), but it **can** enumerate
  the materialized views a schema declares as **surface** and let the agent resolve freshness
  from the cron, the migrations and the CI pipeline codefit never sees. `DB-022`, the OLTP
  twin, takes the identical reversal in the same ADR. **Decided and recorded, not built**:
  `db.View` (`internal/core/db/db.go`) still carries only `Name`, `Pos` and `Body` — verified
  against the struct, not assumed — with no way to say a view is materialized, the same
  parser-floor shape `DW-021` (`Index.Method`) and `DW-020` (`Table.Partitioning`) each needed
  before their rules; the future rule is a **schema-level census**, one item per schema,
  following `DW-005`/`DW-011`/`DW-020`. `VERSIONING.md`, `COVERAGE.md` and
  `internal/core/dbcoverage/dbcoverage.go` each carry an append-only superseding note pointing
  at the ADR — the original "permanently dropped" text is kept, not deleted, as the record of
  what Phase 2.5 decided and why it changed. No rule, finding, surface item or baseline
  fingerprint changes: `dwrules.All()` stays seven rules, `dbrules.All()` stays fourteen.
- **P1-4b — BLOCKED — PRAC-004's owed manifest entry.** Its permanent drop is recorded only
  in [ADR 0056](decisions/0056-a-practices-rule-affirms-only-what-it-checked-and-prac-004-is-dropped.md)
  and the CHANGELOG; it owes a coverage-manifest entry. **Blocker:** there is no Go coverage
  manifest to put it in — `internal/providers/golang/coverage.go` does not exist — and that is
  entangled with the still-open architect decision on the Go provider's status (P1-3/P4-1).
  Creating a Go manifest to host one entry would pre-empt that decision and is deliberately
  out of scope here; this item stays open until P1-3/P4-1 resolves.

### P1-5 — CLOSED (`readme-per-dimension-reach`) — check whether the README promises the HTTP/SSE transport

`--port` is not implemented; the server is stdio only. If the README offers it, that is one
more claim to correct.

**Checked, no change needed.** `README.md:181` already says
`"**On the roadmap (not yet in \`main\`):** the HTTP/SSE transport;"` — filed under what is
**not** yet available, not offered as a working feature. Verified against
`internal/cli/mcp.go`'s `newMCPServeCmd`: the `--port` flag exists but its `RunE` returns
`"the HTTP/SSE transport (--port) is not implemented yet; use stdio (no --port)"` whenever
`port != 0`, and the flag's own help text says `"(not implemented yet)"`. The claim matches
the code exactly; no README edit was needed for this item.

---

## P2 — finish Phase 3

Phase 3 per the PRD (§25): `codefit-review-code`, `codefit-check-practices`,
`codefit-scan-tests`, plus incremental regression risk and the closing protocol that folds
agent-confirmed findings back into the report. **Done when** `codefit-review-code` produces
an actionable review on a real PlantaLinda PR.

| thread | what it is | state |
|---|---|---|
| **H0** | layer 0 (`changed_files`) + the finding cache | ✅ shipped in `v0.2.7` |
| **H1** | the practices dimension | 🔨 2 of 6 slices — see `docs/specs/practices-dimension.md` |
| **H2** | tests quality + regression risk | ⬜ not started (H0 unblocked it) |
| **H3** | code review — **the phase's close criterion** | ⬜ not started |
| **H4** | the closing protocol | ⬜ not started |

H1's remaining slices: the sensor + `codefit-check-practices` (S3), the **TypeScript** rules
that make it a product (S4), the per-rule severity config (S5), and the mandatory close —
wiring it into `scan-all` (DoD, per ADR 0016).

Version line: Phase 3 targets `0.3.0`; work toward it tags `v0.3.0-alpha.N` (see
`VERSIONING.md`).

---

## P3 — declared capability gaps

Roughly forty open limits across security, the database dimension, the cache and the
transport. **Every one of them is written down with its reason** in `COVERAGE.md`,
`internal/core/dbcoverage/dbcoverage.go`, the ADRs or the CHANGELOG's declared-limits blocks.
They constrain what codefit finds; they do not mislead anyone about it.

They are not enumerated here on purpose — duplicating them would create a fourth place to
drift. The manifests are the source; this document points at them.

The ones most worth revisiting first, by user impact:

- NestJS authorization detection sees `@UseGuards` only — module-bound and global guards are
  invisible, so `known_authz_detected` reads false across such apps.
- PII coverage is partial and explicitly an open design question, not a settled exclusion.
- The finding cache barely warms under concurrent tool calls on Windows.
- The finding cache has no test at the MCP-handler level.

---

## P4 — scope decisions owed to the architect

### P4-1 — Does Go become a user-facing auditable SECURITY language?

Since P0-5, "does Go become auditable" is no longer all-or-nothing: `scan-all`
already measures the DB dimension for a Go project with a configured schema. What
remains is narrower and still a real scope decision: does `providerForLanguage`
(`internal/mcp/scanall.go`'s `languageProviders` table) ever gain a `"go"` entry so
`codefit-scan-security`/`codefit-scan-endpoint`/`by_dimension.security` become real
for Go code? Today the Go provider maps **one** surface category (`authz`) against
TypeScript's four, has six hand-written security rules against TypeScript's rule
engine, and has no coverage manifest. Wiring it in is one line — Lock A
(`internal/mcp/language_source_test.go`) exists specifically to turn that one line
into a failing test instead of a silent slide, so this stays a decision, not an
accident.

Parity is a phase-sized effort, comparable to what Phase 1 did for TypeScript. The argument
*for* it: codefit is written in Go, developed with AI, and cannot audit itself through its
own tools — only through an internal test.

### P4-2 — Documentation-quality rules do not exist anywhere

No rule, no sensor, no surface category audits code documentation. The PRD contemplates it
only obliquely: "comentarios desactualizados" appears inside RF-02, the *agent-reasoned*
review sensor, which is unbuilt (H3). Nothing addresses doc comments on exported API.

This is one of the two **language-agnostic** rule families, which makes it worth more than a
language-specific rule: it pays off in every provider at once.

### P4-3 — CLOSED as a decision (`p1-config-and-owed-entries`), open as an implementation — materialized-view refresh: reframed as surface, not impossible

**Decided (2026-08-04), recorded (2026-08-10).** `DW-022` was dropped as permanently
uncoverable because refresh cadence lives in scheduler state that static DDL does not carry.
`DB-022` is the same rule on the OLTP side. That reasoning is right about **affirmations** and
wrong about **surface**. codefit cannot say "this view is stale". It *can* enumerate: *this
schema declares N materialized views; their freshness depends on a scheduler outside the DDL;
here they are.* The agent then resolves it — it can read the cron, the migrations, the CI
pipeline and the application code, none of which codefit sees. That is precisely the division
of labour the project declares (PRD §10).

**The ADR this decision owed is written:**
[ADR 0063](decisions/0063-materialized-view-refresh-is-surface-not-a-permanent-exclusion.md)
(P1-4a, closed above). What P4-3 named as consequences "in scope when this is taken" are the
ADR's own decisions now, not open questions:

- It **reverses a recorded permanent exclusion** — recorded in the ADR, which pays the debt
  `DW-022` owed (P1-4a) with a better answer instead of a burial.
- It needs a **parser floor first**: `db.View` carries `Name`, `Pos` and `Body` and has **no
  way to say a view is materialized** — verified against the struct, not assumed. Same shape
  as the floors `DW-021` (`Index.Method`) and `DW-020` (`Table.Partitioning`) needed before
  their rules. **Not built.**
- It should be a **census** — one item per schema carrying the list, not one per view —
  following `DW-005`/`DW-011`/`DW-020`. A schema with forty materialized views must not
  produce forty items. **Not built.**

**What remains open, carried forward as its own future slice, not by this entry:** the parser
floor on `db.View` and the census rule itself. Building them is sized like what `DW-020`/
`DW-021` each took on their own — this document does not schedule that slice yet.

### P4-4 — Algorithmic complexity stays deferred

The `complexity` dimension is declared, weighted 10, and has **no sensor**; it reports as not
measured on every response. The PRD defers it post-v1.0 with a concrete reason: measuring how
an algorithm scales requires *executing* it, which does not fit the deterministic MCP flow.
Not a debt to pay — a decision to revisit after v1.0.

---

## How this document is maintained

It declares state, so the reflect-today rule applies: it describes what `main` does, never
what a phase will do when it closes. When a priority is taken, it moves or leaves; when a new
debt is declared, it lands in the tier its user impact puts it in — not the tier that is
convenient.

It is a **pointer**, not a fourth copy. The sources stay authoritative: `COVERAGE.md` and the
coverage manifests for what is detected, the ADRs for why, `VERSIONING.md` and `CHANGELOG.md`
for what shipped, `docs/specs/` for design contracts.
