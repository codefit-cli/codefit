# ADR 0013 — Custom authz helper registry: agent reasons, human approves, baseline persists

**Status:** Accepted · **Date:** 2026-06-28 · **Phase:** 1.2 (surface recognition)

## Context

codefit recognizes authorization by a HARDCODED set of NextAuth-style helper names
(`getServerSession`, `auth`, `getToken`, `requireAuth`, …). Every project with its
OWN auth convention — `requirePermission`, `getAuthenticatedUserSalonId`, a custom
RBAC wrapper — falls into a massive false-negative: codefit reports
`known_authz_detected: false` on handlers that ARE checked, because it does not know
the project's helper by name. Dogfooding against a real app (salonpro, ~290 authz
surface items) made this concrete: nearly every item said "no known helper" though
the app guards them with a custom helper.

Three ways to close it, two rejected:

- **The developer configures a list.** Configuration rots, adds friction, and is
  exactly the kind of thing developers forget to update — the list drifts from the
  code and the blind spot returns silently.
- **codefit guesses (a heuristic).** "A function whose name contains `auth`/`perm`"
  is a name-based filter — the precise fragility ADR 0005 rejects. It would both
  miss helpers and wrongly clear unguarded handlers. codefit must not guess what is
  authorization; that is semantic reasoning.

The thing that CAN tell a project's authz helper from any other function is the
AGENT reasoning over the code — semantic work, the agent's job. What it should not
do is re-reason it on every scan (wasted context) or decide it alone (a registration
silences many items at once).

## Decision

The agent **reasons** which functions are the project's authz helpers; a **human
approves**; codefit **persists** the approved helper in the committed baseline and
**recognizes** it on later scans — so the agent never re-reasons it.

```
codefit enumerates (known_authz_detected:false for unknown helpers)
   → agent reasons: "these N items call requirePermission, which is the project's authz helper"
   → agent PROPOSES to the human; human DECIDES
   → codefit-baseline-register-authz-helper persists it (by:"human", reason, date)
   → later scans recognize it → known_authz_detected:true, no re-reasoning
```

### Persistence reuses the baseline (project knowledge)

Registered helpers live in a new `authz_helpers` section of `.codefit-baseline`,
beside the acknowledged items — committed, shared like the rest of the baseline.
Each entry is `{name, language, reason, at, by:"human"}`. A scan loads them once and
adds them to the built-in set for that project and language. `baseline.Diff` carries
them forward (they are project knowledge, not a per-scan observation). The fingerprint
of a surface item is `category+file+snippet` and does NOT include the facts, so
registering a helper does not churn the baseline — items stay `known`, their fact
just flips.

### A new tool, not an extension of confirm-surface

`codefit-baseline-register-authz-helper {root, language, helper_name, reason}` (with
`-unregister-` to reverse it — the developer's decision is always reversible). It is
its own tool, in the `codefit-baseline-*` family, NOT part of `confirm-surface`:
confirm-surface integrates a verdict on ONE item into the report and is stateless
(no persistence). Registering a helper declares a project-wide PATTERN affecting many
items and PERSISTS it as a human decision — the nature of `baseline-accept`, not of
confirm-surface. `reason` is mandatory, recorded `by:"human"`, exactly like accept.

### Safeguard 1 — registration is a human decision (ADR 0011)

Registering silences the authz gap on EVERY item that calls the helper (290 items
with two helpers on salonpro) — far more reach than an accept (one item). So it
inherits, reinforced, the accept discipline: the agent NEVER registers a helper on
its own. It PROPOSES; the human decides; codefit records `by:"human"` (it cannot
verify it — the skill enforces the discipline). The skill says it explicitly: "never
register a helper without the human; a wrongly-registered helper silences the authz
fact on dozens of real items."

### Safeguard 2 — the helper changes a FACT, not a VERDICT (ADR 0005, ADR 0006 amended)

Registering a helper flips `known_authz_detected` false→true — the fact "this helper
is present" — and reorders the AUTHZ concern to resolved (the authz question, "is the
caller permitted?", is answered). It does NOT clear the **IDOR/ownership** gap: an
IDOR endpoint guarded by the helper STAYS actionable, because the helper proves
authentication/permission, never that the caller owns THIS resource — which codefit
cannot verify from structure. This rests on the [ADR 0006](0006-scan-all-endpoint-synthesis.md)
amendment that decoupled the IDOR gap from `known_authz_detected`. A registered
helper that authenticates but does not check ownership therefore cannot produce a
false "resolved_clean" on a real IDOR — a false green is worse than an honest red.
The signal labels a registered helper as such ("registered for this project") for
traceability — the fact says what was seen and where it came from, never "this is
secure".

## Consequences

- A project teaches codefit its auth convention ONCE (human-approved); the agent
  stops re-reasoning recognized helpers, saving context on every later scan.
- The recognition is per-project and per-language, with no global config to rot.
- The blind spot is closed without a heuristic and without weakening IDOR: the
  asymmetry of ADR 0005 holds — codefit reports facts, the agent and the human judge.
- Scope: the file-level `codefit-surface-*` tools (which take files, not a project
  root) keep using the built-in set only — registered helpers are project knowledge,
  applied by the project-scan tools (`scan-all`, `scan-endpoint`, `scan-security`).
