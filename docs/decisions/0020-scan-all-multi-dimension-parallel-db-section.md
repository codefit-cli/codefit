# ADR 0020 — scan-all runs multiple dimensions; a parallel non-endpoint DB section

**Status:** Accepted · **Date:** 2026-07-05 · **Phase:** 2 (DB dimension — wiring into scan-all, the close/DoD)

## Context

Per the dimension lifecycle (ADR 0016), a dimension is done when scan-all runs it.
scan-all today runs only security and shapes everything as endpoints
(actionable / resolved_clean / frontier over `EndpointReport`). The db dimension is
built — 8 rules over Prisma and SQL-DDL schemas, fingerprinted items, and a unified
multi-sensor baseline (ADR 0019). This wires it into scan-all as db's Definition of
Done.

Two facts shape the design: db findings are NOT endpoints (a table without a
primary key does not hang off a route), and not every project has a database.

## Decision

### scan-all composes the closed dimensions explicitly

`HandleScanAll` runs security (always) and db (only when `database.schema_paths` is
configured). It is an explicit composition of the two closed dimensions, not a
generic sensor loop — generalising the buckets and a registry-driven loop are
deferred. Security remains the required core: if it fails, scan-all fails (as
today). db is independent: any db not-measured / failure is reported in its own
section, never fatal to security.

### A parallel, non-endpoint DB section

The db results are a new `DBSection` on `ScanAllResponse` (a pointer, `omitempty`):
flat `findings` (affirmations) and `surface` (questions), the same shapes scan-db
returns, because they are not endpoints. The three endpoint buckets and the Summary
are untouched. When a project has no `schema_paths`, `DB` is nil and the marshaled
response is byte-for-byte identical to before db was wired (no regression).

### One unified baseline diff, two presentations

The baseline diff is computed once over the union `observedFrom(securityRes,
dbRes)`, scoped to the categories of the sensors that ran (ADR 0019), and saved
once (`diffBaseline`). Then two presentations consume the same diff:
`filterEndpointsByBaseline` for security (unchanged), and `filterDBByBaseline` — a
direct `diff.Shown` filter — for the non-endpoint db items. This is the seam (iii)
declared in ADR 0019, now implemented.

### Honest, independent dimensions — a db error is SOFT here

A db not-measured state (disabled, no parser, schema read or parse failure) is
reported as `DBSection{Measured:false, Note:…}`; it never blanks or invalidates
security. In scan-all a db error is SOFT (reported, not fatal) — UNLIKE the
standalone `codefit-scan-db`, where a configured-but-missing schema is a hard error
— because one dimension's misconfiguration must not blank a multi-dimension audit.

## Consequences

- scan-all is multi-dimension; the response gains a parallel DB section without
  touching the endpoint model or the Summary.
- The unified baseline runs live with both sensors; cross-dimension gone-corruption
  is prevented by the ADR 0019 scope.
- No regression for projects without a database (`DB` omitted).
- **Declared limitation:** scan-all does not distinguish "this project has no
  database" from "the database dimension was not configured" — both yield `DB=nil`.
  That is an orchestration debt (a project genuinely without a DB and one that
  simply lacks `schema_paths` look the same), acceptable now and a future
  improvement.
- `by_dimension` / a global cross-dimension score is still NOT wired
  (`scoring.Compute` stays a dead path) — the next slice.

## Related
- ADR 0016 — dimension lifecycle (scan-all is the close/DoD).
- ADR 0019 — unified multi-sensor baseline (the scope this relies on).
- ADR 0006 / 0008 — the endpoint-centric buckets (untouched here).
