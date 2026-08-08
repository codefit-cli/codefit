# 0061 — The baseline write is gated on every check codefit can perform

**Status:** accepted · **Date:** 2026-08-08 · **Phase:** 3, priority P0 (new, discovered
while measuring roadmap P0-4) · **Spec:** `docs/specs/baseline-write-gate.md` · **Implements:**
invariant **I3** of `docs/specs/audit-protocol.md` (draft, `docs/decisions/0060-*.md`), as a
**mitigation**, not the full delivery-confirmation layer that ADR proposes.

## Context

`codefit-scan-all` persisted `.codefit-baseline` while it was still building its own
response — `internal/mcp/scanall.go:372` → `internal/mcp/baseline.go` (`diffBaseline` →
`diff.Next.Save`), roughly 100 lines before the handler returned. That write ran **before**
every check codefit performs on its own output: `scoring.MissingWeights`, `ScopeBlock.Validate()`,
and `fitToBudget`'s `stillOver`.

Reproduced live against a real MCP client, on a fresh project with no baseline:

```
1. codefit-scan-all → the client REJECTS the response:
     "result (312,692 characters) exceeds maximum allowed tokens"
   the user sees an error and no data
2. codefit had already written .codefit-baseline — 66,227 bytes, 373 items
3. the retry reports: "0 new, 373 known"
```

**373 findings were recorded as seen by a reader who received nothing.** The census that
found this measured its test coverage: breaking delivery on purpose turned 20+ tests red,
every one of them on "the handler returned an error", and **not one** on "the baseline was
written anyway". Nothing in the tree inspected the on-disk baseline after a failed response.

## Decision

### 1. The write moves after every check, and is separated from the computation (R1)

`diffBaseline` used to compute the diff **and** save it in one call. It now only computes:

```go
func diffBaseline(prev *baseline.Baseline, observed []baseline.Observed, scanned map[string]bool,
    files scope.Scope) (baseline.DiffResult, BaselineDelta)
```

`buildScanAll` returns the computed-but-unsaved next baseline as a fourth value
(`*baseline.Baseline`, nil on every error return), and the save moves to
`handleScanAllBudgeted`, the one caller that also knows the outcome of the budget-fitting
step:

```go
rendered, stillOver := withNamedActionable(resp, actionable, budget)
if !stillOver {
    if err := next.Save(baselinePath); err != nil {
        return ScanAllResponse{}, fmt.Errorf("saving baseline: %w", err)
    }
}
return rendered, nil
```

Any of `scoring.MissingWeights`, `ScopeBlock.Validate()`, or `fitToBudget` reporting
`stillOver` now means: **nothing is written.** The two former checks are structurally
guaranteed — `buildScanAll` cannot compute `next` and also return an error, so no error path
out of it can be followed by a save (locked by
`TestScanAllWriteGate_BuildScanAllErrorNeverWrites`). The third, `stillOver`, is the one
reachable through the public surface with a legitimate input, and it is the one the census
named **most dangerous**: the probability of a response not fitting its byte budget **rises
with the number of findings**, so the second run of a growing project is quieter than the
first exactly when it should be louder. Locked by
`TestScanAllWriteGate_StillOver_NothingWritten` and
`TestScanAllWriteGate_StillOver_PreExistingBaselineUntouched` (the second plants a real
baseline, adds a new endpoint, forces `stillOver`, and asserts the file is **byte-identical**
to what it was before the failed call — not merely "still exists").

### 2. R2's symmetry: `known` gets the same two-dimensional scope guard `gone` already has

`baseline.Diff`'s `gone` direction was already guarded by `scanned[category] && files.Includes(file)`
(ADR 0048/0029): a sensor that did not run, or a file this pass never opened, cannot prune an
item. The `known` direction had no equivalent — **any** observed item matching a previous
fingerprint was promoted to known and refreshed, with no check on whether that item's own
category or file was in scope this pass.

This was not hypothetical. The code×schema cross rules (DB-010/DB-013) anchor their
fingerprint to the **schema file**, which the DB dimension always reads in full regardless of
scope narrowing (`runDBForScanAll` passes `scope.Full()` to the sensor). A narrowed pass whose
`scanned` set deliberately excludes cross categories (`scanall.go`, `if !scp.Narrows()`) could
still re-observe the same schema-anchored fingerprint from a shrunken set of query filters,
and — before this change — silently re-confirm it `known` under a category the pass itself
had declared out of scope. Worse: because the item was *also* carried forward verbatim by the
existing out-of-scope loop, the observed-loop's un-guarded promotion **duplicated** the item
in `Next.Items`.

The fix is one guard, placed where the `gone` guard already lives, stated once: **an item's
state may only be advanced by a pass that actually looked at it.**

```go
for _, o := range observed {
    if prevItem, ok := prevByFP[o.FP]; ok {
        if !scanned[prevItem.Category] || !files.Includes(prevItem.File) {
            continue // already carried forward verbatim by the out-of-scope loop above
        }
        ...
```

Locked by `TestDiff_ObservedButCategoryDidNotRun_NotPromotedToKnown` and
`TestDiff_ObservedButFileNotOpened_NotPromotedToKnown` (`internal/core/baseline/known_scope_test.go`),
each proven by mutation: replacing the guard's condition with `false` reproduces both the
wrong `known` state and the duplicate `Next.Items` entry; restoring it turns both green.
`TestDiff_ObservedAndInScope_StillPromotedToKnown` is the control — the ordinary in-scope
path is unchanged.

