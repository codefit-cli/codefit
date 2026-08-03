# ADR 0048 — The change scope is an agent-supplied INPUT, and the baseline scope becomes two-dimensional

**Status:** Accepted · **Date:** 2026-08-03 · **Phase:** 3, thread H0, slice S1

**Extends [ADR 0019](0019-unified-multi-sensor-baseline-scoped-gone.md).** 0019's decision —
an item is eligible to be `gone` only if a sensor owning its category RAN — stands
unchanged and is kept. This ADR adds a SECOND dimension beside it; it does not replace
it. Either dimension alone is a way to go blind, in opposite directions.

**Related to [ADR 0014](0014-neutral-schema-model-and-provider-owned-schema-parsing.md)
and [ADR 0029](0029-code-schema-cross-infrastructure.md).** 0014 is why the DB dimension reads
`database.schema_paths` rather than a repository walk, which is the whole of R4 below;
0029 is why the code×schema cross runs outside the DB sensor and therefore needs its own
answer under a narrowed pass.

**Implements** `docs/specs/change-scope.md`.

## Context

Layer 0 of the filtering pyramid (PRD §19) is the cheapest tier: *do not analyze a file
that did not change*. It had existed as an unwired abstraction since Phase 0 —
`internal/core/pipeline` declaring `LayerChanges`, `internal/core/cache` implementing a
content-hash finding store, both marked `Status: INERT` in their own `doc.go` with zero
production importers. The regression-risk half of RF-06 (Phase 3, thread H2b) cannot
exist without a notion of "what changed", so the scope had to become real before that
thread can start.

`AuditContext` also carried a dead `Since string` whose comment promised "a git ref for
incremental (`--since`) mode". It had never had a reader or a writer.

The design question is not *how* to narrow an audit — a set membership test is trivial.
It is: **how does a narrowed audit avoid lying?** A partial audit indistinguishable from
a full one is a lying auditor, and the most expensive lie is not a missed finding but a
silently deleted memory of one.

## Decision

### 1. The scope is an INPUT from the caller, never derived from git

`internal/core/scope` is a pure leaf holding a set of project-relative paths. The MCP
adapter fills it from an optional `changed_files` on `codefit-scan-security` and
`codefit-scan-all`. codefit does not shell out to git, does not read `.git`, does not
diff refs and assumes no branch model. Two standing reasons, both doctrinal rather than
technical:

1. **codefit has no power over the user's git** (CLAUDE.md, autonomy principle). Reading
   the index or diffing a ref would make codefit's answer depend on a workflow it does
   not own and must not assume.
2. **MCP-first.** The caller is an AI coding agent that already knows which files it
   touched — it just wrote them. Asking git would be codefit re-deriving, worse and less
   portably, a fact the caller already holds.

`codefit-scan-db` deliberately takes no `changed_files`: its inputs are the configured
schema paths, not a walk, so a caller who wants to know whether the schema changed
either passes it or does not call the tool.

### 2. Absent or empty means FULL, and the zero value means NOTHING — on purpose

The two fail-safes point in opposite directions, and both directions are the safe one for
their consumer.

- `scope.Of(nil)` and `scope.Of([]string{})` return `scope.Full()`. "Nothing was passed"
  must never be read as "audit nothing"; the safe direction for an auditor is to look at
  MORE. An agent that means "nothing changed" does not call the tool.
- The zero value `Scope{}` includes NOTHING, which is why `Full()` is an explicit
  constructor rather than the zero value. `baseline.Diff` asks `Includes` to decide
  whether an item may be marked gone, so a caller that forgets to pass a scope prunes
  nothing — it under-reports instead of deleting audit memory it never verified.

A **walk** must ask neither of those. It asks `Narrows()`, false for both `Full()` and the
zero value, so an `AuditContext` assembled without a scope audits everything. Gating a
walk on `Includes` would turn an unset scope into a silent no-op audit reporting score
100 — a false all-clear, the exact class of lie codefit exists to catch. That direction is
test-locked (`TestRun_UnsetScope_AuditsEverything`).

Canonicalization (`scope.Canon`) is one exported definition, applied on construction AND
on lookup: separators to `/`, then `path.Clean`. It is deliberately platform-INDEPENDENT
(`path`, not `filepath`) — a project-relative path travels between an agent, a committed
baseline and a checkout on either OS, and resolving `src\a.ts` only when codefit happens
to run on Windows would reintroduce exactly the separator drift `v0.2.6` paid to remove.

### 3. The baseline scope becomes TWO-DIMENSIONAL

`baseline.Diff(prev, observed, scanned, files)`. An item is eligible to be `gone` only
when BOTH admit it:

```
scanned[item.Category]  AND  files.Includes(item.File)
```

The category dimension (ADR 0019) does not cover a partial audit, and this was a **live
corruption risk, not a hypothetical**: a security finding in a file the pass never opened
still belongs to the `security` category, which DID run. A category-only guard sees it
unobserved-and-in-scope, marks it `gone`, and `codefit-baseline-prune` then deletes the
audit memory of every file the scan did not look at — silently, and in the direction of
going blind.

