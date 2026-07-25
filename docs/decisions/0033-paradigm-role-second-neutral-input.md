# ADR 0033 — Paradigm/table-role as a core-computed second neutral input

**Status:** Accepted · **Date:** 2026-07-25 · **Phase:** 2 (RF-03 OLAP closure, S1)

## Context

RF-03 closes codefit's OLAP debt: paradigm detection, 3NF-suppression on
OLAP-classified schemas, and (in later slices of this same change) the
star/snowflake, SCD, columnar-index, and partitioning rule family (DW-0xx).
Every one of those rules needs something beyond the bare `db.Schema` — a
per-table classification of whether it is a fact table, a dimension, or
ordinary OLTP structure — before it can reason at all.

`internal/core/crossrules` (ADR 0029) already solved the identical shape of
problem once: DB-010/DB-013 need a second neutral input (`query.QueryFilter`,
extracted from application code) beyond the schema. That precedent — a
SEPARATE two-argument `Rule` family, assembled at the layer that legitimately
holds both inputs — is the template this ADR extends, with one deliberate
divergence in WHERE assembly happens, driven by where the paradigm's second
input actually originates.

## Decision

### 1. Classification is a pure function of `db.Schema`, in a new core leaf

`internal/core/paradigm/` computes `Classification{ Paradigm, Roles }` from
`*db.Schema` alone. It imports ONLY `internal/core/db` — never a provider,
never `internal/config`, never a sensor — the same ADR-0015 schema-only
simplicity `internal/core/dbrules` already locks, now extended to this
second neutral input. A layering contract test
(`TestCoreParadigm_NeverImportProviderOrSensor`, cloned from
`crossrules/layering_test.go`) makes this mechanical, not aspirational.

### 2. Detection SEEDS; an explicit config override RESOLVES

`paradigm.Detect(s)` uses table-name prefixes (`fact_`/`dim_`/`stg_`/`mart_`)
as the PRIMARY signal — name-only was rejected as too noisy (locked decision
A5) — corroborated by structure: a single-column primary key reads as a
surrogate key; a `fact_`-prefixed table additionally needs foreign-key
fan-out to 2+ distinct tables. A table with no recognized prefix is always
`unclassified`, regardless of structure — structure corroborates a name
candidate, it never substitutes for one.

`paradigm.Resolve(detected, override)` applies `database.paradigm` on top.
`database.paradigm` gains `"auto"` as its documented default (resolving the
PRD-vs-code mismatch flagged during exploration): `auto`/empty ⇒ detection
decides; an explicit `oltp`/`olap`/`mixed` value ALWAYS wins over detection.
This is the project's developer-autonomy principle applied to a new signal:
codefit never overrides what the developer explicitly declared. Resolve
replaces `Classification.Paradigm` only — `Roles` stays detection-derived
even under an override, so a per-table consumer (the 3NF-suppression pass,
and every future DW rule) can still reason about individual table shapes
under an explicit `mixed` override.

### 3. A SEPARATE two-arg Rule family (`dwrules`), not a mutation of `dbrules.Rule`

`internal/core/dwrules/` mirrors `crossrules.go` exactly:
`Rule.Check(*db.Schema, *paradigm.Classification) (...)`, `All()`, `Run`,
`RunWith`, `OwnedCategories()`. `dbrules.Rule` (`Check(*db.Schema)`) is
UNTOUCHED — no signature change, no new parameter threaded through every
existing schema-only rule. `dwrules.All()` is empty in S1: this is the inert
skeleton later slices populate (DW-001/002/005/010/011 in S2, DW-021 in S3,
DW-020 in S4) without ever touching the seam, proven by a seam-gate test
(`RunWith` over an empty rule set returns `(nil, nil)`) identical in spirit
to `crossrules.RunWith`'s own precedent.

### 4. Assembly happens in the SENSOR, not the MCP adapter — the deliberate divergence from ADR 0029

The cross's second input (`query.QueryFilter`) is extracted from application
CODE by a language provider, so ADR 0029 correctly assembles it in
`internal/mcp` — the only layer that knows both the provider and the parsed
schema. The paradigm's second input is different in kind: it is (a) a pure
function of `db.Schema` — self-derivable entirely in core, zero provider
involvement — plus (b) a config string (`database.paradigm`) the DB SENSOR
already holds via `ctx.Config`. There is no provider crossing to broker, so
routing assembly up to the MCP adapter would add a layer for no reason.
`internal/sensors/db/db.go`'s `Sensor.Audit` — which already holds both
`schema` and `ctx.Config` — is the correct, simpler assembly point:

