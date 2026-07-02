# ADR 0016 — Dimension lifecycle: a standalone sensor + tool, wired to scan-all at close

**Status:** Accepted · **Date:** 2026-07-02 · **Phase:** cross-cutting (all dimensions)

## Context

codefit audits several dimensions — security, db, review, complexity, tests. How
a dimension is *built* and, crucially, when it is *done* has been implicit: the
security dimension established a shape, the db dimension followed it, but the model
was never written down, so it gets re-explained every session. This ADR records it
as standing doctrine — not a decision about one slice, but the process that governs
every dimension.

Like every ADR in this directory, it is **immutable**: if the model itself changes
(for example, once the first non-endpoint dimension closes and `by_dimension` is
actually turned on, and the wiring teaches us something), a later ADR **supersedes**
this one — it is not edited in place. "Standing doctrine" means it governs until
superseded, not that it mutates.

## Decision

### The lifecycle of an audit dimension

1. **A dimension is a SENSOR + its rule(s)/parser(s)/surface + a permanent
   standalone MCP tool** (e.g. `codefit-scan-security`, `codefit-scan-db`). The
   standalone tool is not temporary scaffolding: the developer or the agent invokes
   it to audit *one* dimension on demand, according to what they are working on at
   that moment. It exists for good.

2. **The dimension is developed standalone** — slice by slice, TDD, dogfood — until
   it is COMPLETE (every rule/parser/surface that belongs to it). During that
   development, `scan-all` is NOT touched.

3. **Its mandatory close step (Definition of Done) is wiring it into `scan-all`.**
   This is how security was closed; it is how every dimension is closed. A dimension
   is not "ready" until `scan-all` runs it.

4. **Therefore every dimension is designed with that final wiring in mind.** From
   the first slice, each rule/surface decision must be compatible with how `scan-all`
   will aggregate and present those findings. The close is a plan, not an
   afterthought.

### What `scan-all` is under this model

`scan-all` runs ALL the dimensions that are FINISHED and wired. That it runs only
security today is CORRECT — security is the only closed dimension. It is not a bug,
nor a name that lies: it is the faithful state of which dimensions are complete.
`scan-all` is the **close destination** of each dimension, not a separate feature
or optional infrastructure. Wiring into `scan-all` *is* finishing the dimension.

### Known technical consequence for non-endpoint dimensions (e.g. DB)

`scan-all`'s current bucketing (`actionable` / `resolved_clean` / `frontier`) is
anchored on HTTP endpoints (`EndpointReport`, `Method`, ADR 0006). Findings that are
NOT endpoints — a DB table without a primary key does not hang off a route — do not
belong in that bucketing. When such a dimension is wired, it gets its OWN
section/bucket in the `scan-all` response; it is never forced into the
endpoint-centric model.

`by_dimension` (the score broken out per dimension: security, db, …) is the
mechanism that is TURNED ON as part of the close wiring, so `scan-all` shows each
dimension's score beside the global. It is defined in the code today but
disconnected (the standalone tools return a plain `Score`); it is switched on when a
dimension is closed, not before.

### The permanent lens (scalability + maintenance)

- All rule logic reasons over the core's neutral model (e.g. `core/db.Schema`) and
  lives in the CORE — written once, inherited by every future provider. The provider
  ONLY parses. No ORM/language-specific rule logic in a provider (ADR 0015).
- If a rule needs an ORM/language-specific fact, that is a SIGNAL the neutral model
  is incomplete → enrich the core once, never smuggle specific logic into the rule
  (ADR 0014).
- Every parser/rule limit or decision is locked as a TEST (a contract), never left
  as an assumption.

## Consequences

- The "why does scan-all only run security?" question is answered once and for all:
  it is the honest state, by design.
- New dimensions have a clear Definition of Done (wired to scan-all) and design
  every slice toward it.
- The scan-all response will grow non-endpoint sections and activate `by_dimension`
  as dimensions close — anticipated here, not discovered later.
- This is the document to read when starting any new dimension or feature, so the
  model and the lens are not re-explained each session. Because ADRs are not loaded
  automatically at session start, the project `CLAUDE.md` carries a short pointer to
  this ADR (and to its foundational pair 0014/0015); that pointer is what guarantees
  the doctrine is actually in context.

## Related

- ADR 0006 — scan-all endpoint synthesis (the endpoint-centric bucketing).
- ADR 0014 — neutral schema model, provider-owned parsing (enrich the core, not the rule).
- ADR 0015 — DB rules as neutral core functions (rule logic in the core, provider only parses).
