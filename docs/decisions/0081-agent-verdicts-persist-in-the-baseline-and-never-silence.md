# 0081 — Agent verdicts persist in the baseline, and only a human silences

- Status: accepted
- Date: 2026-08-20
- Supersedes: nothing directly. Strengthens `surface.Integrate`'s validation
  (the `SurfaceID != StableID(...)` check, which only proves the request is
  internally self-consistent, never that the item still exists) and closes
  ADR [0021](0021-by-dimension-scoring-wired-into-scan-all.md)'s stated
  dependency ("surface scores only after confirm-surface") — see the H4
  finding in `docs/roadmap.md`.

## Context

Measured against `main` before this change:

- `codefit-confirm-surface` takes the agent's verdicts, validates them,
  returns probabilistic findings — and persists nothing. Its request carries
  no `root`, so it could not write a baseline even if it wanted to.
- `scoring.Compute` is called in exactly one place (`internal/mcp/scanall.go`),
  inside the scan, before the agent has said anything.
- The baseline stores `fp / category / file / snippet / acknowledged`. There
  is no field for what an agent concluded, and `Ack.By` / `AuthzHelper.By`
  are each documented as "always human: codefit never acknowledges /
  registers on its own".

So the agent reasons, answers, and the answer dies in the conversation. The
next audit reasons the same items again — measured on a real field run as 323
open questions lost between passes. The PRD promises the opposite (§7): a
verdict "queda registrado para baseline, score y trazabilidad — no solo en la
conversación". `score.global: 100` beside those 323 open questions is not a
scoring defect: the score is computed at the one point in the protocol where
the only thing it can count is deterministic affirmations, and the path for
the agent's answer to reach it did not exist. This ADR builds that path.

Two constraints already existed and shaped every decision below:

- **Staleness is solved.** `findings.Fingerprint = sha256(category + file +
  normalizeContent(content))` (ADR 0009). A code edit changes the fingerprint,
  so a stored verdict stops applying the moment its content changes, and a
  reformat does not churn it.
- **The audit-protocol asymmetry (`docs/specs/audit-protocol.md`, I3)**:
  over-reporting is noise, harmless; under-reporting is "a false all-clear;
  unforgivable". Persisting a verdict raises the stakes on validating it.

## Decision

### D1 — an agent verdict is recorded, and never silences

`baseline.Item` gains `AgentVerdicts []AgentVerdict`, appended by
`(*Baseline).RecordVerdict`. Recording NEVER sets `Item.Ack` and NEVER changes
whether the item is shown on a scan — only a human, through the existing
`Accept` path (`codefit-baseline-accept`), silences an item. This is the
project's autonomy principle (`CLAUDE.md`) applied literally: the agent
recommends, the human decides. Locked by mutation M1: forcing `RecordVerdict`
to also set `Ack` makes the "does not silence" test fail (the item comes back
`acknowledged`); reverting makes it pass again.

### D2 — conflicting verdicts are both kept, never overwritten

Two verdicts on the same fp, opposing direction (`vulnerable` vs
`not_vulnerable`), are BOTH appended. `Item.InConflict() bool` is DERIVED —
true iff the list holds at least one `vulnerable` AND at least one
`not_vulnerable` (`uncertain` participates in neither direction) — never
stored as a flag. The baseline is a committed file read by binaries of
different versions over time; a stored flag written by one binary's
conflict rule would be trusted by a different binary's reader. Deriving means
every reader applies its OWN current rule to the raw facts. `Prune` and
`Accept` already mutate `Items` without touching any derived flag, which
would be a second silent-staleness path for a stored one. Locked by mutation
M2: replacing the append with an overwrite makes the "conflict keeps both"
test fail (1 entry, `InConflict()` false); reverting makes it pass again.

### D3 — a new tool in the `baseline-*` family, not an extension of `confirm-surface`

`codefit-baseline-record-verdict` is a new tool, not a new field on
`codefit-confirm-surface`. Three reasons: (a) ADR 0013 already decided this
exact question for authz helpers, on the same grounds (confirm-surface is
stateless); reusing that precedent is cheaper than reversing it. (b)
Persisting to a committed file must never be a hidden side effect of
reasoning. (c) `confirm-surface`'s request carries no `root`, so any
persisting path changes its shape regardless.

### D4 — the direction of a verdict decides who may act on it

