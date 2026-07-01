# ADR 0014 — A neutral schema model in the core, provider-owned schema parsing

**Status:** Accepted · **Date:** 2026-06-30 · **Phase:** 2 (DB sensor — schema foundation)

## Context

Phase 2 introduces the DB sensor (RF-03 OLTP structure, RF-04 N+1). Before any
rule, sensor, or `codefit-scan-db` tool can exist, codefit needs to *understand
the database's structure* the way it already understands code: as a neutral model
the core reasons over, filled by a language/format-specific parser.

The first source of that structure is a Prisma `schema.prisma`. Two facts shape
the design:

1. `schema.prisma` is **not** TypeScript. The TS provider's parser
   (`gotreesitter`, ADR 0002) parses the `.ts`/`.tsx` grammar, not the Prisma
   DSL. A schema parser is a new, separate thing.
2. The relevamiento flagged that the existing Prisma *access* detection (the
   surface layer in `internal/providers/typescript/idor.go`,
   `overfetch.go`) returns bare strings like `"db.user.findUnique"` **without a
   line number**, and that this missing position is a real gap — a finding cannot
   anchor, and the baseline fingerprints by `file`/content. The schema model must
   not repeat that mistake.

Three questions had to be answered, mirroring the questions ADR 0001 and ADR 0003
answered for the AST:

1. Where does the parsed schema live so the **core never depends on a concrete
   format** (Prisma, SQL-DDL)?
2. How does a provider expose schema parsing **without bloating the shared
   `LanguageProvider` interface** (which a second parser has not yet validated),
   and **without inverting the core↔provider dependency**?
3. Should the model express only what Prisma can say, or the whole OLTP surface
   Phase 2 audits?

## Decision

### A neutral, pure schema model in the core: `internal/core/db`

Introduce `db.Schema` (and `Table`, `Column`, `Index`, `ForeignKey`, plus
`View`/`Procedure`/`Trigger`, the `Type` enum and `Pos`), a format-agnostic
structural model that lives in the core. The core (the future DB sensor and its
rules) reasons over a database **only** through `db.Schema`; it never imports a
Prisma parser or knows the source was a `.prisma` file. This is exactly the
relationship `core/findings.Finding` already has with the providers: a neutral
type in the core, **filled** by each provider — the core is blind to provenance.

`core/db` is a **pure data-model leaf**: structs, the `Type` constants, nothing
else. It defines **no interface** and imports **nothing** from `internal/providers`
(the same leaf discipline `core/findings` keeps). See the dependency invariant
below.

