# Spec — The baseline write is gated on everything codefit can verify

**Status:** draft · **Implements:** invariant **I3** of `docs/specs/audit-protocol.md` ·
**Target:** `v0.3.0` line

## The defect, reproduced

`codefit-scan-all` persists the baseline **while building its response**, roughly 100 lines
before the response is returned. Reproduced live against a real MCP client:

```
1. fresh project, no baseline
2. codefit-scan-all → the client REJECTS the response:
     "result (312,692 characters) exceeds maximum allowed tokens"
   the user sees an error and no data
3. codefit had already written .codefit-baseline — 66,227 bytes, 373 items
4. the retry reports: "0 new, 373 known"
```

**373 findings were recorded as seen by a reader who received nothing.** The write is at
`internal/mcp/scanall.go:372` → `internal/mcp/baseline.go:50` (`diffBaseline` →
`diff.Next.Save`), and it happens **before** `scoring.MissingWeights`, **before**
`block.Validate()`, and **before** `fitToBudget` — every check codefit performs on its own
output.

The census measured the coverage: breaking delivery on purpose turned **20+ tests red, every
one of them on "the handler returned an error"** and **not one** on "the baseline was written
anyway". Nothing inspects the on-disk baseline after a failed response.

## The name for this

**RFC 9110 §9.2.2**:

> Idempotent methods are distinguished because the request can be repeated automatically if a
> communication failure occurs before the client is able to read the server's response.

A response that exceeded the client's limit **is** a communication failure before the client
could read it. The user's retry was legitimate; the tool's contract was not. A scan is
semantically safe (§9.2.1) and semantically idempotent, and the `known` write breaks both.

## The prior art this follows

**PHPStan is the only surveyed tool with this guard**, and it is exactly this shape: when the
run produced internal errors it **refuses to write** — *"%s occurred. Baseline could not be
generated."* PHPStan and Psalm both make baseline writing an explicit invocation that never
happens during a normal run; ESLint goes further and makes its *programmatic* API incapable of
creating suppressions at all, leaving that to the CLI.

The failure mode is documented in the wild, twice, in major tools:

- **Semgrep**, December 2025 release notes: *"Fixed an issue where findings in files that time
  out or fail to scan were set to a status of Fixed, ensuring scan results more accurately
  reflect what was actually analyzed."*
- **SonarQube**, community #131476: a second scan closed every issue as Fixed after analysing
  nothing (`28/28 files marked as unchanged`, broken compilation database). The governing rule
  as stated by staff: *"Issues are Closed when subsequent analysis doesn't re-find them"* —
  with no distinction between *"the analyser looked and found nothing"* and *"the analyser
  never looked."*

## R1 — The write happens last, after every check codefit can perform

The baseline is persisted only once the response has passed everything codefit is able to
verify about its own output:

- `scoring.MissingWeights` returned no error,
- `ScopeBlock.Validate()` returned no error,
- `fitToBudget` did not report `stillOver`.

Any of those failing means the response will not reach its reader as a complete, valid result.
**Nothing is written.** The next scan re-observes everything and reports it as new, which is
the correct outcome: the reader never saw it.

`diffBaseline` currently computes the diff **and** saves in one call. The computation is
needed to build the response; the save is not. They separate.

## R2 — What is written is scoped to what was actually analysed

This is Semgrep's December-2025 fix, and codefit has the asymmetric half of it today: the
`gone` direction is guarded by a two-dimensional scope (category **and** file, ADR 0048) so a
pass cannot prune what it never opened — while the `known` direction has no equivalent guard.

A file that could not be read, a sensor that did not run, a narrowed pass: none may promote an
item to `known`. The rule is the same in both directions and it is stated once: **an item's
state may only be advanced by a pass that actually looked at it.**

## R3 — The residual is declared, not papered over

**MCP has no delivery acknowledgement.** Verified against the specification: a response
carries the request `id` and nothing flows back; no ack, retry or reliability mechanism is
defined. A response that is well-formed and within budget can still be lost to a dropped
connection, a client crash, or a client-side limit codefit does not know about.

That residual is **not closable by this change, or by any amount of care in this process** —
it is the Two Generals problem, and no bounded protocol gives a sender certainty of delivery.
It is declared as a known limit with that reasoning attached, so it is met as a recorded item
rather than rediscovered.

**This change is a mitigation, not a cure.** The cure is structural — deriving "seen before"
instead of storing it, the shape Semgrep and golangci-lint both use — and it is a separate,
larger change.

## R4 — Atomicity is not the fix, and saying so is part of the change

`Baseline.Save` already writes to a temp file and renames
(`internal/core/baseline/baseline.go:139`). **No other tool in the survey does**, and it did
not help: the write was atomic, complete, and wrong. The ADR states this explicitly, because
"we made the write atomic" reads like a fix and is not one.

## Out of scope, stated

- **Deriving `known` instead of storing it.** The structural answer; its own change.
- **The budget's unit.** `ResponseBudgetBytes` counts **bytes**; the client's limit is in
  **tokens** — measured live, verbatim: *"exceeds maximum allowed tokens"*. The gate's
  `stillOver` signal is therefore only as good as a proxy. That is roadmap **P0-4** and it is
  the immediate follow-up: a gate is worth what its sensor is worth.
- **`codefit-baseline-prune`** (`internal/mcp/baseline.go:410`) re-scans and deletes, then
  saves before returning — the same shape as this defect on a human-triggered path. Recorded
  here, addressed after.
- The four decision-path writes (`baseline-accept`, `register`/`unregister-authz-helper`) are
  correct as they are: an explicit human decision is not a read path.

## Test contract

Each proven by **mutation** — break the exact behaviour, watch it fail, restore, watch it
pass. Both runs in the commit message.

1. **The defect's own regression test.** A scan whose response fails each of the three checks
   leaves `.codefit-baseline` **untouched** — asserted with `os.Stat`, not inferred from the
   returned error. This repository already has the assertion shape:
   `TestHandleScanAll_NothingMeasurable_Errors` asserts `os.Stat(...) → IsNotExist` with the
   note *"the nothing-measurable guard must fire BEFORE any baseline write"*. It simply has no
   delivery-path analogue. *(Mutation: move the save back above the checks — the current code.)*
2. **A pre-existing baseline is not modified** on a failed response, not merely "not created":
   plant a baseline, force a failure, assert the file's bytes are identical.
3. **The happy path is unchanged.** Same response, same delta, same file contents as before
   this change for a project that succeeds.
4. **R2's symmetry**: an item whose file was not opened by this pass is not promoted to
   `known`, by the same scope guard that already prevents it being marked `gone`.
5. **`stillOver` specifically**: the budget-exceeded path is the one the census named most
   dangerous, because the probability of a response not fitting **rises with the number of
   findings** — the second run is quieter than the first exactly when it should be louder.
   `scanall_budget_test.go` exercises `stillOver` today and never opens the baseline file.
