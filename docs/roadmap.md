# Roadmap — priorities, debts, and what is still owed

**Status:** current as of `main` @ `4bdd0f0` (2026-08-17). Every claim here was measured
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

### P0-7 — CLOSED (`response-asserts-only-what-it-established`) — the response asserted two negatives it had never measured

Found by dogfooding codefit against a real 45-migration Flyway project. Two defects that look
unrelated and are the same law broken twice: **a response may assert a negative claim only when
that claim was ESTABLISHED by measurement, never inferred from a proxy that also has innocent
causes.**

*The unread-schema note (D2).* ADR 0044's floor inferred "codefit did not see what this file
declares" from "no position in the model names this file". That absence is also produced by a
migration whose every statement is a guarded re-declaration of schema another migration already
declares — reduced correctly, and the correct reduction adds nothing, so it *cannot* leave a
position — and by a file of pure `INSERT`/`GRANT`. Measured, before and after, by running the
real sensor over a copy of the real project and the "before" case on a detached worktree at the
pre-change commit:

```
before   13 of 45 migrations under "codefit read NOTHING from this file"   37 tables
after     3 of 45                                                          37 tables
```

The identical table count is the control: the fix reclassifies, it does not change the model.
The 3 that remain are genuinely unseen content (`ALTER COLUMN … TYPE`, `SET DEFAULT` +
`DROP NOT NULL`, and one CTE-prefixed statement).

*The budget note (D3).* `budgetNote` derived its fit claim from the *withheld endpoint count*,
not from the response's size — which `fitToBudget` had already measured by serializing the
response and passed in as an argument the zero-withheld branch returned before reading. A
project with a database and no security provider (zero endpoints, large `db.surface`) exceeded
its 40 000-byte budget while asserting it fit.

Fixed: a per-source **statement census** in the neutral model
(`db.Schema.Sources`) gives the sensor measured evidence instead of silence, fail-closed for any
parser that does not fill it — a guard locked by its own test, because an invariant
with no mechanical control is an intention, not an invariant;
the blind-file list is enumerated in full while the benign lists stay capped; `Measured` goes
false when every configured source is traceless *whatever the reason* (without that widening the
fix would have made a seed-only glob look like a clean audit); and the budget note reads the
measurement it was already handed. See
[ADR 0068](decisions/0068-a-negative-claim-needs-positive-evidence-the-statement-census-and-the-measured-budget.md),
which supersedes ADR 0044 §2.5's declared over-report for the DML/permission family.

**Explicitly NOT done here:** the structural per-bucket cap for `db.surface` — **still P0-4's
remaining half**. The budget governs the endpoint lists only, so a DB-heavy response exceeds it
with nothing to withhold; extending withholding there needs a stable ranking for db surface
items, a "fetch the rest" tool for a named db item, and a per-bucket count/withheld contract.
The note now declares that limit instead of hiding it, which is a mitigation, not the cure.
Also untouched, each its own change: the unbudgeted `codefit-coverage` response; Go's
`looksSecret` false positives on enum constants. (`summary` counting security only was the
fourth item on that list; it is now **P0-8**, below. `codefit init`'s detection message was
the fifth; it is now **P0-10**, below — and the message turned out to be the symptom of the
refusal itself.)

### P0-8 — CLOSED (`summary-counts-every-dimension`) — the summary counted one dimension and called itself the summary

`codefit-scan-all`'s `summary` carried four unqualified counts — `endpoints`,
`deterministic_findings`, `surface_items`, `certain_concerns` — every one of them computed
from the **security** sensor's result. Nothing in the response, the tool description or the
generated skill said so. An agent that skims the summary first (which is what a summary is
for) read a project-wide zero over evidence the same response was carrying.

Two facts make it a false all-clear rather than an incomplete one. A project whose language
has no registered security provider gets zero from `secRes` while the DB dimension runs
anyway — its parser is chosen by the schema file's shape, not by the language. And every
DW-0xx rule is surface-only while the score counts deterministic affirmations only, so a
warehouse schema yields `by_dimension.db` high **and** `summary.surface_items: 0`. Two
independent all-clears over the same schema. Found in dogfood: `summary.surface_items: 0`
beside 62 mapped DB surface items.

It was a CLASS, not a field — all four counts had the identical defect — and it is
invariant **I4** (`docs/specs/audit-protocol.md`): a partial result must declare itself
partial. The harm is **I2**: a zero that means nobody looked.

