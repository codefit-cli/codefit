# ADR 0023 — N+1 is per-endpoint SURFACE, not a deterministic DB rule

**Status:** Accepted · **Date:** 2026-07-17 · **Phase:** 2.2 (DB debt Slice A — N+1 / RF-04)

## Context

RF-04 (PRD DB-201) asks codefit to catch the N+1 query antipattern: a database
query issued once per iteration of a loop, turning one logical read into N. It is
one of the most common performance defects in AI-generated data-access code, and
invisible during normal development — exactly codefit's mandate.

The tempting placement is a DB-dimension rule beside DB-050/DB-011/DB-020, reading
the neutral `db.Schema`. That is wrong on two counts. First, N+1 is not a property
of the SCHEMA — it is a property of the CODE that queries the schema (a query call
lexically inside a loop). The neutral DB model has no loops. Second, whether a
given query-in-a-loop actually hurts is a judgment: a loop over three literal
elements and a loop over an unbounded result set are structurally identical; only
context decides if it matters. codefit does not fake that judgment (ADR 0004, ADR
0005).

## Decision

### N+1 is SURFACE, emitted by the language provider, never a `dbrules.Rule`

The detector `nplus1Query` lives in the TypeScript provider
(`internal/providers/typescript/nplus1.go`) and implements `surface.Query`
(`internal/core/surface/framework.go:27`). Its `Enumerate` returns
`[]findings.SurfaceItem` (`nplus1.go:25`) — it constructs no `findings.Finding`
anywhere. The queryable category is `CategoryNPlus1 = "nplus1"`
(`internal/core/surface/surface.go:11`), dimension `db`. DB-201 is a PRD/prose id
only; there is no DB-201 rule constant and no file in `internal/core/dbrules/`.
This placement is locked by `TestCoreDB_DBRules_NeverImportTypeScriptProvider`:
the core DB rule set structurally cannot depend on a provider.

Why the provider and not the core: detecting a query-inside-a-loop needs the
language's AST (loop constructs, call sites), which only the provider parses. The
neutral DB model is loop-free by design; forcing N+1 into it would drag code-shape
concepts into the schema layer and break the layering doctrine (ADR 0014, 0016).

### ORDERED, never FILTERED — the structural fact is stated, the agent judges

Per ADR 0005, N+1 surface is ORDERED by certainty, never FILTERED by a guess at
impact. A loop over a three-element literal array is enumerated **exactly** like a
loop over an unbounded query result (`nplus1.go:20-22`). This is not just
documented — `describeIteratedSource` (`nplus1.go:168`) names a literal array's
element count as a reported FACT ("a literal array of N element(s)"), never a
reason to drop the item. The MCP handler repeats the same discipline
(`internal/mcp/surface.go:95`). A false green (suppressing the "small" loop) is
worse than an honest red the agent dismisses in one glance.

### Reuse of the IDOR/authz frontier, verbatim — no separate detector

N+1 reuses the existing surface machinery rather than reimplementing handler
discovery or call classification: `auditTargets` (`idor.go:105`) for handler
discovery, `isPrismaCall` (`idor.go:486`) and `isServiceCall` (`overfetch.go:268`)
for query-call classification — the same frontier semantics IDOR, broken-authz,
and over-fetching already use (`nplus1.go:30,92,95`). A query call is a local
Prisma access OR a cross-function-frontier service call, identical to the other
three surfaces. Because it rides `auditTargets`, it applies uniformly across
Next.js / Express / Fastify / NestJS with no per-framework detector, and is wired
once into the provider (`typescript.go:177`).

### Mapped as per-ENDPOINT surface, into scan-all's endpoint buckets

N+1 is exposed by the MCP tool `codefit-surface-nplus1`
(`internal/mcp/mcp.go:17`, registered `server.go:40`) and folds into scan-all's
per-endpoint buckets (ADR 0006), never the schema-only DB section. It is a
property of an endpoint's handler code, so it is reported where the endpoint
surface is reported.

## Alternatives considered

- **A `dbrules.Rule` over the neutral schema** — rejected: the schema has no
  loops; N+1 is a code-shape property, not a schema property. Would force loop
  concepts into the neutral model and break layering (ADR 0014).
- **A deterministic Finding ("N+1 detected")** — rejected: whether a
  query-in-a-loop matters is contextual (bounded vs. unbounded, cached vs. hot);
  codefit maps the surface and the agent judges (ADR 0004, 0005).
- **A per-framework N+1 detector** — rejected: reusing `auditTargets` and the
  frontier helpers gives uniform coverage across all frameworks for free.

## Consequences

- The N+1 surface ships as a TypeScript-provider capability, one new category
  (`nplus1`), one new MCP tool, and endpoint-bucket wiring. Core DB rules,
  sensors, and the reducer are untouched.
- Because it is surface, not a finding, N+1 never blocks a commit on its own
  (`scoring.IsBlocked` is security-critical only) — it informs, the agent decides.
- Coverage prose (`internal/providers/typescript/coverage.go`, mirrored in
  `COVERAGE.md`) documents N+1 as Reasoning surface, explicitly per-endpoint,
  never the DB section.

## Related

- ADR 0004 — deterministic rule vs mapped surface.
- ADR 0005 — an honest red over a false green (ORDERED, never FILTERED).
- ADR 0006 — endpoint-centric scan-all buckets.
- ADR 0014 — neutral DB model (why N+1 cannot live in the core DB layer).
- ADR 0016 — dimension lifecycle (surface folds into scan-all at phase close).