The file dimension alone would fail in the mirror direction, pruning the dimensions whose
sensor did not run. Both are required; both fail safe (an empty `scanned` and the
zero-value `Scope` each include nothing).

The no-regression floor is a golden: a `Full()` scope through `Diff` produces a delta
byte-identical to the previous behavior.

### 4. `codefit-baseline-prune` accepts no scope at all

Its internal re-scan always passes `scope.Full()`. This is a **deliberate asymmetry**:
scanning may be cheap and partial; forgetting may not. Deleting audit memory requires
having looked at everything. There is no `changed_files` on the prune tool, so the
asymmetry cannot be argued away by a caller.

### 5. Cross-rule categories ABSTAIN from the gone-scope under a narrowed pass

The code×schema cross (DB-010/DB-013) still **runs** when the scope narrows — fewer query
filters can only produce fewer items, and the items it does produce are real. But
`crossrules.OwnedCategories()` is added to `scanned` only when `!scp.Narrows()`.

The reason is that these items are the one shape the file dimension cannot protect: an
item **anchors to the schema**, while the evidence for it is **every query filter in the
repository**. Under a narrowed pass the schema anchor may well be in scope while the query
that justified the item lives in a file the pass never opened — so the item goes
unobserved, its file is in scope, and the guard would let it be pruned. Abstaining is the
same posture the DW census rules already take: a shrunken census does not get to judge.

### 6. A dimension whose inputs are all out of scope is NOT MEASURED

The DB dimension runs in `scan-all` only when at least one configured `database.schema_paths`
entry is in scope (a configured path may name a directory of migrations, so a scoped file
inside one counts; the prefix test is on canonical path SEGMENTS, so `db/migrations-old` is
not mistaken for `db/migrations`). Otherwise `by_dimension.db` is `null` — not measured —
through the machinery that already exists (PRD §21 plus ADR 0019's `OwnedCategories`
scoping), rather than scoring an untouched schema 100.

When it does run, it reads **all** its configured schema paths, never a narrowed subset: a
schema judged from half its migrations is itself a shrunken census.

### 7. Every scan response carries a `scope` block, validated in production

`{mode, requested, audited, auditable_total, unmatched, note}`, present unconditionally so
a consumer never infers the mode from an absence.

- `auditable_total` counts the whole project, never the scope. Narrowing must not shrink
  the denominator, or "3 of 412" collapses into a self-flattering "3 of 3".
- `unmatched` lists requested paths the audit never reached (deleted, wrong extension,
  inside a skipped directory). Without it, an agent that passes three wrong paths receives
  "0 findings" and reads it as clean. It is the difference between *audited and clean* and
  *never looked*.
- `note` is MANDATORY when `partial` and FORBIDDEN when `full`, enforced by
  `ScopeBlock.Validate()` **in the handler**, not merely asserted in a test. A violation is
  a loud error rather than an unlabelled partial result an agent would read as a full one.

`scoring.IsBlocked` is unchanged and stays non-configurable. The narrowing is carried by
the note, not by the scoring: under a partial scope `blocked: false` means "no critical in
the audited slice", not "no critical". `blocked: true` needs no caveat.

### 8. `AuditContext.Since` is REMOVED

Replaced by the scope, not kept alongside it. A field naming a capability codefit does not
have is the same class of lie as a manifest that over-promises, and keeping a dead git-ref
field beside a live non-git scope would have re-stated the promise this slice exists to
retire.

## Consequences

### No audit rule changed

No finding, no surface item and no baseline fingerprint moves for a full scan. Every
coverage source is untouched: `rules/`, `internal/core/dbrules/`, `internal/core/dwrules/`,
`internal/core/paradigm/`, `internal/core/crossrules/`, `internal/core/dbcoverage/` and
every `internal/providers/*/coverage.go`, so `COVERAGE.md` and the `codefit-coverage`
manifest are unchanged too. This is a behavior-preserving addition.

### It decides WHAT IS AUDITED, not what is reused

`internal/core/cache` and `internal/core/pipeline` remain INERT with zero production
importers. The content-hash finding cache is slice S2 and is **not built**. A repeat scan
of the same files is not cheaper; a narrowed scan is cheaper only because fewer files are
opened. Nothing here caches or reuses a finding between runs.

Wiring the cache later carries its own hazard, already identified and recorded here so it
is not rediscovered: the cache key must include the codefit and rule versions, or an
upgrade that adds rules serves stale findings for an unchanged file and reports "clean"
under rules it never ran.

### The response surface grew

`ScanResponse` and `ScanAllResponse` gain a required `scope` object, and
`findings.SensorResult` gains `auditable_total` / `audited_files`. Additive for a consumer
that ignores unknown fields; a consumer that pins an exact shape sees new keys. A sensor
whose inputs are CONFIGURED rather than walked (the DB dimension) reports its sources in
`audited_files` and leaves `auditable_total` zero — there is no file census to be a
denominator of.

### What this does not decide

Whether an agent SHOULD narrow a scan is the agent's and the developer's call, not
codefit's. codefit's obligation is that a narrowed answer never reads as a wider one, and
that a narrowed pass can never delete what it did not examine.