What landed: `summary` is per dimension (`summary.security`, `summary.db`, a derived
`summary.totals`, and a `summary.note`), each count declaring which dimension it counted, a
`null` sub-block for a dimension nobody measured, and both agent-facing surfaces teaching the
shape. Breaking — `summary.security.*` carries the old values verbatim. See
[ADR 0069](decisions/0069-the-scan-all-summary-declares-the-dimension-of-every-count.md)
and `CHANGELOG.md`.

**Named, then built (partially):** I4's specific completeness gap is now closed —
`internal/mcp/summary_measured_completeness_test.go` asserts, on the real `HandleScanAll`
response, that every dimension `score.by_dimension` reports as measured has a non-null
`summary` sub-block under the same wire name (the value half), and separately parses
`internal/core/findings` and `internal/mcp` with `go/ast` to require every
`measured = append(measured, findings.X)` wiring site to have a matching `ScanAllSummary`
JSON tag (the wiring half, static — no sensor needed, so it catches a forgotten summary field
the moment the append site is written, before any fixture would exercise it). Both halves were
proven to fail correctly: dropping a measured dimension's summary block, and adding a third
dimension to `measured` without extending `ScanAllSummary`, both turn the control red.

**Still named, not built:** a counted census (5 of 6 `docs/specs/audit-protocol.md` invariants
— I1 through I5 — already have at least a partial or instance-scoped control; only **I6** has
none) confirms, rather than shrinks, this entry's original "its own, larger change" framing:
**zero of the six** invariants has the registry-level meta-control ("a registry mapping
invariant → control, with a test that fails when an invariant has no control") that
`docs/specs/audit-protocol.md`'s "How the protocol is enforced" section describes. I4's
completeness control closes one instance; the registry itself remains open.

**Deliberately deferred, not an oversight:** `ScanAllSummary` still carries slots only for the
dimensions measurable today (`security`, `db`), not all six the way `score.by_dimension`
already does. Six-slot summary symmetry was judged a contract change not worth taking in this
change; a dimension that is never measured is protected by nothing here.

**Still open, unchanged by this:** the structural per-bucket cap for `db.surface` (P0-4's
remaining half). A correct `summary.db.surface_items` makes a DB-heavy response's size
problem more visible; it neither causes nor fixes it.

---

### P0-9 — CLOSED (`implement-rf-10-test-severity`) — the PRD promised a configurable test severity; the code hardcoded one mode and it was not the default

RF-10 has stated since PRD v1.3 that "findings de seguridad en archivos de test se degradan
a `info` (configurable)", and §14 spelled out what configurable meant:
`sensors.security.test_severity: info | downgrade | keep`. The code shipped neither half. It
hardcoded `downgrade` — critical→high, high→medium, and so on — and the key `test_severity`
existed in **zero** Go files. `CLAUDE.md` restated RF-10 faithfully, so the doctrine file and
the PRD both described behaviour the binary did not have.

It is a lie of the mild kind and that is exactly why it sat: `downgrade` is one of the PRD's
own three sanctioned modes, so the code was not doing something arbitrary — it was doing one
real mode, permanently, and not the one the requirement chose as the default. Blocking was
identical under either reading (nothing maps back up to critical), so no gate ever caught it.
What differed was the SCORE: a critical secret in a test cost the security dimension 10
points that RF-10 says it should not cost. And nothing was configurable at all.

No ADR had ever revised the requirement. The deviation was undecided drift, not a deliberate
decision, which is what made correcting the documents the wrong repair: it would have
enshrined the bug in three places and contradicted the PRD that `CLAUDE.md` itself names the
source of truth.

What landed: the full enum with `info` as the default, validated with a located error;
`keep` accepted (refusing a PRD-named mode would override the developer) with its
blocking consequence stated by one warning per run, at materialisation, only when a finding
actually survived at critical; the key on a security-specific config type so the other five
sensors cannot silently accept it, with a reflection lock naming them if that regresses; and
an AST census tripwire over `Report.IncludeInfo`, whose being unread is what makes forcing
findings to `info` safe. Nine RF citations corrected in the same change (seven RF-11→RF-10,
two RF-10→RF-08). See
[ADR 0070](decisions/0070-path-criticality-is-configurable-and-reaches-only-the-security-sensor.md)
and `CHANGELOG.md`.

**Named, not built:** RF-10 says criticality weights "cada finding", and only the security
sensor applies it. The DB sensor does not, because a project-relative path glob classifies a
schema FILE rather than the table a finding is about, and "a test table" is not a question
RF-10 answers. Declared in ADR 0070 and in `internal/sensors/db/doc.go`; open.

### P0-10 — CLOSED (`init-never-refuses-always-declares`) — init refused a project over a field the audit never reads, with a message naming files that cannot help

`codefit init` exited non-zero and wrote nothing when no marker file resolved a provider,
with a message listing `go.mod, package.json, pyproject.toml/requirements.txt,
pom.xml/build.gradle`. That list is `config.allowedLanguages` (four languages), not the
provider registry (two). Only `go.mod` and `package.json` could ever succeed; `tsconfig.json`
can too and is **not named**; the other four are named and **cannot help** — creating one
changes nothing. A Java project holding a `pom.xml` was told to create a `pom.xml`.
Reproduced with the real binary on three fixtures before the fix.

The message was the symptom. The refusal was the defect: `project.language` is validated and
then read by no production sensor or handler, and a Python project declaring
`database.schema_paths` is already fully audited (P0-5, committed as
`internal/mcp/scanall_dbonly_test.go`). So init withheld a config over a field the audit
never reads, on a project the DB dimension could have audited — the same invariant-I2 shape
P0-5 closed in `scan-all`, still live in `init`, against `CLAUDE.md`'s autonomy principle.

Three releases of drift went unnoticed because the only lock asserted `err != nil` and
nothing about the message.

What landed: `config.LanguageUndetected`, admitted by validation (`""` still rejected);
`Detect` returning it with an EMPTY `path_criticality` rather than borrowed globs, so no path
is classified and RF-10's re-weighting never fires; `registry.InitDetectMarkerFiles()`, so no
user-facing text spells a marker name by hand; one shared db-only clause interpolated by both
the undetected and the registered-but-unexposed statements; and the deletion of
`RenderSkill`'s `"" → typescript` fallback, which fabricated a language in the FIRST artifact
an agent reads. `internal/mcp`'s Lock C was derived from a hardcoded marker map that has no
entry for the ABSENCE of a marker — it would have stayed green while covering nothing — and
is now driven from `registry.All()` plus a no-marker root. See
[ADR 0071](decisions/0071-init-never-refuses-over-language-it-declares.md) and `CHANGELOG.md`.

**Rejected, and not re-openable:** adding Java and Python to the registry. It moves the line
without removing it — Ruby, PHP, C#, Rust and every future language stay refused.

**Named, DECLARED, not built — the second gap. The UNGATE half is now CLOSED; detection is
not.** `internal/scaffold/config.go` used to gate the whole `database:` block, `schema_paths`
included, behind a detected ORM — the one DB field **zero** production code reads — which was
wrong in both directions at once. It went silent for a drizzle/typeorm project, which received
a `database:` block holding only `orm: drizzle` (configuring nothing) *and* no gap declaration,
because the declaration keyed on the same ORM; and it would have spoken for a Flyway project
the moment detection filled `schema_paths` with no ORM beside it. The gate now follows
`len(SchemaPaths) > 0` in all three sites, and both locks moved with it — their counter-cases
each set ORM *and* SchemaPaths, so the predicate move was invisible to the suite that was
supposed to guard it. See
[ADR 0073](decisions/0073-the-config-gate-follows-what-the-audit-reads.md); this closes explore
finding 5 (the lock was on the wrong predicate).

**CLOSED — SQL migration directories are detected, and only ever written when PROVEN.**
Discovery is language-independent (it no longer sits behind language detection, which used to
end `Detect` before any schema enrichment ran on exactly the projects that needed it), walks 6
directory levels while read depth stays 1, and promotes a directory only when its apply order
is proven from the filenames, the **real** parser reconstructs ≥1 table from it, and it is the
only directory that proved. Everything else — golang-migrate naming, a set that reconstructs
nothing, two proven candidates — gets the block **commented with the real path and the
reason**, and the invented `"db/migrations"` placeholder no longer appears in a config where
codefit found a real path. See
[ADR 0074](decisions/0074-init-writes-a-database-block-it-can-prove.md).

**The baited lock did its job and was RETARGETED, not deleted.**
`TestGenerate_SkillClaimHoldsForBaitedMigrationDir` — a fixture holding both an unregistered
build manifest and a real Flyway-shaped migration directory — went **red** the day detection
landed, exactly as designed, forcing the generated skill's "carry NO such key" claim to be
revisited rather than quietly falsified. It keeps its name and its baited fixture; its body is
now a two-directional equivalence between the config and the skill one run wrote.

**Still open, and separable: `flywayOrderedSQL` orders Flyway naming only.** golang-migrate's
`1_init.up.sql` cannot be ordered by proof (`10_x` sorts before `1_init`), so such a directory
is never written live — it is named and explained instead. That is the safe direction and it
does not block anything: the strictness gate lives beside the regex that owns it, so extending
the resolver widens `init` automatically with no scaffold change.

**Still open, and DECLARED: the SQL dialect is never measured.** A proof with no
`database.type` set runs under the PostgreSQL binding (**P0-12**, filed below — there is no
sniff). Mitigated, not fixed: a live block carries a commented `type:` line directly above
the key, and the report names the dialect the proof ran under. Measured and locked: a
MySQL-flavoured set reconstructs **zero** tables under that binding, so it fails the proof gate
and is commented rather than written live.

### P0-11 — CLOSED (`a-configured-schema-path-always-leaves-a-trace`) — a configured schema path that resolved to nothing scored 100

The unread floor counted resolved **files** — a quantity the resolver itself decides — so a
configured `database.schema_paths` entry that resolved to no file was subtracted from both sides
of its predicate and vanished. Measured live on `main`: a path naming a directory holding one
`.go` and zero `.sql` (the ordinary golang-migrate embed layout) returned
`{"findings":null,"measured":true,"score":100,"surface":null}`. Worse, and the reason this is a
P0 rather than a curiosity: with one real path beside one empty one the scan was **legitimately
measured** and the empty path was named **nowhere** — a partial result that does not declare
itself partial. The first shape at least looks suspicious; the second looks completely normal.

Fixed: the unit of account moves from the resolved file to the **configured path**, so a path
that resolved to nothing reaches the floor as a first-class entry and the note names it, states
the consequence and states the action. The dead `total > 0` guard is deleted rather than kept
"defensively" — it is what hid the bug, and deleting it makes the degenerate case fail closed.
See [ADR 0072](decisions/0072-a-configured-schema-path-always-leaves-a-trace.md) and
`CHANGELOG.md`.

**Named, DECLARED, not built — two follow-ups.**

1. **`ScanDBResponse.Score` is an `int` without `omitempty`,** so a not-measured
   `codefit-scan-db` response serialises `"score": 0` beside `"measured": false` — a zero that
   reads like a measurement. It is pre-existing and byte-identical before and after this change,
   but this change makes it **fire more often**, because the total zero-resolution case now lands
   on exactly that shape. Moving it to a pointer is a JSON contract change that deserves its own
   controls; bundling it here would have made a failing control ambiguous. Open.
2. **Nested schema trees.** `flywayOrderedSQL` lists one level deep, so a directory whose `.sql`
   sit in a subdirectory resolves to zero files. The *lie* is closed (it reports not-measured with
   the path named); the *capability* is deferred, because recursing without a cross-directory
   ordering rule would have to pick an order silently — the same trap one level down that the
   golang-migrate naming already sets. Declared at the resolver. Open.

### P0-12 — the SQL dialect is never measured, and nothing sniffs it

`sqlDialectParser("")` binds the **PostgreSQL** parser when `database.type` is absent. Nothing
measures which dialect the DDL actually is, and nothing ever has — this entry exists because a
prior citation pointed at a sniff that does not exist, and an undeclared residual is the defect
this board is ordered by.

**Why it is not worse than it looks.** Measured while building the proof gate: a MySQL-flavoured
set (backticks, `AUTO_INCREMENT`, `ENGINE=`) reconstructs **zero** tables under that binding, so
it fails `init`'s ≥1-table proof and is commented rather than written live. The dangerous shape —
a live block over a partly-wrong model — needs DDL that reduces under the wrong binding and means
something different there. Not yet exhibited, not yet ruled out.

**Why it is not closable by declaring harder.** Three artifacts already declare it (a commented
`type:` above the key, a report sentence naming the binding the proof ran under, ADR 0074). The
gap is that codefit cannot tell the developer *which* dialect it read, because it never asked.

**The cheap first move is a probe, not a parser**: a shape census over the candidate's own bytes
(backtick quoting, `AUTO_INCREMENT`, `ENGINE=`, `NVARCHAR`, `GO` batch separators) is enough to say
*"this looks like MySQL; set `database.type`"* without pretending to parse it. Declaring what a
measurement found beats declaring that no measurement was taken.

### P0-14 — CLOSED (`a-response-crosses-the-wire-once`) — every tool response crosses the wire TWICE, and the client meters ONE copy

**Measured, not inferred.** `internal/mcp/server.go`'s `addTool` returns `nil` for the
`*CallToolResult`. go-sdk v1.6.1 then serializes the same output JSON into a `TextContent`
block whenever the handler leaves `res.Content == nil`. The integration test proves the two
copies are byte-identical, over a real client/server transport pair, on both the pre-change
and post-change trees:

```
PRE-CHANGE  structured 143,293 B · text block 143,293 B · identical: true · 286,586 B total
POST-CHANGE structured  16,006 B · text block  16,006 B · identical: true ·  32,012 B total
```

The two numbers above are that change's before/after and are not tracked here as the current
payload — the duplication, not the size, is what this entry is about. The same committed
integration test re-measures the size on every run; on `main` today it reads **22,249 B
structured, 44,498 B on the wire, 68 entries**, the growth being the per-rule split buying 36
more nameable entries and the response declaring its own index bytes.

**MEASURED, then closed as a declared limit — not fixed, and the measurement is why.** The
question this entry existed to answer was *which copy the client meters*, because a wrong answer
would have corrupted the response budget. It was answered by driving two binaries — `main`'s and
one whose `addTool` suppresses the copy — against a live client (Claude Code 2.1.196,
2026-08-17) over stdio, with the SAME content sized by `codefit-coverage`'s `detail` list:

```
30 ids  duplicated   ACCEPTED   response declared bytes: 64,661
35 ids  duplicated   REJECTED   result (74,580 characters)
35 ids  single copy  REJECTED   result (74,580 characters)   <- IDENTICAL
```

At 35 ids the payload is ~74,968 B. Metering BOTH copies would have reported ~149,936. The client
reported 74,580 — one copy — and reported the identical figure for the binary that duplicates and
the one that does not. Second control: the two results the client persisted are **74,918 bytes and
byte-identical**. It does not merely count one copy; it stores one.

**The positive control reproduced.** ADR 0062's bisection (2026-08-09, `scan-all` content)
bracketed the client at 64,097 accepted / 74,195 rejected. This one (`coverage` content, another
tool, eight days later) brackets it at 64,661 / 74,580 — within ~1% at both ends. The method
reproduces, so the result is not an artifact of how it was measured.

**Consequence for P0-4: nothing moves.** `addTool` at `d054534` — the exact commit of the `v0.2.6`
binary ADR 0062 drove — is byte-identical to `main`'s, and `go.mod` pins the same go-sdk v1.6.1 at
both points, so that calibration was always taken against a duplicated wire. `ResponseBudgetBytes
= 40,000` is correct and needs no correction. This entry's original warning — that ADR 0062's
ratio **must not be double-counted** on the strength of the duplication finding — is now a
measured fact rather than a caution.

**Why it is declared rather than removed.** Removing the copy buys zero headroom in the client
that was measured; the MCP spec describes the `TextContent` copy as backward compatibility for
clients that read `content` but not `structuredContent` (the SDK cites it at
`mcp/server.go:386-388`); and `addTool` is the single seam all 16 tools pass through. Zero benefit
against non-zero compatibility risk, across every response codefit emits. See
[ADR 0079](decisions/0079-the-client-meters-one-copy-so-the-duplicated-wire-is-a-declared-limit.md)
for the full record, including the one-line suppression, so re-opening this for a different client
costs a rebuild and not a rediscovery.

**What stays true and is NOT closed by this.** The transport cost is real: every response still
puts twice its bytes through the pipe. The measurement is scoped to ONE client, ONE date, ONE tool
— Cursor, VSCode, OpenCode and the rest have their own metering, unmeasured, and a client that
reads only `content` was never exercised. And **P0-4's remaining half is untouched**: the
structural per-bucket cap for `db.surface`, where a response exceeds its budget with nothing
withholdable, is addressed by nothing here.

### P0-13 — CLOSED (`sec-001-affirms-only-what-the-name-established`) — SEC-001 (Go) affirmed a credential that was not there, at Confidence 1.0

The Go name gate had a second arm — any name containing `key` as a substring with a value of
16+ bytes. An **AST census** over codefit's own tree (the sensor driven through the real
`go/ast` parser, because no static probe can enumerate `*ast.KeyValueExpr` or a multi-name
`*ast.ValueSpec`) found **4 of its 5 name-gate findings were false**: enum constants and
descriptive names, each reported as "looks like a hardcoded credential". This is the top of the
board's ordering because it is the failure the ordering criterion names first — codefit
**telling the user something untrue**, in an affirmation channel, at full confidence.

