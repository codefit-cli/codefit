# 0059 — Security is a soft dimension inside `scan-all`

**Status:** accepted · **Date:** 2026-08-05 · **Phase:** 2/3 boundary, priority P0-5
(`docs/roadmap.md`) · **Spec:** `scan-all-db-without-language` (SDD change, Engram)

## Context

`buildScanAll` (`internal/mcp/scanall.go`) ran the security sensor **unconditionally**
and used a failed provider resolution as the sole gate for the whole handler:

```go
secRes, err := runSecurity(req.Root, req.Language, ...)
if err != nil {
    return ScanAllResponse{}, nil, err  // ~30 lines before the DB section runs
}
```

A project whose language has no security provider — a Go project, today the only
example — got `unsupported language "go"` from `codefit-scan-all` even when its
`.codefit.yaml` declared a `database.schema_paths` the DB dimension could measure
entirely on its own: the DB sensor's schema parser is picked from the configured
schema *paths* (Prisma vs SQL-DDL), never from `req.Language` (`schemaParserForPaths`,
unchanged by this decision). The DB dimension's own inputs were satisfiable; the
handler never got there.

This was found while investigating a different question — "how does codefit know it
does not have a security provider for a language?" — which surfaced that the answer
was "it doesn't, in any structural sense": `providerForLanguage` was a hand-written
`switch` with no registry, and it was one of **three independently hand-written**
language-resolution switches in the codebase (`providerForLanguage`, `surface.go`'s
`providerFor`, and `scaffold/detect.go`'s `detectLanguage`) that already disagreed:
`detectLanguage` recognizes Go (`codefit init` configures a Go project and installs
the skill), while `providerForLanguage` and `providerFor` do not. `codefit init`
receives a Go project by one door and `codefit-scan-all` evicts it by the other.

## Decision

### 1. Security runs only when a provider resolves; the DB dimension is unblocked

`secRan := providerForLanguage(req.Language, nil) != nil` replaces the hard error as
the security gate. A **nil provider** is the only outcome made non-fatal — a
config-load error or a sensor error inside `runSecurity` is still a hard `error`
return, unchanged. Every other read of the security result (`measured`/`scored`,
`scanned`, `auditable_total`, the three endpoint-bucket notes) is now conditioned on
`secRan`, so a Go project with a configured schema gets `db.measured: true` while
`by_dimension.security` stays honestly `null`.

### 2. The baseline's `scanned` set becomes empty-by-default opt-in

**Invariant SCANNED-OPT-IN:** `scanned` starts as an empty map and is only ever added
to inside the `if <dim>Ran` block of the dimension that owns those categories. Before
this change, `scanned := securityScope(req.Language)` unioned the security categories
**unconditionally**, regardless of whether security ran — a real corruption once
`runSecurity` became gate-able: a security-owned baseline item (e.g. an `authz` item)
would be wrongly marked `gone` and eligible for `codefit-baseline-prune` by a DB-only
pass that never looked at the code the item lives in.

This was proven, not assumed: the fix landed in two commits on purpose. The first
gated only the `runSecurity` call and deliberately left `scanned`'s construction
unconditional — reproducing the corruption for real, on a Go+schema fixture with a
planted `authz` item (`Gone=1`, the item wrongly pruned). The second commit moved the
union inside `if secRan` and turned that same scenario green
(`TestHandleScanAll_SecuritySkipped_DoesNotPruneSecurityBaseline`). A third,
uncommitted mutation (removing the guard again, confirming `Gone=1`, restoring it) is
the mutation proof CLAUDE.md's verification discipline requires.

The invariant is safe by construction in the failure direction: a *forgotten* gate can
now only fail to **add** categories to `scanned`, never wrongly add them —
`baseline.Diff` only marks an item gone within a category `scanned` admits (ADR 0019),
so under-scoping degrades to "prunes nothing it shouldn't have", never the reverse.

### 3. Nothing measurable is an error, not a null-filled 200

When neither a security provider resolves **nor** the DB dimension runs, `scan-all`
returns an error naming both missing inputs — the unresolved language (plus the
single-sourced supported-language set, see below) and, when a schema *was* configured
but failed to parse or read, the specific reason instead of a generic "not
configured" clause.

Two reasons drove this over the alternative (a 200 response with every score
dimension `null`):

- A response with every `by_dimension` entry `null` is **indistinguishable from an
  impeccable project** to an agent skimming it — the exact defect this whole change
  fixes, mirrored one level up.
- `scoring.Compute` called with an **empty** `measured` slice is a code path nobody
  exercises in production today, and it returns `Global: 0` — the worst possible
  score, read by an agent as "catastrophic" for a project that was, in truth, never
  looked at once. Returning that value from the one call site that could produce it
  from an empty set is the worst place to leave an unread path.

The guard (`len(measured) == 0`) sits **before** `diffBaseline`, which saves the next
baseline — `measured` was hoisted above the baseline-diff call specifically so the
refusal happens before any write, not after. `scoring.Compute` therefore never
receives an empty `measured` set in this codebase; the guard makes that property true
by construction rather than documented and hoped-for.

**Accepted consequence, architect-visible:** a *narrowed* `scan-all` pass
(`changed_files`) on a Go project whose changed files exclude its schema now errors
instead of returning an all-null 200. This is the same honesty trade as decision 1: an
agent asking "what did you audit in this slice?" gets a refusal that names why, not a
response shaped like an answer.

### 4. The supported-language set has one source

Naming the supported set in the refusal message by hand would have created a
**fourth** hand-written list, independently driftable from the three that already
disagreed. `providerForLanguage`'s `switch` became `languageProviders`, a map table
that is now the single source both `providerForLanguage` and the new
`SupportedLanguageNames()` read — the latter derived by calling `Language()` on what
the table actually constructs, never a literal.

Three regression locks landed with the refactor, turning the language-resolution
contract into tests instead of memory (`internal/mcp/language_source_test.go`):

- **Lock A** — the resolvable set stays exactly `{typescript, ts, tsx}`. Verified live
  during apply: adding `"go"` to `languageProviders` failed the lock's positive-probe
  assertion; removed after confirming the failure. This is the mechanical fence
  against wiring `golang.New()` into `providerForLanguage` without the deliberate
  decision that would require (roadmap P4-1, still open).
- **Lock B** — `surface.go`'s extension-based `providerFor` must resolve the same
  language every `languageProviders` entry resolves by name, and nothing outside that
  union. Verified live: adding a `.go` case to `providerFor` failed the lock; reverted
  after confirming.
- **Lock C** — every language the real `scaffold.Detect` (`codefit init`) recognizes
  from marker files must be in the resolvable set **or** declared in the new
  `initDetectsButScanAllCannotAudit` allowlist (same shape as the existing
  `deliberatelyNotInSkill` lock), checked in both directions so a stale entry also
  fails. `"go"` is declared, with the reason spelled out. Verified live: emptying the
  allowlist failed the lock for `"go"`; restored after confirming.

None of the three locks required new production behavior to pass on their own — they
lock what `languageProviders` / `providerFor` / `scaffold.Detect` already do. Their
value is turning three independently-editable switches' agreement into something a
test enforces, not something a reviewer has to remember to check across three files.

**Deliberately NOT done here:** unifying the three switches into one implementation,
and resolving the `init`-welcomes / `scan-all`-refuses contradiction for Go beyond
"the DB dimension now measures it". Both are roadmap P1-1b. Wiring `golang.New()` into
`providerForLanguage` (a full user-facing Go security provider) is roadmap P4-1, an
open scope decision — this change makes the temptation to slide it in one line easier
to resist, not the decision to do it.

### 5. `ScanAllResponse` gains an always-present `Security` section

```go
type SecuritySection struct {
    Measured bool   `json:"measured"`
    Note     string `json:"note,omitempty"`
}
```

`Security SecuritySection` is a new field on `ScanAllResponse`, deliberately **not**
`omitempty` — the one place this change diverges from `DBSection`'s own shape rather
than mirroring it. `DB` may legitimately be absent (`json:"db,omitempty"`) because the
dimension does not apply to a project with no `database.schema_paths` configured
(ADR 0020); security applies to **every** project scan-all can run at all, so an
absent `Security` section could only mean an older codefit binary, never "not
applicable". The precedent for always-present is already in-file: `Scope` and
`Budget` are both unconditional for the same reason (a consumer must never infer a
mode from a field's absence).

When `secRan` is false, `Security.Note` states plainly that the schema may have been
audited while the code was not — reachable only after the nothing-measurable guard
(decision 3), so a DB-only pass having actually run is guaranteed whenever this note
fires. The three endpoint-bucket notes (`actionable`, `resolved_clean`,
`frontier_pending`) also take `secRan`: a zero count because security did not run now
says so explicitly, instead of returning the same empty string a real "nothing found"
zero would — the same precedent as `BudgetBlock.Note` (ADR 0048): "no mention of
truncation" and "nothing was truncated" must not be the same bytes on the wire, and
neither must "security found nothing" and "security did not run".

`scope.auditable_total`, when security did not run, is now the count of distinct
canonical schema sources the DB dimension actually read, rather than the
security-only value of `0` — a `0` would assert "no auditable files exist", which is
false the moment a schema was configured and read. The residual — how many files a
security provider *would* have counted for this language — is unknowable without a
provider, and that caveat lives in `Security.Note`, not in `scope`, which
(`ScopeBlock.Validate`) may only carry a note on a *partial* scan.

Finally, the code×schema cross (DB-010/DB-013) is silently skipped on every DB-only
pass today, because it needs a language provider's `QueryExtractor`
(TypeScript-only). `runCross` now returns a third value, a skip reason, appended to
`DBSection.Note` — silence on that path used to read as "the cross ran and found
nothing", which was false; it never ran at all.

## Consequences

- **`ScanAllResponse` grows a key for every consumer, including TypeScript's.** A
  TypeScript project's response now carries `"security": {"measured": true}` beside
  everything it already carried — the same class of change as `by_dimension.practices`
  in the coverage-manifest work (see CHANGELOG `[Unreleased]`), and flagged with the
  same ⚠️. Verified, not asserted: the TypeScript success criterion is **"every
  pre-existing field value and the baseline delta identical"**, not "byte-identical".
  A test (`TestScanAll_TypeScriptHappyPath_OnlyDiffIsAddedSecurityKey`) proves exactly
  that sentence — against the **real** pre-change response, captured from a
  `git worktree` of `main` before this change (not a re-implementation of what the old
  shape "should" have produced), with the new `security` key stripped from both sides
  before comparing.
- **`codefit-scan-security` and `codefit-scan-endpoint` are unchanged and asymmetric on
  purpose.** Both still hard-error `unsupported language %q` for a language with no
  provider. `scan-all`'s multi-dimension design lets one dimension be soft when
  another can still deliver value (the same principle ADR 0020 already established for
  `db`); the two single-dimension tools have no dimension to fall back to, so their
  hard error stays the honest behaviour for them. This asymmetry is deliberate, not an
  oversight to close later.
- **A narrowed `scan-all` on a Go project can newly error where it used to
  succeed-with-nulls.** See decision 3's accepted consequence. No narrowed-scan test
  in the existing suite exercised this combination before, so nothing regresses; it is
  recorded here because it is a new refusal shape, not a silent one.
- **The Go row in the README's "Supported languages" table needed correcting in this
  same change**, per `CLAUDE.md`'s documentation-honesty rule: before this change the
  row's "Provider + static security/best-practice detectors" was misleading in one
  direction (no MCP tool could audit a Go project at all); after this change it would
  be misleading in the *other* direction if left unchanged (a reader could infer full
  Go support, when only the DB dimension, schema-only, is real). The row now states
  the actual, narrow, post-change capability. The rest of the table's format and the
  general "only TypeScript" README framing (roadmap P1-1a) are unchanged by this
  change; P1-1c owns the full rewrite.