A `vulnerable` verdict adds alarm — the safe direction, may be recorded
freely, no gate. A `not_vulnerable` verdict removes alarm — the direction
that is unforgivable if wrong (I3) — so it never removes an item from the
actionable/scored view on its own; only a human's `Ack`, via
`codefit-baseline-accept`, does that. This is enforced in two places over
this change's three slices: the baseline never auto-derives `Shown`/`Ack`
from an `AgentVerdict` (this slice), and slice 3's score fold skips any item
carrying a human `Ack` but never skips one for an unacked `not_vulnerable`
alone.

### D5 — the tool re-validates against a fresh re-analysis, not internal consistency

Today's `surface.Integrate` checks only that a submitted `SurfaceID`
recomputes from the submitted `(file, line, category)` triple — the request
is coherent with itself, never that the item exists. `HandleBaselineRecordVerdict`
replaces that floor for persistence: for each entry, after the cheap id
recompute check, it re-runs the security sensor
(`runSecurity(root, language, helpers, scope.Of(files))` — the same free
function `codefit-scan-endpoint` and `codefit-baseline-prune` already use,
never a direct call to a provider's `AnalyzeSurface`, whose items carry an
EMPTY `Fingerprint` until `stampFingerprints` runs inside the sensor) and
looks up a fresh item at that exact anchor. Only a match persists — using the
FRESH item's content-hash `Fingerprint` as the baseline `fp`, never the
line-based id, so ADR 0009 staleness still governs identity. A non-match is
refused and named (`no_surface_item_at_anchor`), never dropped silently.
Refusal is per-entry: a batch of N verdicts persists the valid ones and
reports the rest, never all-or-nothing. A whole-run failure (no provider for
the language, unreadable root) returns an error, never a silent empty
persist. Locked by mutation M3: regressing the check to accept any anchor
whose id merely recomputes (today's weaker floor) makes the "refuses a
moved/fixed item" test fail — the sharpest of the three mutations, because
the regressed behavior IS what `main` does today, proving this decision
strengthens rather than restates it.

An empty batch returns before any analysis runs: `scope.Of(nil)` resolves to
`scope.Full()` (an empty scope is deliberately never "audit nothing"), so
falling through for a zero-length batch would silently full-scan the whole
project for nothing.

### Shape

```go
type Actor string
const (
    ActorHuman Actor = "human" // Ack, AuthzHelper — silences
    ActorAgent Actor = "agent" // AgentVerdict only — never silences
)

type AgentVerdict struct {
    Verdict    surface.Verdict   `yaml:"verdict"`
    Reasoning  string            `yaml:"reasoning"`
    Confidence float64           `yaml:"confidence"`
    Severity   findings.Severity `yaml:"severity,omitempty"`
    At         string            `yaml:"at"`
    By         Actor             `yaml:"by"` // stamped by RecordVerdict, never caller-supplied
}
```

`Ack.By` and `AuthzHelper.By` are retyped `string` → `Actor` (wire bytes
unchanged: their literal `"human"` assignments still compile as untyped
string constants). `by` becomes a two-value vocabulary declared once, on
`Actor`'s own doc comment and the package doc's new third safeguard tier
(SURFACE / DETERMINISTIC / AGENT VERDICT). The three existing "always human"
field comments (`Ack.By`, `AuthzHelper.By`) are untouched and still literally
true — each is scoped to its own field. `Severity` is added beyond the
spec's original shape: `surface.severityFor` uses the agent's severity when
valid, else a class default; without persisting it, a later fold would
silently substitute the default and lose the agent's judgment about what
resource is exposed.

`Reasoning` is capped at 500 runes with a `…` marker, applied in
`RecordVerdict` itself (core, so every caller is bounded) — distinct from the
existing `maxReasonLen` (200 runes), which bounds only the LIST-time
projection and does not bound what gets written to disk. Verdict COUNT is
NOT capped: a code edit resets the fp (and its whole verdict history) to
empty (ADR 0009), so the list is self-bounding across time and only unbounded
within one fp's unchanged lifetime — inherently small. Every verdict record,
not only "vulnerable" ones, is kept: dropping `not_vulnerable` records would
silently break D2 (conflict detection needs both directions present).

### D6 — the cross-version data-loss hazard: accept + declare (A), and preserve unknown fields for next time (B)

`yaml.Unmarshal` is non-strict: an OLDER codefit binary reading a NEWER
baseline silently drops fields it does not know, and if that binary then
calls `Save` (e.g. via `baseline-accept`), the recorded `agent_verdicts` are
deleted from the committed file. The originally proposed exit — "bump
`Baseline.Version` so an old binary fails loudly" — does not exist as a
lever: `Version` is referenced in exactly two places in the whole repo
(`Load`'s and `Save`'s identical `if b.Version == "" { b.Version = "1" }`
default), never compared or validated. v0.2.6–v0.2.9 are already
distributed and ignore `Version` entirely.

Three exits were weighed:

| Exit | Cost | What it breaks | Verdict |
|---|---|---|---|
| **A — accept + declare** | ~0 production lines: this ADR, a CHANGELOG entry, one warning line in the header `Save` writes into `.codefit-baseline` itself | Nothing | **Chosen** |
| **B — preserve-unknown round trip** | ~small (`Baseline.unknown map[string]yaml.Node`, custom `UnmarshalYAML`/`MarshalYAML`, carried into `Diff`'s `Next`, which is built from scratch) | Nothing | **Chosen alongside A** — protects the NEXT format addition, not this one |
| **C — poison pill** (`version` as a YAML int, type-erroring the old `string` field) | ~5 lines | Every old-binary operation on the file (`scan-all`, `-accept`, `-prune`, `-list`) fails with a generic `parsing baseline %q: ...` and no upgrade instruction, until every teammate upgrades | Rejected |

**Why A over C**: the baseline is committed. The loss is silent IN THE TOOL,
not in version control — a run that destroys 300 verdicts produces a `git
diff` showing the deletion, and `git revert` recovers it. That is
categorically different from an unreported vulnerability (I3's "unforgivable"
direction): it is recoverable, visible-in-review data loss, in the place a
developer already looks every day. C buys fail-loud at the price of
hard-bricking every old-binary operation on the whole file, for a message it
cannot even word correctly (the upgrade instruction would have to live in the
already-installed old binary).

**Why B is included despite not fixing the shipped case**: cheap
(`internal/core/baseline/baseline.go`'s `Baseline.UnmarshalYAML`/
`MarshalYAML`), and it removes this exact hazard for every FUTURE field
addition to the format. Its trap, paid here so a future author does not
rediscover it: `Diff` builds `Next` from scratch, so the unknown-field
catch-all must be carried into `Next` explicitly (`Next: &Baseline{...,
unknown: prev.unknown}`) — Load/Save alone are not enough, because a
Load→Diff→Save round trip (what every scan actually does) would still drop
it.

**A fourth exit — a sidecar file** keyed by fp, which an old binary never
opens and thus cannot rewrite — would make loss impossible without forcing an
upgrade. Named, not recommended: it contradicts this ADR's own data contract
(`agent_verdicts` nested under `items:`) and doubles the merge-conflict
surface the baseline already accepts by being a single committed file.

The warning lives in the header `Save` writes into `.codefit-baseline`
itself (not only here), because that is where a developer on an old binary
might actually read it before wondering where 300 verdicts went.

## What this does NOT do

- It does not change `scoring.IsBlocked`, which runs over raw sensor findings
  in `HandleScanSecurity` only; `scan-all` does not compute it, and this
  change does not wire an agent's probabilistic judgment into it. The block
  gate stays reserved for deterministic critical security affirmations
  (PRD §18) — letting a probabilistic verdict force a hard block would
  invert the autonomy principle in the one place codefit deliberately has no
  dial.
- It does not fold a persisted verdict into `scoring.Compute`'s input — that
  is slice 3 of this change (R6/R7), landing separately.
- It does not report per-item agent reasoning in `codefit-scan-all`'s
  baseline delta — that is slice 2 (R4/R5).
- It does not solve the merge-conflict-frequency cost: the baseline is
  already committed and already conflicts on `Items` edits; every reasoning
  pass now writes, not just human accepts, so collisions across branches rise
  in frequency. No new merge machinery is proposed here — a named, accepted
  cost, not a solved problem.

## Alternatives rejected

**Store `InConflict` as a field, set on append.** Rejected under D2: a stored
flag can be read by a different binary version than the one that wrote it,
and `Prune`/`Accept` already mutate `Items` without a matching flag update —
two silent-staleness paths a derived function has zero of.

**Extend `codefit-confirm-surface` with an optional `root` and a `persist`
flag.** Rejected under D3: ADR 0013 already chose the opposite shape for the
structurally identical authz-helper question, and doing so here would still
change confirm-surface's contract for every caller, stateless or not.

**Validate only that the submitted id is self-consistent (today's
`Integrate` check), and accept it for persistence.** Rejected under D5: this
is exactly the floor mutation M3 proves insufficient — it is `main`'s current
behavior, and the failing "refuses a moved/fixed item" test under that
regression is what demonstrates the stronger re-validation is load-bearing,
not decorative.
