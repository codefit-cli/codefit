# ADR 0029 — Code↔schema cross infrastructure (neutral QueryFilter + provider QueryExtractor + core cross-runner)

**Status:** Accepted · **Date:** 2026-07-21 · **Phase:** 2 (index-vs-query — RF-03 DB-010/DB-013 groundwork)

## Context

DB-010 (missing index on a filtered column) and DB-013 (missing composite index)
are the next big Phase-2 rules, and they are of a DIFFERENT nature than every DB
rule shipped so far (0.2.x). Those rules reason over the schema alone —
`dbrules.Rule.Check(*db.Schema)`. An index rule cannot: it must see the CODE
("this query filters by `email`") AND the SCHEMA ("is `email` indexed?") at once.
The PRD states this outright — RF-03 line 1161: *"Las reglas de índices requieren
schema + análisis de queries del código."*

A relevamiento against the tree confirmed the cross does not exist anywhere:

- `AuditContext` carries no provider and no schema.
- `dbrules.Rule.Check(*db.Schema)` is schema-only, locked by
  `TestCoreDB_DBRules_NeverImportTypeScriptProvider`.
- In `scan-all`, the security dimension (a `LanguageProvider` over code) and the
  db dimension (a `SchemaParser` over schema) run independently and converge only
  in the JSON.
- The TS provider's query call-site machinery (`isPrismaCall`, `prismaCallInfo`)
  captures the model and method but NOT the WHERE clause — the filtered column is
  not extracted today.
- No `QueryCallSite` / query model exists.

N+1 (ADR 0023) faced the same "code fact about a query" pull and could stay
code-only: N+1 is a pure property of the code (a query lexically inside a loop),
the schema never enters, so it lives entirely in the provider as `surface.Query`.
The index cross CANNOT take that escape: half of it — "does an index exist?" — is
neutral schema. Burying it in the provider would force every future provider to
re-implement schema reasoning, exactly the anti-pattern ADR 0015 exists to kill.

The tension: the cross needs both code and schema, but the core must never import
a provider (ADR 0014/0015).

## Decision

### The cross reasons over TWO neutral inputs, so the core needs no provider

The core already consumes a neutral schema (`db.Schema`) produced by a provider's
`SchemaParser`. We add the symmetric half: a neutral query model produced by a
provider's `QueryExtractor`. With BOTH inputs neutral, a core cross-rule can join
code and schema without a `core → provider` edge. This is Option A of the
relevamiento (neutral model + core runner), wired via Option C (the MCP adapter
assembles the two neutral inputs).

Three pieces:

1. **`internal/core/query` (new leaf)** — `QueryFilter{Model, Columns[],
   Composite, Pos}`. The neutral code side of the cross, mirroring `db.Schema` as
   the schema side. It speaks logical names (model/field), never the physical DB
   name (`@@map`/`@map`), which no query written in code ever references. Depends
   only on `core/db` (for the shared `db.Pos` anchor).

2. **`providers.QueryExtractor` (new capability)** —
   `ExtractQueryFilters(SourceFile) ([]query.QueryFilter, error)`, resolved by
   type-assertion exactly as `SchemaParser` and `CoverageManifest` are (ADR 0014).
   NOT part of `LanguageProvider`; convergence waits for a second real extractor.
   The TS provider implements it in `queryfilters.go`, reusing the N+1 /
   over-fetching call-site machinery (`walkTS`/`isPrismaCall`/`prismaCallInfo`)
   plus a new `prismaWhereColumns` that reads `args[0].where` specifically.

3. **`internal/core/crossrules` (new rule family)** — `Rule.Check(*db.Schema,
   []query.QueryFilter)`, a distinct family from `dbrules.Rule` (two neutral
   arguments, its own `Run`). Ships with an EMPTY `All()` this slice, plus the
   shared `reconcile` helper. Locked by `TestCoreCrossrules_NeverImportProvider`
   (neither `core/query` nor `core/crossrules` may import anything under
   `internal/providers`).

