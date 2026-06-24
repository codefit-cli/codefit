# ADR 0006 — codefit-scan-all: the per-endpoint synthesis with three certainty levels

**Status:** Accepted · **Date:** 2026-06-23 · **Phase:** 1 (surface synthesis)

## Context

Running the three surface categories (IDOR, authz, over-fetching) over a real
project (Bitácora) produced 101 surface items plus the deterministic findings, as
three separate category lists. The detection was solid — all real findings were in
the actionable set — but a user calling the tools got three lists with the same
endpoint repeated across them (every IDOR handler is also an authz handler;
`tasks/process` appeared four times). The aggregate was three detectors
juxtaposed, not one picture. The synthesis layer was missing.

## Decision

`codefit-scan-all` is the complete picture, aggregated **by endpoint**. The unit
is the handler; its concerns come from **two sources** — the deterministic rules
and the mapped surface — and are presented **together**, because they are about
the same handler. codefit runs the deterministic sensor and the three surface
queries (already run together in the security sensor) and groups the result by the
handler each finding/item belongs to.

### Three certainty levels (epistemological honesty)

Each concern carries its certainty, and concerns are ordered within an endpoint
from hardest to softest:

1. **Deterministic** (certainty 1.0): a rule finding. codefit **affirms** it —
   `affirms=true`, `probabilistic=false`. Nothing to reason; it is a fact.
2. **Surface confirmed** (structural certainty): codefit saw the shape locally and
   **asks** the agent (`affirms=false`). E.g. IDOR with no authz detected,
   over-fetch from a local find with no select.
3. **Surface frontier** (uncertainty): the data left the handler body
   (`local_access_detected=false`); codefit could not see, and the agent follows.

The report **never flattens** these to undifferentiated "problems". A deterministic
finding is an assertion; a surface concern is a question. The agent must
distinguish what codefit affirms from what it asks — that is what `certainty`,
`affirms`, and `probabilistic`/`confidence` are for.

### Ordering — by actionable gap, hardest kind first, never by severity

Endpoints are ordered by their **actionable structural gaps** — the missing or
broken controls codefit detected — hardest kind first:

1. **affirmed** — a deterministic finding (an asserted vulnerability),
2. **access** — no authz/ownership check on a sensitive or id→resource handler,
3. **exposure** — a serialization with no select/omit (over-fetch),

then by certain-concern count. This is still ordering by **fact** (which control
is missing), not by severity — codefit never says one endpoint is "worse"; the
agent judges danger in its verdict.

**Why access ranks before exposure — and what it is NOT.** Access ranks before
exposure because of **structural reachability**, a fact about the endpoint: when
no authz/ownership check is detected, the barrier to entry is *absent* — anyone
can reach the handler — whereas an over-exposure gap sits on a handler whose
barrier is *present*. "Barrier present vs absent" is a structural property codefit
observes, not a judgment. This ordering does **NOT** assert that access problems
are more severe, more dangerous, or more important than exposure problems. It is
**not a severity ranking**: an exposure gap (e.g. a leaked password hash) may well
be more severe than a given access gap, and codefit takes no position on that.
Severity is the agent's to set in its verdict, per concern, with the domain
context codefit does not have. codefit only orders by the structural fact of
which control is missing — reachability barrier first — so the agent reviews the
unguarded handlers before the guarded-but-over-exposing ones; it ranks the
*review order*, not the *danger*.

**Rejected: ordering by count of concerns.** The first design ranked endpoints by
how many certain concerns they carried (more facts → higher). It is intuitively
reasonable — and it was wrong, refuted by the Bitácora data. Over-fetch with no
select is **ubiquitous** (every serialization of a Prisma find — 30/30 on
Bitácora had no select), so almost every endpoint carries at least one actionable
fact, and a count-only order then resurfaces the **most-instrumented** endpoints
on the certain-concern tiebreak — including ones that are fully protected. The
real findings did not come first: `debug/metrics-raw` (a sensitive endpoint with
no authz) sat mid-low with its single concern, below protected-but-instrumented
handlers like `admin/logbooks`. Ordering by the **kind** of gap (access before
exposure) puts the missing-access-control findings on top, where a missing check
means anyone can reach the endpoint — a structural fact about reachability, not a
severity score. This is recorded because "order by count" will look reasonable
again; the Bitácora datum (no-select is everywhere) is why it is rejected.

IDOR is marked as a structural **refinement** of authz when an endpoint has both
(the sensitive handler also receives a client id) — a fact, not a judgment.

### Agent-first; the human view is a later opt-in

The report is JSON for the **agent** to reason — not for direct human reading. A
human renderer (`export-report --format md|html`) is a future, opt-in addition
over this same canonical JSON (PRD §27); it is **registered as pending and not
built**. codefit never spends effort on presentation no one asked for.

### What scan-all does not do

It does not judge severity (the agent), does not filter (everything is present,
organized), invents nothing (every concern carries the id of a real finding or
surface item), does not flatten the deterministic/surface distinction, and does
not render for humans.