The length guard was **inverted**, and that is measurable rather than arguable: descriptive
kebab/snake values pass 16 bytes *because* they are descriptive, while a credential has no
length floor. `SIGNING_KEY = "s3cr3t"` (6 bytes) was rejected by the same gate that accepted a
29-byte category name. It admitted the false-positive class and rejected a true-positive one.

**Why deletion alone would have been a REGRESSION, not a fix.** The first arm substring-matched
`apikey`, and `lower("API_KEY")` is `"api_key"`, which does not contain it. Measured over six
real credential spellings, **five fired only through the deleted arm**. The component matcher
with adjacent-pair joining is what makes the deletion safe; shipping the deletion without it
would have bought the false-positive fix with five undeclared false negatives.

Fixed by one matcher in a stdlib-only core leaf with **three vocabularies**, so DB-053 (measured
across 29 corpora in ADR 0047, no longer cloned) stays provably byte-identical while SEC-001's
set moves. SEC-050 adopts the convention and keeps its own crypto-material set.

**The second measured narrowing, and its repair.** Component matching also dropped the PLURAL
spellings substring matching had carried for free — `passwords`, `secrets`, `tokens`, `apiKeys`,
`apikeys`, `privateKeys`, `refreshTokens`, `mySecrets`, `userPasswords`, eight of them at any
value length. That was a loss from replacing the FIRST arm, orthogonal to the second arm's
deletion, and no declared limit described it. SEC-001's set now folds the regular `+s` plural of
every entry (and only SEC-001's: DB-053 and SEC-050 keep their frozen sets), with the
false-positive side measured — zero new sites in the AST census over codefit's own tree. See
[ADR 0075](decisions/0075-sec-001-affirms-only-what-the-name-established.md) and `CHANGELOG.md`.

**Named, DECLARED, not built — three follow-ups.**

1. **The lowercase-concatenation gap.** `secretkey` is one component and cannot be split without
   a dictionary. Declared in code (`namematch.LimitLowercaseConcatenation`), rendered on
   SEC-001's own line by `codefit-coverage`, and locked by a test so it is a limit rather than a
   belief. Open as a capability, closed as a claim.
2. **A productive credential-name rule instead of a list.** ADR 0047 replaced ADR 0046's list
   only because the rule was measured byte-identical over 29 corpora. **No credential-name
   corpus exists**, and the error directions are opposite — DB-052's admission SILENCES,
   SEC-001's AFFIRMS. Deferred with its reason, not forgotten.
3. **TypeScript's `$NAME` still matches by substring**, so `tokenizer` fires there. Carried as a
   divergence row with a written reason in the cross-provider case table, which loads TS's regex
   from the real embedded YAML rather than copying it. Widening or narrowing TS is separate,
   measured work.

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
- **P1-4b — CLOSED (`readme-count-and-prac004-entry`) — PRAC-004's owed manifest entry.**
  Its permanent drop was recorded only in
  [ADR 0056](decisions/0056-a-practices-rule-affirms-only-what-it-checked-and-prac-004-is-dropped.md)
  and the CHANGELOG; it owed a coverage-manifest entry. The blocker this item used to name —
  "no Go coverage manifest to put it in, entangled with the still-open P1-3/P4-1 decision" —
  stopped applying once P4-1 resolved
  ([ADR 0065](decisions/0065-go-is-exposed-because-the-response-declares-what-it-lacks.md),
  `declared-partial-language-exposure`) without building
  `internal/providers/golang/coverage.go`, which left this item's landing site real but not
  yet used. **Paid, without a coverage-manifest file**: `providers.RuleSet` gained
  `Excluded []ExcludedRule` (id + reason) and `ValidExclusions()` (C6 — an excluded id can
  never also be `Declared`, checked through the interface with both real providers). Go's
  `Practices` RuleSet (`internal/providers/golang/capability.go`) now names `PRAC-004` there
  with its ADR 0056 reason, so `codefit-coverage` for `"go"` states the permanent gap in
  `NotCovered` (derived by R1) instead of leaving an agent to infer it from `PRAC-004`'s
  absence from `Declared`. No rule, finding, or coverage-manifest file was built —
  `internal/providers/golang/coverage.go` still does not exist, deliberately, matching what
  ADR 0065 already decided.
  **Follow-up, same change name:** `sdd-verify` judged the "no ADR needed" call on
  `ExcludedRule`/C6 unsound (different in kind from `dbcoverage.NotCovered()`'s untyped
  prose precedent) and found, by mutation, that C6 cannot tell a real exclusion from a
  fabricated one. Both are now recorded in
  [ADR 0066](decisions/0066-a-permanent-exclusion-is-a-typed-cross-provider-fact-and-a-phantom-one-is-still-a-lie.md),
  which also adds `RuleSet.ValidExclusionSource()` (C7) — a phantom-exclusion shape check
  built only where `Enumerable` is `true` (TypeScript's loader-backed security rules) and
  explicitly declared not-applicable where it is `false` (Go's hand-written lists,
  including `PRAC-004` itself — that specific gap stays open, see the ADR). README's third
  restatement of TypeScript's surface-mapping reach (line ~214, "Surface mapping — the
  agent reasons.") was also added to the P1-6 lock below, closing the gap `sdd-verify`
  flagged as a SUGGESTION.

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

### P1-6 — CLOSED (`readme-count-and-prac004-entry`) — README undercounted TypeScript's surface-mapping reach

`README.md` stated TypeScript's surface mapping as **three** categories in two places (the
"Works today" bullet and the Supported-languages table row); it is **four** —
`nplus1` shipped as a real category (`codefit-surface-nplus1`) in `v0.2.2`, and both
restatements were never updated, undercounting it for roughly seven weeks. Pre-existing,
unrelated to any change in this window; `sdd-verify` measured it and `git blame` confirmed
the age.

**Both lines corrected** to name all four categories (IDOR, broken authorization,
over-fetching, N+1). **Locked**, not just fixed:
`internal/providers/readme_surface_count_test.go` reads `README.md` and, for every category
in `typescript.New().Capability().Surface`, checks that a matching prose marker appears in
both restatements — derived against the real declaration rather than a second hardcoded
count, so a category added to the vocabulary with no README update fails this test for a
real reason. No other restatement of the count was found on a positive-probe search of the
repository (README's own intro line, `CLAUDE.md`, `COVERAGE.md`, and every CHANGELOG/ADR hit
were checked; the CHANGELOG/ADR mentions are dated, pre-`v0.2.2` historical entries this
project's doctrine does not rewrite).

**Follow-up, same change name:** `sdd-verify` (obs #1467) found a *third* restatement the
lock did not cover — README.md's "What codefit covers today" section, "Surface mapping —
the agent reasons." bullet (line ~214) — already accurate (not a live defect), but outside
the lock's exhaustiveness. Extended: the test now also checks that bullet, proven by
mutation (removing the `over-fetching` marker from it fails the test; the first mutation
tried, removing only the first `N+1` mention, did **not** fail, because the same word
recurs later in the same bullet's unrelated prose about item ordering — a disclosed,
narrow false-negative surface specific to this wider block, not fixed here).

### P1-7 — `codefit-surface-*` silently ignores every project-registered authz helper, for every language

`internal/mcp/surface.go:200`'s `providerFor` resolves the language provider for the
`codefit-surface-*` family (`codefit-surface-authz`, `codefit-surface-idor`, etc.) with
`e.New(nil)` — no helpers, always, regardless of what the project has registered via
`codefit-baseline-register-authz-helper`. `internal/mcp/scanall.go`'s
`providerForLanguage` and `internal/mcp/scan.go` both thread the real
`recognizedHelpers(root, language)` list through to `e.New(authzHelpers)`; only the
surface-tools call site discards it. This is not TypeScript- or Go-specific — it is the
one resolver in the registry-driven trio (ADR 0064, P1-1b) that never receives the
parameter at all, for any exposed language.

**User impact.** A user registers a helper, the agent tells them (per
`internal/mcp/baseline.go`'s own response text) to "re-run `codefit-scan-all` so items
using it reflect `known_authz_detected=true`" — and that promise holds, scoped to
`scan-all`/`scan-security`. But a user or agent that calls `codefit-surface-authz`
directly on the same project, expecting the same registered helper to apply, silently
gets `known_authz_detected` computed against an empty helper set instead — the same
vacuous-false shape [ADR 0067](decisions/0067-every-surface-producer-emits-non-nil-structural-facts.md)
just spent an entire change eliminating for Go's *default* (no-helper) case, except this
one is present *even when the project configured a helper*, because the parameter never
reaches the provider. Nothing in the tool's own output states that `codefit-surface-*`
does not see registered helpers — the gap is real, silent, and per this document's own
ordering criterion, exactly the kind of thing that costs a user their trust: not knowing
how far the tool they are calling actually reaches.

**Why it is not fixed here.** Found and disclosed by ADR 0067
(`sdd/go-surface-structural-facts`, PR #127) while wiring the equivalent parameter for Go
elsewhere in the same registry trio — this exact call site is pre-existing (predates that
PR), cross-provider (not Go-specific, not introduced by that fix), and explicitly out of
that change's scope (its design decision D3). Filed here, not fixed there.

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

### P4-1 — CLOSED (`declared-partial-language-exposure`) — Does Go become a user-facing auditable SECURITY language?

**Decided: yes, exposed — not as a parity claim.** Since P0-5, "does Go become
auditable" was no longer all-or-nothing: `scan-all` already measured the DB dimension
for a Go project with a configured schema. What remained was narrower: does
`providerForLanguage` (via `internal/providers/registry`'s `Exposure`, since ADR 0064)
ever gain a `"go"` entry so `codefit-scan-security`/`codefit-scan-endpoint`/
`by_dimension.security` become real for Go code?

The measurement that forced the decision: Go's exposure was flipped locally and the
real binary was driven over stdio against a Go project containing a SQL injection. It
found the finding — and `surface_items: 1` carried no statement that three of Go's four
surface categories were never searched for, and `codefit-coverage` for `"go"` returned
an error instead of an answer. Exposure without declaration is a partial audit that
reads as a complete one.

**Fixed.** `internal/providers/registry`'s Go entry now declares
`Exposure{SecurityScan: true, SurfaceTools: true, InitDetect: true}` — flipping the one
line Lock A existed to turn into a failing test, deliberately, this time. The rule that
made it safe to flip: **a language may be exposed only if the response declares what it
does not cover for that language.** `surface.DeriveCoverage` computes, for any
provider's declared `Capability.Surface`, the mapped/not-mapped split against the
locked `surface.ProviderCategories` vocabulary — never a hardcoded list — and that
statement now rides on `codefit-coverage` (R1, replacing the old error with a DERIVED
manifest), on `codefit-scan-security`/`scan-all`'s responses (R2, a new
`surface_coverage` field), and on `codefit init`'s printed line and the generated
skill (R4), all before a user runs a single scan. Go's real reach — 6 security rules,
1 of 4 surface categories (`authz` only) — is stated everywhere it is exposed, never
implied as parity with TypeScript's four-of-four.

Flipping the boundary turned seven pre-existing tests red — Locks A/B/C plus four
`scan-all` tests that depended on Go having no resolvable provider — and that was them
working. The locks were not deleted: Locks A/B/C now assert the resolvable set is
`{typescript, ts, tsx, go}` explicitly, the four DB-only `scan-all` tests moved their
fixture to an unregistered language (preserving the exact scenario they always
tested), and a new lock (`TestExposedLanguageDeclaresNonEmptyCapability` +
`TestReplacementLock_ExposedLanguageDeclaresCompleteGap`, generic over every exposed
language) replaces the guarantee the old ones held: nothing is exposed without being
declared.

See [ADR 0065](decisions/0065-go-is-exposed-because-the-response-declares-what-it-lacks.md)
and `docs/specs/declared-partial-language-exposure.md`. **Out of scope, deliberately:**
no new Go rules or surface categories (parity remains a phase-sized effort, comparable
to what Phase 1 did for TypeScript — codefit is written in Go, developed with AI, and
still cannot audit itself through its own tools, only through an internal test); no
`internal/providers/golang/coverage.go` (R1 makes it unnecessary for correctness — see
P1-4b below).

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
