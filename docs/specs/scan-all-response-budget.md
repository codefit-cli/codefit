# Spec — `scan-all` fits in the client that has to read it

**Status:** draft · **Phase:** 3, thread H0 (unplanned, blocking) · **Target:** `v0.2.7`

## The defect

`codefit-scan-all` over a real 317-file TypeScript project returned **313,284 bytes** and
**exceeded the MCP client's limit**. The tool an agent is told to call first did not return.

Measured breakdown of that response:

| section | bytes | |
|---|---|---|
| `actionable` | 311,099 | **99.3%** |
| `frontier_pending` | 1,711 | 14 endpoints, ~122 bytes each |
| `resolved_clean` + `baseline` + `score` + `summary` | ~400 | |

`actionable` held 160 endpoints carrying 367 concerns inline, ~794 bytes per concern.

**This is not a new problem, it is an old decision applied to only half the response.** PRD §21
records that the full canonical dump truncated in MCP clients, which is exactly why `scan-all`
returns a three-bucket synthesis instead (ADR 0006/0008). `frontier_pending` follows that
discipline — it *names* its endpoints and says to fetch detail with `codefit-scan-endpoint`.
`actionable` never did. It inlines everything.

So this is not a limit to declare. codefit is MCP-only; if its primary tool cannot return on a
mid-sized project, codefit does not work on real projects. Documenting that would not be honesty,
it would be capitulation.

## R1 — `actionable` names its endpoints; it does not inline their concerns

Same shape `frontier_pending` already uses. Per endpoint the agent gets enough to **rank and
choose**, and nothing more:

- `file`, `line`
- how many concerns, and of which categories
- the highest certainty present
- whether any concern is a deterministic affirmation (those are facts, not questions, and an
  agent must not have to fetch a file to learn one exists)

The `question` and `signals` text — the ~794 bytes — moves behind `codefit-scan-endpoint`, which
already exists, is stateless, and recomputes the same analysis, so the detail it returns is
identical to what would have been inlined (ADR 0008).

At `frontier_pending`'s density this turns 311 KB into roughly 20 KB.

## R2 — A deterministic finding is never demoted to a name

Surface items are questions; the agent decides which to pursue and the summary is enough to
choose. A deterministic finding at certainty 1.0 is a **fact codefit already concluded**, and
hiding it behind a second call would make a scan's headline result depend on the agent choosing
to look. Deterministic findings stay in the response in full. There was exactly 1 in the
measured project; they are rare by construction and are not what makes the payload big.

## R3 — A response that still does not fit says so

Naming instead of inlining is a large constant-factor win, not a bound. A monorepo with thousands
of endpoints will exceed any budget again, and **the failure mode must not be a truncated
response that reads like a complete one** — the same principle the change scope established in
ADR 0048.

So `scan-all` carries a declared budget. When the endpoint list would exceed it, the response
returns the highest-ranked endpoints and states, in the response itself, exactly how many were
withheld and on what ordering, so the agent knows it is holding a prefix and can ask for more.
Silent truncation is the one outcome forbidden.

## R4 — Only the RENDERING narrows. Nothing else moves.

This is the requirement that keeps the fix from repeating the bug the change scope had to close.

The score, `blocked`, the baseline delta and the `by_dimension` breakdown are computed over the
**complete** analysis, exactly as today. Withholding detail from the payload must not remove a
finding from the baseline diff, must not change a fingerprint, and must not alter a score.

Test-locked: for the same project, the pre-change and post-change responses must agree
**field-for-field** on `score`, `blocked`, `baseline` and `summary`, and the set of endpoints
named in `actionable` must equal the set that was previously detailed there. What changes is how
much of each endpoint is spelled out — nothing about what codefit concluded.

## Out of scope

- No rule changes. No finding, surface item or baseline fingerprint moves.
- `codefit-scan-endpoint` is not redesigned; it already returns full per-endpoint detail.
- The tool descriptions in `internal/mcp/server.go` and the generated skill will need to teach
  the new shape — that is part of the change, not a follow-up, because they are the only thing an
  agent reads before choosing a tool.

## Test contract

Each proven by **mutation**: break the exact behavior, watch it fail, restore, watch it pass.

1. A response over a corpus that previously exceeded the limit now fits the budget, measured in
   bytes on the real serialized payload — not on a fixture small enough to pass either way.
2. `score`, `blocked`, `baseline` and `summary` are field-for-field identical to the pre-change
   response for the same project. *(The mutation: compute the score over the rendered subset.)*
3. The set of endpoints named in `actionable` equals the set previously detailed there — the fix
   drops detail, never endpoints.
4. A deterministic finding is present in full in the response and never behind a second call.
5. Every endpoint named in `actionable` can be fetched with `codefit-scan-endpoint`, and what
   comes back matches what the old response inlined for it.
6. When the budget is exceeded, the response states how many endpoints were withheld and the
   ordering used; a response that withholds silently fails the test.
7. The per-endpoint summary carries enough to rank without fetching: counts, categories, highest
   certainty, and whether an affirmation is present.
