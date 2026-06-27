# ADR 0011 — Acceptance safeguard graduated by certainty

**Status:** Accepted · **Date:** 2026-06-27 · **Phase:** 1 (baseline, RF-08)

## Context

Once the baseline silences `known` items (ADR 0010), the question is **what is allowed
to become silent, and how easily**. A naive baseline silences everything it has seen
before. But codefit emits two epistemologically different things, and treating them
the same is dangerous:

- A **surface item is a QUESTION.** codefit enumerated a structure ("this handler
  reaches a resource by id; no known authz helper in the body") and asks the agent to
  judge it. codefit does not claim it is a vulnerability.
- A **deterministic finding is an AFFIRMATION.** codefit asserts a fact at certainty
  1.0 ("this is `md5()` used for security", "this matches a hardcoded-secret pattern").

Silencing a *question* the team has already looked at is fine — that is the noise the
baseline exists to remove. Silencing an *affirmation* is graver: it hides something
codefit is *sure* about. If an affirmation went `known` automatically (just because a
previous scan saw it), a real, confirmed vulnerability could disappear from the report
with **no human ever deciding it was acceptable**. That is the exact failure mode
codefit exists to prevent — a false "all good".

## Decision

The safeguard is **graduated by certainty** — the stronger codefit's claim, the
stronger the consent required to silence it:

| codefit emits | nature | default on re-scan | to silence |
|---|---|---|---|
| surface item | a question | becomes `known`, **auto-silenced** | accept (mark false positive) |
| deterministic finding | an affirmation (1.0) | **never auto-silenced — shown every scan** | explicit human `accept` with a reason |

Mechanically (`baseline.Diff`): an acknowledged item is silenced regardless of type; a
`known` surface item is silenced; a `known` **affirmation is still shown** (counted as
`affirmations_shown`). Only an explicit `codefit-baseline-accept` (a human decision
with a mandatory reason, recorded `by: human`) silences an affirmation.

This **unifies acceptance in the baseline**: it removes the separate
"consent in `.codefit.yaml`" path that earlier designs reserved for critical findings.
There is now one place a decision to accept anything is recorded, with the safeguard
scaled to the certainty of the claim.

## Consequences

- A confirmed vulnerability (e.g. a hardcoded secret) cannot vanish from the report by
  itself; it nags on every scan until a human writes down why it is acceptable.
- "Accept a false positive" stays low-friction for surface (the common case), where
  codefit only asked a question.
- The agent must never accept on its own — codefit records `by: human` but cannot
  verify it; the skill enforces the discipline (ADR 0012).
- One acceptance mechanism, auditable (reason + date), instead of two.