```go
override := toParadigmEnum(ctx.Config.Database.Paradigm)
cls := paradigm.Resolve(paradigm.Detect(schema), override)
dwF, dwS := dwrules.RunWith(schema, &cls, dwrules.All())
```

`toParadigmEnum` is sensor-local (an identity string cast — config
validation already restricts the value to `auto`/`oltp`/`olap`/`mixed`/`""`)
and is the ONE place config meets core, keeping `paradigm`/`dwrules`
config-free. Because DW rules run INSIDE the sensor, their surface
categories are the sensor's own `OwnedCategories()` — no separate union step
in `scanall.go` the way crossrules needs (crossrules runs OUTSIDE the
sensor, so it must be unioned in by the adapter). S1 ships `OwnedCategories`
unchanged (dwrules has none yet); this is verified as a locked regression,
not assumed.

### 5. 3NF-suppression as a sensor-level, paradigm-aware pass — DB-002/DB-003 stay schema-only

DB-002 (multivalued column) and DB-003 (repeating groups) are existing,
deterministic 1NF surface rules in `dbrules`. Rather than mutate their
signature to accept a classification, the SENSOR applies a suppression pass
over the already-merged surface, keyed on a `"table: <name>"`
`StructuralSignal` now added to both rules' items (the same cheap, honest
pattern DB-052 already used). The pass resolves the table, looks up its role
in `cls.Roles`, and drops the item when the table's role is
fact/dimension/mart — UNDER auto detection or an explicit `mixed` override —
or drops every DB-002/DB-003 item schema-wide under an explicit `olap`
override (the developer is asserting the whole schema is a warehouse, even
for a table structural detection could not otherwise classify). An explicit
`oltp` override NEVER suppresses, regardless of what `cls.Roles` says —
`Roles` stays detection-derived even under an override (decision 2, above),
so the suppression pass gates on the RAW override value, not
`cls.Paradigm`, to honor the spec's explicit requirement that an explicit
`oltp` config value defeats detection outright.

## Consequences

- `internal/core/paradigm/` and `internal/core/dwrules/` are new leaves;
  neither is reachable from a provider or a sensor by construction, locked
  by cloned layering contract tests.
- `internal/core/dbrules/rules.go` (DB-002) and `rules_names.go` (DB-003)
  each gain one `StructuralSignal` line; their `Rule.Check(*db.Schema)`
  signature is UNCHANGED.
- `internal/sensors/db/db.go` gains the assembly + suppression pass;
  `internal/sensors/db/db.go`'s `OwnedCategories()` is unchanged this slice
  (verified, not assumed) since `dwrules.OwnedCategories()` is empty.
- `internal/config` gains `"auto"` as a valid `database.paradigm` value (was
  previously rejected); `internal/scaffold` seeds `"auto"` instead of
  hardcoding `"oltp"` on every Prisma-detected project.
- Zero effect on the deterministic `DimensionDB` score: DW rules and the
  suppression pass only touch SURFACE (ADR 0017), never a Finding.

## Rejected alternatives

- **Mutating `dbrules.Rule` to accept the classification.** Would force
  every existing schema-only rule (DB-050, DB-001, DB-011, DB-051..053,
  DB-020, DB-030/031/040/041) to change signature for a fact only 2 of the 8
  currently need — breaks the simplicity ADR 0015 deliberately protects for
  the schema-only family.
- **Re-homing DB-002/DB-003 into `dwrules`.** Needless churn: it would split
  1NF-checking ownership across two packages for rules that are correctly
  schema-only and pre-date this change; the suppression concern belongs at
  the sensor, where the paradigm fact is legitimately known, not in the rule
  that produces the raw structural signal.
- **Assembling in `internal/mcp`, mirroring crossrules exactly.** Rejected
  because the paradigm's second input has no provider crossing to broker —
  routing it through the adapter would add indirection with no corresponding
  layering benefit (decision 4, above).

## Related

- ADR 0014 — the neutral `db.Schema` model this classification reasons over.
- ADR 0015 — rule logic lives in core, over the neutral model; schema-only
  simplicity this ADR extends rather than breaks.
- ADR 0029 — the cross's second-neutral-input precedent (`crossrules`), the
  template this ADR follows with one assembly-point divergence.
- ADR 0032 — the most recent `core/db`/cross correction; format precedent for
  this document.
