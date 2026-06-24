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
| `0.1.0`  | Phase 1   | TS provider + security sensor + surface mapping + **`init` (AGENTS.md), baseline, `update`** all functional |
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

- **`v0.1.0-alpha.1`** (tagged 2026-06-24) — the usable, dogfooded MCP core:
  deterministic TypeScript security detection, the three surface categories (IDOR,
  broken authorization, over-fetching), the `scan-all` three-bucket summary with
  `scan-endpoint` on demand, all over the MCP stdio server (`codefit mcp serve`).
  Validated in real use against a Next.js/Prisma backend with multiple agent models.
- **`v0.1.0`** is **reserved** for Phase 1 complete — it ships once `codefit init`
  (writes `.codefit.yaml` + enriches `AGENTS.md` with confirmation), baseline, and
  `update` are functional. They are stubs today, so tagging `v0.1.0` now would
  over-promise.

## How to tag a release

```bash
# Annotated tag on a clean main with the gate green:
git tag -a v0.1.0-alpha.2 -m "codefit v0.1.0-alpha.2 — <milestone>"
git push origin v0.1.0-alpha.2

# Verify the build embeds it:
make build && ./bin/codefit version   # → codefit v0.1.0-alpha.2 (commit …, built …)
```
