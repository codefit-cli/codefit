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

`codefit-scan-all` returns a **three-bucket summary**, one bucket per **resolution
level**, with full detail only where the agent can act and everything else named
on demand. Every split is decided by FACTS codefit already computes
(`CertainConcerns`, the actionable gap), not an arbitrary cut:

### 1. `actionable` — resolved locally AND has a gap (full detail)

Endpoints codefit resolved locally (`CertainConcerns>0`) that carry **≥1
unresolved gap** (`Actionable>0` — a deterministic finding, a missing access/
ownership check, or a serialization with no select/omit). They go **whole** — all
their concerns together (including any frontier concern of the **same** endpoint),
because the agent reasons the endpoint, not loose concerns. Deterministic findings
(certainty 1.0) always land here. These are what the agent acts on.

### 2. `resolved_clean` — resolved locally, NO gap (named + verification fact)

Endpoints codefit resolved locally (`CertainConcerns>0`) with **no gap**
(`Actionable==0`) — codefit **verified** the controls are present (an authorization
check, and field selection where data is serialized). They are **named** (file,
method) plus **one verification fact** per endpoint ("codefit verified locally: an
authorization check is present and field selection is present — no gap found"). Not
full detail, because there is nothing to act on; but **affirmed**, not hidden.

### 3. `frontier_pending` — not resolved locally (named)

Endpoints where every concern is frontier (`CertainConcerns==0`): the data left the
handler body, codefit concluded nothing locally. **Named** (file, method,
categories); the agent follows the data in the code. An endpoint here stays here
even if it carries a structural gap signal — `CertainConcerns==0` takes precedence,
because codefit did not resolve it locally.

### `resolved_clean` ≠ `frontier_pending` (the crux)

These two are **named** but they are **epistemological opposites**, and flattening
them into one "not detailed" bucket would repeat the very error of the old frontier
wording — making a positive verification read as a non-conclusion:

- `resolved_clean` is an **affirmation**: codefit looked locally and the controls
  are present. Its verification fact says so.
- `frontier_pending` is an **absence of conclusion**: codefit could not follow the
  data, so it asserts nothing — the agent must.

The agent must distinguish "codefit checked, it is clean" from "codefit could not
check". The verification fact communicates the check; the frontier note communicates
the limit. They are different on purpose.

### The summary DECLARES every bucket (honesty, not hiding)

`resolved_clean` and `frontier_pending` each carry `count`, their named `endpoints`,
and a `note`. Nothing is dropped silently: the agent sees how many endpoints are in
each state, why they are not detailed, and how to fetch any of them. This is
**prioritising while declaring**, not hiding.

### Absence of actionables is NOT "clean"

When **nothing** was resolved locally (`actionable` and `resolved_clean` both empty,
all frontier), the frontier note states it **emphatically**: codefit concluded
nothing locally, this is **NOT a clean result**, every endpoint requires following
the data in the code. Same principle as the frontier signal wording (ADR follow-up):
an empty actionable set must never read as "codefit found nothing → clean", because
that is exactly the misread that makes an agent discard real work. An empty
`actionable` with a populated `resolved_clean` is also honest — codefit checked and
those are clean (the verification facts say so), which is **not** the same as
"nothing found". The genuinely empty project (no surface, no findings) is distinct
and communicated by `summary.endpoints == 0`.

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

- The scan-all response shrinks to the gap-bearing endpoints (full detail) + the
  named clean and frontier lists — a fraction of the raw item dump. On the Bitácora
  backend: **80807 → 23988 bytes (29.7%)**, buckets **10 actionable / 11
  resolved_clean / 24 frontier_pending**. It fits in an MCP response without
  truncation, so the agent reasons it inline.
- The real findings (every gap-bearing endpoint and every deterministic finding) are
  **always** in `actionable`, complete — the truncation never drops them.
- One more round-trip to get a clean-or-frontier endpoint's detail, by design: that
  detail is low-value (a clean endpoint has nothing to act on; a frontier one is
  followed in the code anyway), so paying for it only when asked is the right trade.
- **Breaking change** to the scan-all output contract: `Endpoints` is replaced by
  `Actionable` + `ResolvedClean` + `FrontierPending`. Acceptable pre-release
  (Phase 1, no public tag).

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
