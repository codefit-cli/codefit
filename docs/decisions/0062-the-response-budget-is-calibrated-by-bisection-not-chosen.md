# 0062 — The response budget is calibrated by bisection, not chosen

**Status:** accepted · **Date:** 2026-08-09 · **Phase:** 3, priority P0-4 (`docs/roadmap.md`)
· **Supersedes** [ADR 0054](0054-actionable-endpoints-are-named-and-the-response-declares-its-budget.md)'s
**number** (`ResponseBudgetBytes` 60 000 → 40 000), **not its reasoning**: 0054's decision to
name `actionable`'s endpoints instead of inlining them stands untouched — it is what turned
"pick a number" into a question worth calibrating at all, instead of one any number would fail.

## Context

`ResponseBudgetBytes` was **60 000**, and ADR 0054 said so in its own words: chosen from a
derivation (Claude Code's default 25 000-token cap, ~3 bytes/token, so ~75 KB, with 60 KB
picked to sit under that) plus two single data points (312 692 bytes REJECTED, 40 282 bytes
ACCEPTED) — never a measurement of where the real ceiling sits.

**Measured by bisection against a live MCP client (Claude Code, 2026-08-09).** The `v0.2.6`
binary was driven over stdio to generate responses of controlled size, cut from trimmed copies
of a real 317-file TypeScript project (salonpro), then the chosen sizes were sent through the
live client:

```
 32 397  ACCEPTED
 64 097  ACCEPTED        <- largest observed acceptance
 ─────── the ceiling lies in here ───────
 74 195  REJECTED  "exceeds maximum allowed tokens"
105 672  REJECTED
155 164  REJECTED
312 692  REJECTED
```

The real ceiling is bracketed between **64 097 and 74 195 bytes** — narrower and lower than
the 75 KB the old derivation assumed, and the old 60 000 turns out to have had only 6–24%
margin against it (previously believed closer to 49%), not because 60 000 was wrong, but
because nobody had measured the denominator it was being compared against.

## Decision

**`ResponseBudgetBytes` moves from 60 000 to 40 000.** The arithmetic, stated so it can be
re-derived later without re-reading this file:

- **62% of the largest observed acceptance** (64 097 bytes) — a real margin below a real
  accepted size, not a derived ceiling.
- Leaves room for roughly a **60% increase in token density** before a response would approach
  the rejected end of the bracket (74 195 bytes), because the same byte count buys fewer tokens
  when the content is denser (long identifiers, hex fingerprints, deep paths).
- Matches an **independent, earlier data point**: a 40 282-byte response known to have arrived
  (measured 2026-08-04, before this bisection existed) — two measurements taken five days apart
  by different methods landing within 320 bytes of each other.

## The assumption this number rests on — stated, not buried in a comment

The client's limit is in **tokens**. This budget counts **bytes**. The bytes-per-token ratio is
**content-dependent**, so the margin above is not a fixed safety factor: a response at 39 000
bytes of unusually dense content (long hashes, deep nested paths, many short-lived identifiers)
can carry more tokens than a 39 000-byte response of ordinary English-like JSON keys and prose,
and could cross the same client's real ceiling while still reading as "under budget" by
codefit's own count. 40 000 is calibrated against the content shape actually measured, not
against a worst-case token density.

The measurement itself is scoped just as narrowly: **one client (Claude Code), one date
(2026-08-09), one content shape** (a real TypeScript/Prisma project's security findings and
surface). Other MCP clients — Cursor, VS Code, OpenCode — enforce their own limits, and none of
them were measured. This number is not portable evidence about any of them.

## Measured consequence — declared, not left for a user to discover

Rebuilt with `ResponseBudgetBytes = 40 000` and run against a fresh copy of the same real
project (salonpro, 316 files, 174 endpoints, no baseline), verified directly against the
production code path (`buildScanAll` → `withNamedActionable`, not a re-implementation):

```
payload:            39 962 bytes (fits the 40 000-byte budget; stillOver=false)
withheld:            19 of 174 endpoints (actionable 5, frontier_pending 14, resolved_clean 0)
```

