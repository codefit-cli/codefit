# 0054 — `actionable` names its endpoints, and the response declares its own budget

**Status:** accepted · **Date:** 2026-08-04 · **Supersedes nothing; finishes applying
[ADR 0006](0006-scan-all-endpoint-synthesis.md) and [ADR 0008](0008-scan-all-frontier-named-not-detailed.md)**

## Context

`codefit-scan-all` over a real 317-file TypeScript project returned **313 368 bytes** and
**exceeded the MCP client's output limit**. The tool the skill tells an agent to call FIRST
did not return. codefit is MCP-only: a primary tool that does not return on a mid-sized
project means codefit does not work on real projects.

The measured breakdown says exactly where the bytes were:

| section | bytes | |
|---|---|---|
| `actionable` | 311 097 | **99.3 %** — 160 endpoints, 367 concerns inline, ~794 bytes each |
| `frontier_pending` | 1 711 | 14 endpoints, ~122 bytes each |
| everything else | ~560 | |

**This was not a new problem. It was an old decision applied to only half the response.**
PRD §21 records that the full canonical dump truncated in MCP clients, which is why
`scan-all` returns a three-bucket synthesis at all (ADR 0006), and ADR 0008 established
that a named endpoint plus a stateless `codefit-scan-endpoint` call is worth more than an
inlined one. `frontier_pending` and `resolved_clean` followed that discipline from the
start. `actionable` never did — it inlined everything, and it is the bucket that holds
most of a real project.

## Decision

### 1. `actionable` names its endpoints; it does not inline their concerns

Each actionable endpoint is rendered as a summary carrying exactly what it takes to RANK
and CHOOSE, and nothing more: `file`, `line`, `method`, how many concerns and of which
`categories`, how many are actionable and of which `gaps` (hardest kind first), the
`highest_certainty` codefit reached, and `has_affirmation`.

The `question` and `signals` text — the ~794 bytes per concern — moves behind
`codefit-scan-endpoint`, which already exists, is stateless, and re-runs the same
analysis, so what it returns is byte-for-byte the detail that was withheld (ADR 0008).

### 2. A deterministic finding is never demoted to a name — and never withheld

Surface items are questions the agent chooses among; the summary is enough to choose. A
deterministic finding at certainty 1.0 is a **fact codefit already concluded**. Hiding it
behind a second call would make a scan's headline result depend on the agent choosing to
look. They stay in the response in full, in `deterministic_concerns`.

The budget honours the same rule: an endpoint carrying a deterministic finding is **never
droppable**. There was exactly one in the 313 KB response; they are rare by construction
and they are not what makes a payload big. When pinning them means the response cannot fit,
the response says it is over budget rather than trading a fact for a number.

### 3. A response that still does not fit says so

Naming instead of inlining is a large constant factor, not a bound: a monorepo with
thousands of endpoints exceeds any budget eventually. So `scan-all` carries a **declared**
budget (`ResponseBudgetBytes`, 60 000 — MCP clients cap tool output, Claude Code's default
is 25 000 tokens, and this dense JSON runs around 3 bytes per token) and a `budget` block
in every response.

When the endpoint lists do not fit, whole buckets are withheld lowest-priority first —
`resolved_clean` (an affirmation, nothing to do), then `frontier_pending` (named-only
either way), then `actionable` — and within a bucket the lowest-ranked entries go first.
The response then states how many endpoints are missing and what ordering they are a prefix
of. Each bucket keeps declaring the COMPLETE `count` it classified, with `withheld`
accounting for the difference.

`withheld: 0` still carries a note. "No mention of truncation" and "nothing was truncated"
must not be the same bytes on the wire — that ambiguity is exactly how a clipped response
comes to read like a complete one ([ADR 0048](0048-change-scope-is-an-agent-supplied-input-and-the-baseline-scope-is-two-dimensional.md)).

### 4. Only the RENDERING narrows

This is the requirement that keeps the fix from repeating the bug the change scope had to
close. `buildScanAll` computes the COMPLETE analysis — score, `by_dimension`, the baseline
diff, the summary, the scope block, every bucket count — and only then does
`withNamedActionable` name and cut. Nothing downstream of that split is ever read back into
a conclusion.

Test-locked in both directions: against a golden captured from the pre-change tree
(`79e34b0`), and by running the same project at two wildly different budgets and requiring
`score`, `baseline`, `summary` and `scope` to agree field-for-field. If any of them were
computed over the rendered subset, the starved run would disagree.

## Consequences

- **Measured on the real corpus** (`go test -tags dogfood`): salonpro 313 368 → **42 012
  bytes**, 0 endpoints withheld, all 160 actionable endpoints still named. bitacoras
  40 282 → 9 903. The dogfood harness fails if no project in the corpus would have blown
  the budget under the old shape, so the measurement cannot quietly become a statement
  about small projects ([ADR 0052](0052-optimizations-are-validated-by-a-committed-dogfood-harness-over-real-projects.md)).
- **An agent now makes a round trip per endpoint it pursues.** That is the trade ADR 0008
  already made for the frontier, and it is cheap: static analysis is re-run, not retrieved,
  so the detail is guaranteed identical and codefit stores nothing.
- **Both agent-facing sources changed with the code**, because they are the only thing an
  agent reads before choosing a tool: the `codefit-scan-all` / `codefit-scan-endpoint`
  descriptions in `internal/mcp/server.go`, and the generated skill
  (`internal/scaffold/skill.go`), regenerated through `codefit init`.
- **The budget only governs the endpoint lists.** A pathological `db` section could still
  exceed it on its own; that path emits the explicit over-budget warning rather than being
  clipped. Narrowing a `db` section is a separate decision and is deliberately not made
  here.
- **No rule changed.** No finding, surface item or baseline fingerprint moves; `COVERAGE.md`,
  `internal/core/dbcoverage/` and the per-language `coverage.go` manifests are untouched —
  this ADR changes how a result is rendered, never what codefit detects.
