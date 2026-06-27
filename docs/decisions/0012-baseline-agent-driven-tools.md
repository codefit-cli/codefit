# ADR 0012 — Baseline operations are agent-driven MCP tools; codefit never edits code

**Status:** Accepted · **Date:** 2026-06-27 · **Phase:** 1 (baseline, RF-08)

## Context

The baseline needs three operations beyond scanning: see what is tracked, accept an
item, and drop resolved items. The question is **who runs them and how**. Options
ranged from CLI flags the developer types (`codefit baseline accept <id>`) to codefit
mutating things on its own. Both are wrong for codefit's model: it is MCP-first (no
audit CLI), stateless, and bound by the autonomy principle — *the developer always
decides; codefit never acts over their decisions*.

There is also a hard line codefit must never cross: it audits code, it does not change
it. The baseline must not become a backdoor to mutating the project.

## Decision

The baseline operations are **MCP tools the agent drives, on the human's instruction**,
expressed in codefit's own skill — the developer talks in plain language, the agent
operates:

- **`codefit-baseline-list`** — read-only. Returns the tracked items
  (fingerprint, file, category, state; reason+date if acknowledged), filterable by
  `known` / `acknowledged`. It exists so the agent references items **without reading
  `.codefit-baseline` directly** — the agent must not parse codefit's internal file;
  it asks codefit. (The acknowledged reason is truncated in this projection so a
  baseline with many accepted items stays under the MCP truncation line, ADR 0008; the
  full reason stays in the file.)
- **`codefit-baseline-accept`** — records a human's decision (mandatory reason,
  `by: human`). The skill states it explicitly: accept **only** when the human decided
  so in the conversation, **never** on the agent's own judgment (ADR 0011).
- **`codefit-baseline-prune`** — re-scans to confirm items are `gone`, then removes
  them. Distinct from accept: accept = code stays, we decided it is fine; prune = the
  code changed and the item disappeared.

**The golden rule, enforced across all three:** codefit reads code and reads/writes
**its own baseline file only**. It never runs git and never edits a project file. A
fix is the agent's and the human's; `prune` *detects* that code changed, it does not
change code. Every operation is **narrated** to the human by the agent (what was
accepted or pruned, and why) — transparent, never silent.

## Consequences

- The full loop (scan → reason → human decides → accept/fix/prune → re-scan) lives in
  the agent conversation; the developer never leaves their agent or types a codefit
  command.
- The abstraction holds: the baseline file is codefit's, reached only through tools.
- codefit has no power over the project's code or git — consistent with the blocking
  model (it informs `blocked`; the dev acts).
