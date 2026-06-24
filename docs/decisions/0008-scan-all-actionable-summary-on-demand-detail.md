# ADR 0008 — codefit-scan-all: actionable summary + on-demand frontier detail

**Status:** Accepted · **Date:** 2026-06-24 · **Phase:** 1 (surface synthesis)

## Context

ADR 0006 made `codefit-scan-all` the per-endpoint synthesis and returned **every**
endpoint with its full concerns in one response (`Endpoints []EndpointReport`). On a
real backend (Bitácora) that is ~101 surface items plus the deterministic findings.
Verified across **4 real runs with 4 different models**: the response is large enough
that the MCP client truncates it or the agent dumps it to a temporary file and
delegates to a sub-agent to parse the giant JSON. The synthesis was correct but
**unusable in the loop** — the agent never reasoned the findings inline.

The bulk of that payload is **frontier** items: endpoints where the data leaves the
handler body (`local_access_detected=false`). codefit concluded nothing locally
about them — the agent will go to the code to follow the data regardless, so
shipping codefit's (absent) detail for them does not save the trip. They dominate
the volume and add the least.

## Decision

`codefit-scan-all` returns an **actionable summary**, with the frontier detail
**on demand**. The split is decided by a FACT codefit already computes, not an
arbitrary cut: did codefit **conclude locally** (`CertainConcerns>0` — at least one
deterministic finding or a surface_confirmed concern, i.e. `local_access_detected=
true`) or is **every** concern at the frontier.

### In the summary (returned directly)

- **Deterministic findings** (certainty 1.0): always.
- **Endpoints with ≥1 confirmed concern** go **whole** — *all* their concerns
  together (confirmed *and* any frontier concern of the **same** endpoint), because
  the agent reasons the endpoint, not loose concerns. Returned as `actionable`.

### On demand (named, not detailed)

- **Frontier-only endpoints** (no confirmed concern, no deterministic finding): the
  data escaped; the agent goes to the code anyway. They are **named** in
  `frontier_pending` (file + categories, one line each), not detailed.

### The summary DECLARES what it left out (honesty, not hiding)

`frontier_pending` carries `count`, the named `endpoints`, and a `note` telling the
agent how many there are, why they are not detailed (they are frontier, followed in
the code), and how to fetch any of them (`codefit-scan-endpoint`). The agent knows
the rest exists and how to get it. This is **prioritising while declaring**, not
hiding.

### Absence of actionables is NOT "clean"

When **nothing** was resolved locally (all frontier, `actionable` empty), the note
states it **emphatically**: codefit concluded nothing locally, this is **NOT a clean
result**, every endpoint requires following the data in the code. Same principle as
the frontier signal wording (ADR follow-up): an empty actionable set must never read
as "codefit found nothing → clean", because that is exactly the misread that makes an
agent discard real work. The genuinely empty project (no surface, no findings) is
distinct and communicated by `summary.endpoints == 0`.

### New tool: `codefit-scan-endpoint`

Re-analyses **one file** on demand and returns its endpoints' full concerns (same
concern contract as scan-all). **Stateless**: it re-runs the static analysis over
the project and filters to the requested file — it stores nothing and retrieves
nothing. codefit does not keep the items waiting to be asked for; it recomputes the
request. Static analysis is cheap, and re-running the same pipeline guarantees the
detail is identical to what scan-all would have shown for that endpoint. This
preserves the stateless design (PRD §15): no session state, every call carries what
it needs.

## Consequences

- The scan-all response shrinks to the deterministic findings + the locally-resolved
  endpoints + a named list of the rest — a fraction of the raw item dump. It fits in
  an MCP response without truncation, so the agent reasons it inline.
- The real findings (the locally-resolved endpoints and every deterministic finding)
  are **always** in `actionable`, complete — the truncation never drops them.
- One more round-trip to get a frontier endpoint's detail, by design: that detail is
  low-value (the agent follows the data in the code anyway), so paying for it only
  when asked is the right trade.
- **Breaking change** to the scan-all output contract: `Endpoints` is replaced by
  `Actionable` + `FrontierPending`. Acceptable pre-release (Phase 1, no public tag).

## Rejected alternatives

- **Keep the full dump (ADR 0006 as-is).** Refuted by the 4-model truncation
  evidence: correct but unusable in the loop.
- **Truncate to top-N by some score.** Silent caps read as "covered everything"; and
  any score here is a severity judgment codefit must not make. Splitting by the
  local-resolution **fact** keeps it a fact, and `frontier_pending` declares the rest
  instead of hiding a cut.
- **Persist the items and page through them.** Breaks the stateless design and adds a
  store codefit deliberately does not have. Re-analysis on demand is cheap and keeps
  the server stateless.
