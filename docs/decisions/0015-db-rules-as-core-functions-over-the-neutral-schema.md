# ADR 0015 — DB rules as Go functions over the neutral schema, in the core

**Status:** Accepted · **Date:** 2026-07-01 · **Phase:** 2 (DB sensor — first schema-only rules)

## Context

Slice 1 (ADR 0014) gave codefit a neutral schema model (`internal/core/db`) and a
provider-owned Prisma parser. This slice adds the first DB rules (DB-050, DB-001,
DB-011, DB-002), the DB sensor, and the standalone `codefit-scan-db` tool.

The security dimension already has a rule mechanism: YAML files in a Semgrep
subset (`rules/typescript/security/*.yaml`), compiled and matched against the
**text/AST of one source file** by `core/ruleengine`. The natural question is
whether DB rules reuse it.

They cannot, and the reason is structural: a security rule matches a *syntactic
pattern in one file*; a DB rule answers a *relational question over the whole
schema graph*. "A foreign key with no covering index" cross-references a table's
`ForeignKeys` against its `Indexes` and `PrimaryKey` — set logic over structured
data, not a text pattern. No Semgrep pattern can express it. `core/db.Schema` is
already the parsed, neutral form; the rule reasons over that, not over text.

The second force is scalability. codefit must scale to new ORMs (JPA, Drizzle, EF
Core) at bounded cost. The design test applied to every piece: does it live in the
**core** (written once, inherited by every future provider) or in the **provider**
(rewritten per ORM)? A DB rule that reasons over the neutral schema is written
once and inherited for free by any provider that can produce a `db.Schema`.

## Decision

### DB rules are Go functions over `db.Schema`, in the core — not YAML, not the provider

A DB rule is a small Go value in a new core package `internal/core/dbrules`:

```go
// internal/core/dbrules
type Rule interface {
	ID() string // "DB-050"
	// Check reasons over the neutral schema and emits deterministic Findings
	// (affirmations, Confidence 1.0) and/or SurfaceItems (questions the agent
	// reasons). It never sees Prisma — only db.Schema.
	Check(*db.Schema) ([]findings.Finding, []findings.SurfaceItem)
}

func All() []Rule { /* db050{}, db001{}, db011{}, db002{} */ }

// Run executes every rule and returns the union (findings, surface).
func Run(*db.Schema) ([]findings.Finding, []findings.SurfaceItem)
```

Registration is a hand-written `All()` slice — the same "instantiated by hand, no
registry" convention the sensors use. No reflection, no plugin registry (YAGNI
until there are dozens of rules).

**`dbrules` lives beside `core/db`, not inside it.** `core/db` is a pure leaf that
imports nothing (ADR 0014); a rule imports `core/findings` (and `core/surface`),
so it must be a *separate* core package that depends on both `core/db` and
`core/findings`. Putting rules inside `core/db` would break its leaf invariant.
The dependency arrow stays: `dbrules → {core/db, core/findings, core/surface}`;
`core/db` still imports nothing.

### The provider only parses; every rule is inherited

No DB rule may read anything Prisma-specific. If a rule ever needs a fact the
neutral schema does not carry, that is a signal that **`core/db` is incomplete** —
the fix is to enrich the neutral model once (in the core), never to put ORM logic
in the rule or the provider. This is what makes a rule written today inherited for
free by JPA/Drizzle the day they have a parser. (Slice 1's `DBName` was exactly
this move made ahead of need.)

### Affirmation vs surface — the same boundary as ADR 0004, on a new substrate

ADR 0004 split *deterministic rule* (conclusive over a local subtree) from *mapped
surface* (needs judgment / following the data). The same epistemology governs DB
rules, now over the schema instead of the AST:

- **Affirmation** (`Finding`, `Confidence 1.0`, `Probabilistic false`): the defect
  is structurally undeniable. **DB-050** (a table with no primary key) is a fact
  the schema states outright.
- **Surface** (`SurfaceItem` with `StructuralFacts` + `ReasonToReview`): the
  structure is a fact but whether it is a *problem* needs judgment codefit will not
  fake. **DB-001** (an un-indexed FK — matters only for this table's size/access
  pattern), **DB-011** (a duplicate index — may be an in-flight migration),
  **DB-002** (a multivalued column — a Postgres array is legitimate sometimes). A
  false green is worse than an honest red (ADR 0005); codefit maps the fact and the
  agent judges.

The split is per-rule and locked by tests, so "DB-001 is a question, DB-050 is an
assertion" is contract, not convention.

### DB-001 covering-index semantics (the slice-1 deferred consideration, now due)

A FK is **covered** when some index-like column list has the FK's columns as a
**leading prefix** (same order) — the way a B-tree actually accelerates the FK. The
index-like set of a table is: every `Index` (unique or not) **plus the
`PrimaryKey` treated as an implicit index** and every `@unique` (already an
`Index`). So a FK that is also the PK, or is a leading prefix of the PK or of any
index, is covered; a FK that is only a *non-leading* member of a composite index is
**not** covered (a composite `[a,b]` does not serve a lookup on `b` alone). Prefix,
not exact, because that matches real index usage.

## Consequences

- New core package `internal/core/dbrules` holds all DB rules; `core/db` stays a
  pure leaf. Every rule is language-neutral — inherited by any future ORM provider
  that emits a `db.Schema`, at zero rule-authoring cost.
- Two rule mechanisms coexist by design: YAML/Semgrep for text-pattern security
  rules, Go functions for structural DB rules. They are not unified because they
  answer different kinds of question (syntactic pattern vs relational query).
- A rule needing an ORM-specific fact is a defect signal: enrich `core/db`, never
  the rule. Documented so a contributor does not smuggle Prisma into a rule.
- The coverage manifest is per-language today (`providers/<lang>/coverage.go`) but
  DB rules are neutral — the same DB coverage prose would be duplicated per
  provider. This friction is noted here and resolved when a second provider lands
  (a neutral DB-coverage source), NOT in this slice.
- Scope held: schema-only rules (DB-050/001/011/002). Query-driven rules
  (DB-010/012/013, N+1), name-heuristic rules (DB-051/052/053/003), SQL-DDL,
  `scan-all` integration and `by_dimension` scoring are out of this slice.