### Reconciliation is the certainty gate — exact match or abstain

`reconcile(schema, filter)` joins a `QueryFilter` to the schema by LOGICAL name:

1. **Table:** exactly one `db.Table` whose `Name == f.Model`, else abstain
   (`ok=false`). The match is exact identity, NEVER fuzzy or case-insensitive. All
   naming-convention knowledge lives in the provider: the Prisma Client accessor
   `user` is normalized to `User` (`modelToSchemaName`, the inverse of Prisma's
   first-letter-lowercasing) BEFORE it reaches the core. If that normalization
   misses, an abstain is the honest outcome — the core never forces a match. The
   `@@map` physical name is irrelevant: both ends of a code query speak `.Name`.

2. **Columns:** each filtered column must name a real `db.Column.Name` of the
   resolved table. Existing ones are kept; a name that is not a column (a relation
   field, an OR/NOT artifact, a typo) is dropped. If none resolve, abstain.

This is the `Complete==false` discipline (ADR 0025) applied to the cross: a
mutilated cross is worse than an absent one.

### Certainty is EARNED, not assumed — determinism vs surface

The PRD (line 270) classes missing indexes as deterministic (certainty 1.0). We
qualify that with the reconciliation gate: a cross-rule (DB-010/013, later slices)
may affirm a **deterministic 1.0 finding ONLY on the exactly-reconciled path**
(single table match + real column + demonstrably no covering index — every half
parsed with certainty, the join exact). On any reconciliation gap it **abstains**
(emits nothing, or at most a surface item) — never a 1.0 affirmation. The
infrastructure does not hardcode the choice: `Rule.Check` returns both findings
AND surface, so each rule keeps the door open; the recommendation for DB-010/013
is deterministic-on-resolved / abstain-otherwise.

### The cross is orchestrated in the adapter, not the schema-only sensor

`scan-all` is the only place that resolves BOTH the code provider and the schema
parser, so it is where the cross belongs (wiring C). `runDBForScanAll` extracts
the neutral filters from the code (walking source files, type-asserting the
provider to `QueryExtractor`) and runs `crossrules.Run(schema, filters)`, merging
the output into the existing `DBSection`. The DB sensor stays schema-only and
provider-blind; it merely also RETURNS the schema it parsed (`Result.Schema`) so
the adapter runs the cross without re-parsing. `scan-db` standalone stays
schema-only (it has no code context). `AuditContext` is unchanged.

**Why the sensor exposes stamping (`StampSurface` + `Result.SchemaContent`).** The
cross output is merged AFTER `dbsensor.Audit` returns, so it never passes through
the sensor's internal fingerprint stamping — and an unstamped item has an empty
fingerprint, which `observedFrom` drops, making a cross rule a silent no-op. The
fix keeps stamping as ONE implementation (no drift): the sensor's fingerprint logic
is factored into an exported `dbsensor.StampSurface(surf, content)`, and it also
exposes the parsed file bytes as `Result.SchemaContent` so the caller can stamp
with the same source-line snippets. The stamping is invoked ONLY from the WIRING
(`internal/mcp`, `runCross`), NEVER from the core: a cross rule emits raw items and
the adapter stamps them. This is why `dbsensor`'s public surface grew, and it does
not invert layering — the core still imports neither providers nor sensors, locked
by `TestCoreCrossrules_NeverImportProviderOrSensor` (extended from providers-only to
also forbid `internal/sensors`).

### Seam landed gated-empty

This slice ships the whole seam — extraction, neutral model, reconciliation,
wiring — with `crossrules.All()` empty, so the output is provably unchanged
(`TestCrossSeam_CollectsFiltersButProducesNothing`: filters are collected, yet the
result is byte-identical, ADR 0020). This proves the costura end-to-end before any
rule is stacked. DB-010 and DB-013 are later slices that append to `All()`.

### `@map` / field-vs-column naming: reconcile matches in LOGICAL space