Every element carries an origin **`Pos{File, Line}`**. A DB finding ("table
without a primary key", DB-050) and the baseline both anchor by file + line /
content; an element with no position could not be reported or fingerprinted. This
is the deliberate non-repeat of the access-layer gap (relevamiento §E): position
is part of the model from line one, not bolted on later.

### Schema parsing is a provider capability — the interface lives in `providers`, returns the core type

A provider that can parse a schema satisfies a **small, optional** interface
defined **next to `LanguageProvider`, in `internal/providers`** — not in the core:

```go
// in internal/providers
type SchemaParser interface {
	ParseSchema(sources []SourceFile) (*db.Schema, error)
}
```

This mirrors the contract that already exists in the repo:
`LanguageProvider.AnalyzeSurface()` returns `findings.SurfaceItem` — the
**interface** lives in `providers`, the **return type** lives in `core/findings`,
and `core/findings` does not import `providers`. `SchemaParser` is the same shape:
interface in `providers`, return type (`db.Schema`) in the core; `core/db` imports
nothing from `providers`. Putting the interface in the core would force `core/db`
to import `internal/providers` for `SourceFile` and **invert the dependency this
slice exists to establish.**

`ParseSchema` is **not** added to `LanguageProvider`. The caller (the future DB
sensor / MCP adapter) resolves it by type-assertion — `provider.(providers.SchemaParser)`
— exactly as `codefit-coverage` resolves `CoverageManifest` today
(`internal/mcp/scan.go`, the `interface{ CoverageManifest() coverage.Manifest }`
assertion). This is the deferred-convergence stance of ADR 0003: a shared
abstraction is frozen into `LanguageProvider` only once **two real parsers**
exercise it. Today only the TS provider would implement `SchemaParser` (for
Prisma); the SQL-DDL parser (slice 3) is the second implementer that will tell us
whether the interface holds. Adding it to `LanguageProvider` now would force the
Go provider to implement a method it has no use for and freeze the shape against a
single caller.

### The dependency invariant

> `internal/providers → internal/core/db` is allowed; `internal/core/db →
> internal/providers` is **forbidden**. The core defines the neutral data; the
> provider layer depends on the core, never the reverse.

`core/db` is a leaf with no codefit imports (like `core/findings`). The
`SchemaParser` interface lives in `providers` precisely to keep that arrow
one-directional.

### The provider is filesystem-free; the caller reads the files

`ParseSchema` receives the **already-read** `[]providers.SourceFile` (content in
hand), never a path. Resolving `cfg.Database.SchemaPaths` against the disk is the
caller's job (the future sensor), exactly as `AnalyzeSecurity` receives a
`SourceFile` and never opens a file. The provider stays pure and trivially
testable: bytes in, `*db.Schema` out. (`SchemaPaths` is already populated by
`codefit init`; this slice does not touch that path — it only defines the consumer
shape.)

### The model defines the whole OLTP surface, even what Prisma cannot express

`db.Schema` includes `View`, `Procedure`, and `Trigger` **now**, even though the
Prisma parser of this slice leaves them empty. The rules they serve (DB-020..023
views, DB-030..032 procedures, DB-040..041 triggers) are populated by the SQL-DDL
parser in slice 3. Defining them now makes the model **format-agnostic, not just
language-agnostic**: a given parser fills the subset its format can express
(Prisma fills tables/columns/indexes/relations; SQL-DDL also fills
views/procs/triggers), and the model is the union of the whole OLTP surface — not
the intersection of what one format happens to say.

The governing rule, stated once:

> **The data model of a schema lives in the core; the parsing lives in the
> provider. The model defines the entire OLTP surface Phase 2 audits, even when a
> given parser populates only the subset its format expresses.**

A model shaped to Prisma's expressiveness would force a refactor of the core the
moment SQL-DDL arrives — the same "designed against one source" mistake ADR 0001
avoided for `go/ast` and ADR 0003 for `syntax.Node`. OLAP/Data-Warehouse is **out
of Phase 2** (it will be declared `NotCovered`); nothing OLAP is modeled.

### A hand-written Prisma DSL parser, not a tree-sitter grammar

The Prisma parser is a small, hand-written, line/block scanner over the Prisma
DSL — **not** a tree-sitter-prisma grammar. Reasons, strongest last:

1. **No grammar to adopt.** `gotreesitter` bundles the TS/TSX grammars; it has no
   Prisma grammar. Adopting one means porting/vendoring a new grammar and
   carrying the risk against the **non-negotiable `CGO_ENABLED=0`** constraint —
   a large, uncertain dependency for a DSL we only need a subset of.
2. **Native positions.** A line-oriented scanner yields `Pos.Line` for free,
   which the model requires for every element.
3. **The DSL is small and regular.** Block headers (`model`, `datasource`,
   `generator`, `enum`, `type`), field lines (`name Type modifiers @attrs`), and
   block attributes (`@@id`, `@@unique`, `@@index`) are a closed, stable set. A
   focused parser is more maintainable here than a general grammar, and fully
   under our control and tests.

Prisma `view` blocks are **out of scope this slice** and are NOT a recognized
header: the view rules (DB-020..023) target SQL views, which arrive with the
SQL-DDL parser (slice 3). A `view` block is skipped without error and leaves
`Schema.Views` empty — a contract locked by a test, so the skip is intentional,
not accidental. (`Schema.Views` is only ever populated by the SQL-DDL parser.)

The parser is **two-pass** by necessity, not by preference: a field's type can be
a scalar (`Int`), a **model** name (a virtual relation field — excluded as a
column), or an **enum** name (a real column, `Type=TypeEnum`). The three are
indistinguishable from a single field line. Pass one collects the set of `model`
and `enum` names; pass two resolves each field's type against those sets. Without
this, a "non-scalar type ⇒ relation ⇒ not a column" shortcut would silently drop
enum columns (`role Role`). The fixture carries an enum specifically to fail any
parser that takes that shortcut.

This choice is **scoped to Prisma only**. SQL-DDL (slice 3) is a genuinely
complex language where a real parser will likely be warranted; that is a separate
decision, made with its own evidence. The asymmetry is deliberate, not an
oversight.

## Consequences

- The core gains `internal/core/db`, a neutral, position-carrying, **pure**
  schema model — the foundation every Phase 2 rule reasons over, blind to source
  format, importing nothing from the provider layer.
- The `providers.SchemaParser` interface (in `internal/providers`) is satisfied by
  the TS provider's new `ParseSchema` (Prisma), discovered by type-assertion.
  `LanguageProvider` is untouched; the Go provider is untouched (self-audit stays
  green). Interface convergence into `LanguageProvider` waits for the SQL-DDL
  parser to be the second implementer (ADR 0003 stance).
- The dependency arrow stays one-directional: `providers → core/db`. A future
  import of `providers` from `core/db` is a design regression to reject in review.
- `View`/`Procedure`/`Trigger` exist in the model but are empty after a Prisma
  parse — a documented, intentional transient until slice 3, not a gap.
- The parser is two-pass to resolve model-vs-enum-vs-scalar field types; the enum
  case is locked by a fixture so the shortcut that would drop enum columns cannot
  pass the tests.
- Table/column DB-name remapping (`@@map`/`@map`) is modeled as `DBName` (empty =
  no remap; not defaulted to `Name`, so a rule can tell an explicit remap from
  none). FKs and indexes reference by model name, not `DBName`.
- Known Prisma limits owned from the start: implicit many-to-many relations (no
  explicit FK columns; Prisma synthesizes a join table) are **not** modeled as
  foreign keys this slice; only explicit `@relation(fields:…, references:…)`
  relations are. Declared, not silent.
- This slice ships **no rule, no sensor, no tool, no `scan-all` change** — only the
  model, the parser, and this record. `codefit-scan-db` stays an unregistered
  reserved name (relevamiento §F) until the sensor slice.
