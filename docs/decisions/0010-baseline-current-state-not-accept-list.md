# ADR 0010 — The baseline is a current-state snapshot, not an accept-list

**Status:** Accepted · **Date:** 2026-06-27 · **Phase:** 1 (baseline, RF-08)

## Context

A baseline can be modeled two ways. The traditional SAST model is an **accept-list**:
a file of suppressions the developer curates ("ignore these findings"). The other is
a **snapshot of the current state**: codefit's record of *everything it knows about
the surface right now*, against which a new scan is diffed.

codefit is MCP-first and stateless — it has no session memory — yet it needs to tell
the agent "what changed since last time" so a re-scan does not re-dump the whole
surface. And it must do that without codefit running git or holding state between
calls.

## Decision

The baseline is the **current-state snapshot**, persisted as `.codefit-baseline`
(committed). `codefit-scan-all` owns its lifecycle on every call:

1. **Read** the previous baseline.
2. **Scan** the code; fingerprint each observed item (ADR 0009).
3. **Diff** into four deltas, all by facts codefit computes:
   - `known` — fingerprint in both (seen before, unchanged),
   - `new` — observed, not in the baseline,
   - `changed` — a `new` that pairs with a `gone` at the same `(file, category)`
     (a content edit): the old fingerprint is replaced,
   - `gone` — in the baseline, not observed now.
4. **Write** the updated baseline and **report** the delta. The buckets are filtered
   to what is not yet tracked; `known` surface is silenced but counted.

`gone` items are **retained** in the baseline (flagged, reported as prune
candidates), not auto-dropped — that is why `codefit-baseline-prune` exists as a
separate, explicit operation (ADR 0012). State lives in the repo, not in codefit.

## Consequences

- A second identical scan yields `0 new` and an empty actionable view — the silence
  the baseline exists to provide (validated on Bitácora: 95 items → 0 new on re-scan).
- The baseline is committable and diff-able like any project config; the team shares
  one audit memory.
- An accept-list is a *subset* of this model (the acknowledged items, ADR 0011) — not
  a separate mechanism.
- The file grows until `prune` removes resolved orphans; pruning is deliberate, never
  automatic, so a transient scan miss cannot silently erase tracked debt.
