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

- MCP stdio server; TypeScript projects only.
- Security: deterministic rules + surface mapping (IDOR, authz, over-fetching, N+1).
- Database: schema-only OLTP rules across Prisma and SQL-DDL (PostgreSQL, MySQL, T-SQL),
  the DW-0xx analytic family, and the code×schema cross (`scan-all` only).
- `scan-all` three-bucket synthesis, `scan-endpoint`, the baseline, CVEs via OSV,
  `codefit-coverage`, `codefit init`.
- Layer 0 (`changed_files`) and the content-hash finding cache.

**What it correctly refuses:** a **security** audit of a non-TypeScript project.
`providerForLanguage` (`internal/mcp/scanall.go:499`) maps only `typescript`/`ts`/`tsx`;
`HandleScanSecurity` returns `unsupported language %q` and `HandleCoverage` returns
`no coverage manifest for language %q`. **codefit does not produce a false all-clear on a
language it cannot audit** — it errors. For the security dimension, which genuinely needs a
language provider, that is right.

> **Correction (2026-08-05, measured).** An earlier version of this section said the refusal
> "is the intended behaviour and it is already right", full stop. That was **too broad**, and
> the architect caught it. The database dimension needs no language provider at all —
> `HandleScanDB` resolves its parser by the **input's shape** (`.prisma` / `.sql`), never by
> the app language, exactly as ADR 0018 specifies. Measured on a Go project with a Postgres
> schema: `codefit-scan-db` returns `measured=true`, score 90, 2 findings, 2 surface items —
> while `codefit-scan-all` on the same project errors with `unsupported language "go"`. The
> capability works and the front door refuses it. See **P0-5**.

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

### P0-3 — Prove or disprove the empty-read false all-clear

`os.ReadFile` returns `([]byte{}, nil)` on a first-read EOF, so a file ever observed as
zero-length would be analysed as empty and *that nothing* cached — a clean verdict for a file
never read. codefit declared this itself and says, in its own words, **"this is not proven to
occur"** (ADR 0053).

It is the same family as the cache defect fixed before `v0.2.7`, which *was* real. The first
step is a reproduction, not a fix: if it cannot be reproduced, the warning is withdrawn with
the evidence; if it can, it is P0 in earnest.

### P0-4 — Measure the real MCP response ceiling

`ResponseBudgetBytes` is 60 000. That number was **chosen, not measured**. What was observed
against a real client: 312 692 bytes rejected, 40 282 bytes accepted. The actual cap was
never seen, only bracketed, and nothing was measured between 40 282 and 60 000.

If the real ceiling sits below 60 000, `scan-all` fails today for users on mid-sized
projects — the exact defect `v0.2.7` was cut to fix.

### P0-5 — `scan-all` refuses the database dimension over a language it does not need

Found by the architect, 2026-08-05, and measured before it was written down.

The database dimension is **language-neutral by design**: `HandleScanDB`
(`internal/mcp/scandb.go:45-55`) resolves its schema parser by the **input's shape**
(`.prisma` / `.sql`) and by `database.type`, *"not the app language (ADR 0018)"* — its own
comment says so. It never calls `providerForLanguage`.

Measured on a Go project carrying a PostgreSQL schema:

```
codefit-scan-db   → measured=true, score=90, 2 findings, 2 surface items
                    DB-050 "Table without a primary key"  schema.sql:1
                    DB-050 "Table without a primary key"  schema.sql:6
codefit-scan-all  → ERROR: unsupported language "go"
```

`HandleScanAll` resolves the language provider up front and returns a hard error, so the DB
section — which would have run fine — is never reached. Only the code×schema cross
(`crossrules`) genuinely needs the language, because it extracts query filters from
application code; the schema-only rules do not.

**Why this is P0 and not a capability gap.** The criterion at the top of this document is
whether a user is *misled*. A user with a Go or Python backend and a SQL schema is told
`unsupported language`, and concludes codefit cannot audit their project. It can — it audits
their schema today. codefit denies a capability it has, which is a false statement about
itself in the one direction this project treats as unforgivable. It is also made worse by
the generated skill, which tells the agent to call `scan-all` **first**: the agent hits the
error and never learns `scan-db` would have worked.

The fix is a soft path, not a new capability: the language provider is required only for the
sections that use it. Every not-measured path in the DB dimension is already soft by ADR 0020
(a DB misconfiguration must never blank the security audit) — this is the same principle
pointing the other way, and the `scope` block already has the vocabulary to say which
dimensions were measured and which were not.

---

## P1 — the user cannot tell how far codefit reaches

### P1-1 — Say in the README, before install, which dimensions each language gets

Not "only TypeScript is auditable" — that is the over-broad claim P0-5 corrects. The honest
statement is per dimension: **security and its surface mapping are TypeScript-only; the
database dimension works on any project that declares a schema**, because it reads the schema
file, not the application language. The README must say that before someone installs it,
and it can only say it truthfully once P0-5 makes `scan-all` behave that way.

### P1-1b — Three hand-written language switches disagree, and `init` contradicts `scan-all`

Asked by the architect (2026-08-05): *how does codefit know it lacks a capability for a
language?* It does not know. **It has it written by hand**, and there are three lists:

| where | decides by | recognises |
|---|---|---|
| `internal/mcp/scanall.go:499` `providerForLanguage` | language name | **TypeScript only** |
| `internal/mcp/surface.go:193` `providerFor` | file extension | **`.ts`/`.tsx` only** |
| `internal/scaffold/detect.go:74` `detectLanguage` | marker file (`go.mod`, `package.json`) | **Go *and* TypeScript** |

Read the last row again. **`codefit init` detects a Go project, configures it, and installs
the skill** — and that skill tells the agent to call `scan-all` first, which answers
`unsupported language "go"`. codefit welcomes you at one door and throws you out of another.

The same root cause explains why the Go provider is invisible: it is complete and working
(`AnalyzeSecurity`/`AnalyzeSurface`/`AnalyzePractices`, used by codefit's own self-audit and
by `init`'s detection), and unreachable **only because nobody wrote the `case`**. A capability
that exists but is not in the hand-written list is, to a user, a capability that does not
exist.

P0-5 introduces the single source for the supported set and makes the error message read from
it — deliberately, so naming the supported languages does not create a **fourth** list. What
stays here is **convergence**: the other two switches either adopt that source or get a lock
that makes their divergence visible instead of silent.

Sequenced after P0-5 because, once the DB dimension runs without a language, a Go project with
a schema is no longer refused outright — it becomes a *messaging* problem: say which dimension
runs and which does not, never "supported" unqualified.

### P1-1c — The README lists Go under "Supported languages"

`README.md:662`, first row of the table:

> **Go** | Provider + static security/best-practice detectors. codefit audits itself in CI.

Every word is true, and the placement is not: it sits under a heading that reads **Supported
languages**, so a reader concludes they can audit their Go project. They cannot — `scan-all`
and `scan-security` refuse it today, and after P0-5 they will get the database dimension only.

The row has to say what a *user* gets, not what codefit does to itself in CI.

### P1-2 — `report.score_weights` is validated and then ignored

`config.Validate` rejects the map when it does not sum to 100, and **nothing ever reads it**.
`scoring.DefaultWeights()` is hardcoded at both call sites. A user who re-weights their audit
gets their map validated and silently discarded. Either it works or it is rejected with a
message; today it does the worst of both.

### P1-3 — Decide and declare the Go provider's status

The PRD calls `golang/` the "provider de arranque (self-audit)" and its post-v1.0 language
list names Java and Python — **not Go**. So a user-facing Go provider is a *scope change*,
not a gap. Until that decision is made, the honest minimum is a written statement that Go is
not an auditable language today. See P4-1 for the larger question.

### P1-4 — Two owed manifest/ADR entries

- **DW-022** owes its ADR. `VERSIONING.md` says so in its own words — and per the decision
  recorded in P4-3 below, that ADR should now reverse the exclusion rather than confirm it.
- **PRAC-004**'s permanent drop is recorded only in ADR 0056 and the CHANGELOG. It owes a
  manifest entry, which is blocked on there being a Go manifest at all (P1-3).

### P1-5 — Check whether the README promises the HTTP/SSE transport

`--port` is not implemented; the server is stdio only. If the README offers it, that is one
more claim to correct.

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

### P4-1 — Does Go become a user-facing auditable language?

Today the Go provider maps **one** surface category (`authz`) against TypeScript's four, has
six hand-written security rules against TypeScript's rule engine, and has no coverage
manifest. Wiring it into `providerForLanguage` is one line — and would be dishonest, because
what sits behind it returns almost nothing.

Parity is a phase-sized effort, comparable to what Phase 1 did for TypeScript. The argument
*for* it: codefit is written in Go, developed with AI, and cannot audit itself through its
own tools — only through an internal test.

### P4-2 — Documentation-quality rules do not exist anywhere

No rule, no sensor, no surface category audits code documentation. The PRD contemplates it
only obliquely: "comentarios desactualizados" appears inside RF-02, the *agent-reasoned*
review sensor, which is unbuilt (H3). Nothing addresses doc comments on exported API.

This is one of the two **language-agnostic** rule families, which makes it worth more than a
language-specific rule: it pays off in every provider at once.

### P4-3 — Materialized-view refresh: reframed as surface, not impossible

**Decided (2026-08-04), pending its ADR.** `DW-022` was dropped as permanently uncoverable
because refresh cadence lives in scheduler state that static DDL does not carry. `DB-022` is
the same rule on the OLTP side.

That reasoning is right about **affirmations** and wrong about **surface**. codefit cannot
say "this view is stale". It *can* enumerate: *this schema declares N materialized views;
their freshness depends on a scheduler outside the DDL; here they are.* The agent then
resolves it — it can read the cron, the migrations, the CI pipeline and the application
code, none of which codefit sees. That is precisely the division of labour the project
declares (PRD §10).

Consequences, all of them in scope when this is taken:

- It **reverses a recorded permanent exclusion**, so it needs an ADR — which is the same ADR
  `DW-022` already owes (P1-4). The debt gets paid with a better answer instead of a burial.
- It needs a **parser floor first**: `db.View` carries `Name`, `Pos` and `Body` and has **no
  way to say a view is materialized**. Same shape as the floors `DW-021` (`Index.Method`) and
  `DW-020` (`Table.Partitioning`) needed before their rules.
- It should be a **census** — one item per schema carrying the list, not one per view —
  following `DW-005`/`DW-011`/`DW-020`. A schema with forty materialized views must not
  produce forty items.

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