At the old 60 000 this same project fit **entirely**, 0 withheld. **Real mid-sized projects
will start seeing non-zero `withheld` counts they did not see before.** This is not a defect —
each bucket's `count` remains the complete number codefit classified, `withheld` says exactly
how many are missing, and `codefit-scan-endpoint` still fetches any named endpoint's full detail
on request (ADR 0054, invariant I4 of `docs/specs/audit-protocol.md`) — but it is a genuine,
user-visible behaviour change, not a free tightening, and it is recorded here rather than left
for a user to discover from a `withheld: 5` they were not expecting.

## What this does NOT fix

- **A byte budget cannot guarantee a token limit.** No amount of recalibrating the byte number
  closes the unit mismatch stated above and already declared in ADR 0061's consequences and
  `docs/specs/baseline-write-gate.md`'s out-of-scope list. `stillOver`'s accuracy — and now
  `withheld`'s — is bounded by how good a proxy bytes are for tokens on the content actually
  served, and that proxy quality was never, and is not by this change, made exact.
- **The structural answer is the declared follow-up, not this change.** A hard cap on entries
  per bucket — so response size stops being a function of project size at all — is what removes
  the need to guess a byte number in the first place. That is roadmap P0-4's remaining item
  after this change closes the "measured, not chosen" half of it.
- **codefit gains no tokenizer.** This change adds no dependency and does no token counting; the
  calibration is entirely a byte-domain number chosen against byte-domain measurements.

## Consequences

- `internal/mcp/scanall.go`'s `ResponseBudgetBytes` doc comment carries this ADR's bracket, date,
  client, method and arithmetic in full, so the number's provenance travels with the constant,
  not only in this file.
- No rule, finding, surface item or baseline fingerprint changes. This is the same class of
  change as ADR 0054: how much of a correct analysis a response can afford to spell out, never
  what codefit detects.
- `docs/roadmap.md` P0-4 closes on the "measured, not chosen" half; the structural entry cap is
  recorded as its remaining work, not silently dropped.
- Existing budget tests (`internal/mcp/scanall_budget_test.go`) reference the `ResponseBudgetBytes`
  symbol, never a literal 60 000/40 000, so none of them needed to change value — they now
  exercise the calibrated number automatically. One golden-based regression test
  (`scanall_regression_test.go`, locked to an unrelated ADR 0059 diff) had its comparison
  explicitly widened to exclude the `budget` block, which legitimately differs now, alongside
  the `security` key it already excluded.
- A new test, `TestScanAllBudget_HonestyPersistsWhenTheBudgetForcesWithholding`, locks invariant
  I4 independently of the exact budget value: forced withholding at whatever
  `ResponseBudgetBytes` currently is must still report each bucket's complete `count` and an
  accounting `withheld`. Mutation-proved: recomputing `Actionable.Count` from the rendered
  subset instead of the complete pre-render slice — the exact defect ADR 0054 R4 forbids — turns
  it red; the correct wiring turns it green again.

## Alternatives considered

**Leave 60 000 and only declare the uncertainty.** Rejected: the bisection is evidence the
margin against a real client was thinner than believed (6–24%, not the ~49% the old comment
claimed), and a project the size of the one measured (salonpro) sits close enough to that margin
that declaring the risk without acting on it would be exactly the kind of gap this project's own
doctrine treats as unforgivable — an undeclared limit that quietly becomes a broken tool call.

**Pick a number closer to the measured bracket's floor (e.g. 60 000, just under 64 097).**
Rejected: that would restore almost the same thin margin the bisection just showed to be
insufficient, and it ignores that the client's real limit is in tokens, not bytes — a bytes
number chosen right at the edge of one content shape's bracket has no slack for a denser one.

**Wait for the structural entry cap (hard limit per bucket) instead of recalibrating the byte
number now.** Rejected as the immediate answer, for the same reason ADR 0061 gave for not
waiting on the full I3 delivery layer: the structural cap is real follow-on work (design,
ranking-stability guarantees, its own tests), and the reachable, measured defect — a budget
whose margin against a real client was never actually checked — does not need any of that
machinery to close.