### 3. R3 is declared, not solved: MCP has no delivery acknowledgement

Verified against the specification: an MCP tool response carries the request `id` and
nothing flows back — no ack, retry, or reliability mechanism is defined. A response that is
well-formed and within budget can still be lost to a dropped connection, a client crash, or a
client-side limit codefit has no way to know about (RFC 9110 §9.2.2: *"the request can be
repeated automatically if a communication failure occurs before the client is able to read the
server's response"* — the user's retry in the reproduction above was exactly that, and it was
legitimate; the tool's contract was not).

**This residual is not closable by this change, or by any amount of care in this process.**
It is the Two Generals problem: no bounded protocol gives a sender certainty of delivery. This
change closes the gap it *can* close — codefit no longer advances its own memory on a response
it can already prove did not fit — and declares the rest as a known limit rather than papering
over it with a false sense of completeness.

**This change is a mitigation, not the cure.** The cure is structural: deriving "seen before"
from confirmed delivery instead of storing it unconditionally, which is exactly invariant I3's
full design in `docs/decisions/0060-*.md` / `docs/specs/audit-protocol.md` (the `pending` state,
promotion by implicit reference on a later call). That ADR is not yet merged and its
state-machine, reference-tracking and baseline-migration work are explicitly scoped as a
separate, larger change. This change narrows the *reachable* instance of the defect this
session measured (a still-in-budget-computation response that codefit itself already knows
will not arrive intact) without attempting the harder unsolved half (a well-formed response
lost after it leaves codefit's process).

### 4. Atomicity was not the fix, and saying so is part of this change

`Baseline.Save` already writes to a temp file and renames (`internal/core/baseline/baseline.go:139`,
`os.CreateTemp` + `os.Rename`). **No other tool in the survey does this.** It did not help:
the write this ADR fixes was atomic, complete, and wrong. Torn-file corruption was never the
defect; writing the wrong thing at the wrong time was. "We made the write atomic" reads like a
fix for this defect and is not one — the file that got fsync'd and renamed cleanly still
recorded 373 items a reader never saw.

## Prior art

**PHPStan is the only surveyed tool with this exact guard.** When a run produces internal
errors it refuses to write: *"%s occurred. Baseline could not be generated."* PHPStan and
Psalm both make baseline generation an explicit, separate invocation that never happens
silently during a normal run; ESLint's *programmatic* API is incapable of creating
suppressions at all, leaving that surface to the CLI only.

The failure mode this change closes is documented in the wild, twice, in tools with far more
users than codefit has today:

- **Semgrep**, December 2025 release notes: *"Fixed an issue where findings in files that time
  out or fail to scan were set to a status of Fixed, ensuring scan results more accurately
  reflect what was actually analyzed."*
- **SonarQube**, community thread #131476: a second scan closed every issue as Fixed after
  analysing nothing (`28/28 files marked as unchanged`, a broken compilation database). Staff's
  own framing of the governing rule: *"Issues are Closed when subsequent analysis doesn't
  re-find them"* — with no distinction between "the analyser looked and found nothing" and
  "the analyser never looked."

## Consequences

- **`diffBaseline`'s signature changed** (no `path` argument, no error return — `Diff` cannot
  fail) and **`buildScanAll` gained a fourth return value**, the unsaved next baseline. Two
  pre-existing white-box tests that called `buildScanAll`/`withNamedActionable` directly
  (`scanall_budget_test.go`, plus the `dogfood`-tagged `dogfood_budget_test.go`) were updated
  to the new shapes; no behavioural assertion in them changed.
- **`withNamedActionable`/`fitToBudget` now return `(ScanAllResponse, bool)`** — the bool is
  `stillOver`, needed by the caller to gate the save. The rendered response's content and the
  `stillOver` **WARNING** note in `Budget.Note` are unchanged; only what happens to the
  baseline file differs.
- **A still-over response is still returned to the caller**, unchanged from before this
  change — it is not converted into an error. The gate is entirely about the write, not about
  refusing to answer; an over-budget response with an honest `WARNING` note is more useful to
  the agent than a hard failure, and changing that shape was explicitly out of scope.
- **`codefit-baseline-prune`** (`internal/mcp/baseline.go`) has the same shape of defect — it
  re-scans, computes, and saves before returning, all in one call — on a **human-triggered**
  path rather than the read path this change closes. Recorded in the spec's out-of-scope list,
  not fixed here.
- **The budget's unit remains bytes while the client's limit is tokens** (roadmap P0-4,
  unchanged by this ADR). `stillOver`'s accuracy is bounded by that proxy; this change makes
  the gate meaningful, it does not make the sensor behind it exact.

## Alternatives considered

**Make the write itself smarter (e.g. only write items in the rendered subset).** Rejected:
the defect was never about *which* items got written, it was about writing *at all* before
codefit knew whether the response would arrive. A partial write keyed to the rendered subset
would still advance memory for items the client never received back, which is a smaller
version of the exact bug.

**Convert `stillOver` into a hard error.** Rejected as a change to a different, unrelated
contract (`fitToBudget`'s declared behaviour of always returning a best-effort response with a
`WARNING` note, R3 of `docs/decisions/0054-*.md`). Gating the write does not require changing
what the caller receives.

**Wait for the full I3 delivery layer (ADR 0060) instead of a narrower fix.** Rejected as the
immediate answer: that design needs a state-machine, reference tracking, and a baseline
migration, all explicitly scoped as follow-on work. The reachable, measured instance of the
defect — a response codefit itself already knows is broken before it returns — does not need
any of that machinery to close.
