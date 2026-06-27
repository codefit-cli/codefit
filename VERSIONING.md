# Versioning

codefit follows [Semantic Versioning 2.0](https://semver.org/spec/v2.0.0.html) with
pre-releases, mapped to the PRD rollout phases (PRD §25). This document is the
contract for what each number means and when it moves — so the version is honest and
consistent over time, like the rest of the docs.

## The scheme

While codefit is pre-stable it stays on **`0.x`**. The version is derived from the
nearest git tag at build time (`git describe --tags`), so `codefit version` always
reflects the real commit, never a hand-set string.

```
0 . MINOR . PATCH [ -PRERELEASE ]
    │       │       │
    │       │       └─ alpha / beta / rc — see "Pre-releases" below
    │       └───────── bug fixes and small additions within the current phase
    └───────────────── one PRD phase completed (see the map below)
```

### MINOR ↔ PRD phase

Each PRD phase that **closes** raises the MINOR. The MINOR lands (no pre-release
suffix) only when the phase is **complete and usable end-to-end from `main`** — same
honesty rule as the README and CHANGELOG: we do not announce a phase as done while
pieces of it are still stubs.

| Version  | PRD phase | Meaning |
|----------|-----------|---------|
| `0.1.0`  | Phase 1   | TS provider + security sensor + surface mapping + **`init` (config + skill) and baseline** functional (`update` is Phase 4) |
| `0.2.0`  | Phase 2   | DB sensor (OLTP/OLAP, indexes, views, procs, N+1) |
| `0.3.0`  | Phase 3   | Code review + best practices + tests + regression risk |
| `0.4.0`  | Phase 4   | Knowledge packs + coverage manifest + public `v0.1.0`-class release |
| `1.0.0`  | —         | Stable API; post-1.0 brings Java (`1.1`), Python (`1.2`) |

> The Phase 4 row is where the project first cuts a public `0.x` release; the table
> maps phases to MINORs, the actual public-release milestone is tracked in the PRD.

### PATCH

Bug fixes and small additions that do not close a phase raise the PATCH within the
current MINOR line (e.g. a fix after `0.1.0` → `0.1.1`).

## Pre-releases

Before a MINOR is complete, builds toward it carry a pre-release suffix on the
**target** version, ordered `alpha < beta < rc < (final)`:

- **`-alpha.N`** — usable core of the phase, validated, but the phase is not
  feature-complete (pieces still missing or stubbed).
- **`-beta.N`** — feature-complete for the phase, stabilising; APIs may still shift.
- **`-rc.N`** — release candidate; no known blockers, final checks only.

A pre-release tag like `v0.1.0-alpha.1` means "on the way to `0.1.0`, at the alpha
stage" — it does **not** claim `0.1.0` is done.

## Current state

- **`v0.1.0` — Phase 1 complete.** Usable end-to-end from `main`: the MCP stdio
  server, deterministic TypeScript security rules, surface mapping (IDOR / broken
  authz / over-fetching), the `scan-all` three-bucket synthesis with `scan-endpoint`
  on demand, `codefit init` (config + self-discovering skill), and the **baseline**
  (committed audit memory with list / accept / prune). Validated in real use against a
  Next.js/Prisma backend. Cut from `main` as part of the documentation-sync release
  that closes Phase 1.
- On the way to it: **`v0.1.0-alpha.2`** (2026-06-25, added `codefit init`) and
  **`v0.1.0-alpha.1`** (2026-06-24, the first usable MCP core).
- `codefit update` is a Phase 4 item (`0.4.0`), not a Phase 1 blocker.

## How to tag a release

```bash
# Annotated tag on a clean main with the gate green:
git tag -a v0.1.0 -m "codefit v0.1.0 — Phase 1 complete"
git push origin v0.1.0

# Verify the build embeds it:
make build && ./bin/codefit version   # → codefit v0.1.0 (commit …, built …)
```