The cross reconciles in LOGICAL (field-name) space, and that is correct for the
Prisma scope of DB-010, verified against the real parser
(`typescript.TestPrismaMapNamingSpace`): a Prisma `@map("user_email")` puts the
FIELD name `email` in `db.Column.Name` (the physical `user_email` goes to
`db.Column.DBName`), and `@unique`/`@@index` record FIELD names too. So a code
query's `where` field, `Column.Name`, and `Index.Columns` are one consistent
field-name space — `@map` does NOT cause a zero-match (the code query never uses
the physical name; both ends speak `.Name`). `reconcile` never reads `DBName`.

The genuine gap is a CROSS-naming-space cross: a schema expressed in
PHYSICAL-column-name space (a SQL-DDL parse, where `Column.Name` is the DB column
`user_email`) crossed with LOGICAL-field-name code (`email`). There `reconcile`
abstains rather than fuzzy-match across the field↔column boundary
(`crossrules.TestReconcile_PhysicalNameSchemaAbstains`).

Decision: **declare the limit (relevamiento option b), not normalize (option a).**
DB-010's scope is Prisma (the only extractor), which is internally consistent in
field-name space, so no normalization is needed and `reconcile` stays exact. When a
non-Prisma extractor lands (e.g. an ORM over a SQL-DDL schema), the field→column
resolution belongs in THAT provider — it owns both its code parser and its schema
parser, so it emits already-resolved column names — keeping `reconcile` dumb and
exact (option a becomes a per-provider concern, never a core one). For
presentation, a rule can still surface the physical column name via the resolved
table's `Column.DBName`; matching stays logical.

## Declared limits (this slice)

- **OR / NOT** in a WHERE, and **nested relation filters** (`where:{profile:{…}}`),
  are not reduced to indexable columns. OR/NOT are skipped at extraction; relation
  filters are emitted as candidates and dropped at reconcile (only the schema knows
  a relation field is not a column). Both are locked by tests.
- **Cross-naming-space** (logical-field code × physical-column schema, i.e. a
  Prisma-field query against a SQL-DDL schema) abstains by design, locked by
  `TestReconcile_PhysicalNameSchemaAbstains`. Field→column resolution is deferred to
  whatever non-Prisma extractor needs it, in that provider.
- Only the Prisma extractor exists (the TS provider). SQL-DDL / other ORMs
  implement `QueryExtractor` when their slices land.

## Alternatives considered

- **Cross inside the TS provider, emitted as surface (the N+1 escape)** — rejected:
  it works only where one object parses both code and schema (Prisma/TS), not for
  SQL-DDL (a separate `sqlddl` parser, ADR 0018), and it duplicates schema
  reasoning per language — the anti-pattern of ADR 0015.
- **Enrich `AuditContext` to carry provider + schema** — unnecessary: the adapter
  already knows both and assembles the neutral inputs explicitly; no context change
  is needed.
- **Case-insensitive / fuzzy model↔table match in the core** — rejected: it leaks a
  naming convention into the neutral core. The provider owns normalization; the
  core matches exactly and abstains on a miss.

## Consequences

- New leaf `core/query`, new family `core/crossrules` (empty rule set), new
  capability `providers.QueryExtractor` with a TS implementation, and adapter
  wiring — all with the seam proven no-op.
- `dbsensor.Result` gains a `Schema` field (additive; the sensor stays
  schema-only).
- The layering doctrine holds: `TestCoreCrossrules_NeverImportProvider` and the
  untouched `TestCoreDB_DBRules_NeverImportTypeScriptProvider` both stay green.

## Related

- ADR 0014 — neutral schema model and provider-owned parsing (the one-way arrow).
- ADR 0015 — DB rules as core functions over the neutral model (why the cross
  cannot live in a provider).
- ADR 0016 — dimension lifecycle (wire from slice 1).
- ADR 0020 — db is soft in scan-all; empty rule set → byte-identical output.
- ADR 0023 — N+1 stayed code-only; the cross cannot (contrast).
- ADR 0025 — `Complete==false` honesty (the reconciliation abstain floor).
