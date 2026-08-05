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

---

## P1 — the user cannot tell how far codefit reaches

### P1-1a — Say in the README, before install, how far codefit reaches per language

The code refuses honestly for the tools that still refuse (`scan-security`,
`scan-endpoint`, `coverage`). The documentation must say so just as plainly, *before*
someone installs it and points it at a Go or Python repository — and, since P0-5, say
the more nuanced true thing for `scan-all`: DB-dimension-only, schema-only, for a
language with no security provider. The README's "Supported languages" table's Go row
was corrected for the one claim P0-5 falsified (P1-1c below owns the rest of the
table's rewrite).

### P1-1b — Unify the three independent language-resolution switches

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
switches then disagree. What stays here is **convergence**: the other two adopt that source or
keep the locks permanently. P0-5 deliberately did not converge them, and did not resolve the
`init`-welcomes / `scan-all`-mostly-refuses shape for Go — that is entangled with P4-1.

### P1-1c — Rewrite the README "Supported languages" table

`README.md`'s table has Go in its first row:

> **Go** | Provider + static security/best-practice detectors. codefit audits itself in CI.

Every word is true and the placement is not: under a heading that reads **Supported
languages**, a reader concludes they can audit their Go project. Since P0-5 they get the
database dimension only. P0-5 corrected the one claim it directly falsified; the table's
framing, its other rows, and its relationship to the per-tool refusal behaviour (P1-1a) are
unreviewed. The row has to say what a *user* gets, not what codefit does to itself in CI.
Owed a full pass once P1-1b settles what the table should actually claim.

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
