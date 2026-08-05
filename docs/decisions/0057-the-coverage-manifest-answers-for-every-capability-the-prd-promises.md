# 0057 — The coverage manifest answers for every capability the PRD promises

**Status:** accepted · **Date:** 2026-08-04 · **Phase:** 3, priority P0-1
(`docs/roadmap.md`) · **Spec:**
[`docs/specs/coverage-manifest-completeness.md`](../specs/coverage-manifest-completeness.md) ·
**Builds on [ADR 0014](0014-neutral-schema-model-and-provider-owned-schema-parsing.md) and
[ADR 0016](0016-dimension-lifecycle-standalone-then-wired-to-scan-all.md)**

## Context

`codefit-coverage` returns the coverage manifest **to the agent**. It is the only thing an
agent reads to learn what codefit can and cannot detect, and everything it does afterwards
— what it compensates for, what it trusts codefit to have handled — rests on it. A gap the
manifest does not declare is a gap the agent will not cover for, because it was never told
the gap exists.

`internal/core/dbcoverage/` had two mechanical controls, both added after two blocking
manifest defects shipped on `main`:

- **Control A** — every rule registered in `dbrules.All()` / `dwrules.All()` /
  `crossrules.All()` has a manifest entry. *Guards: built but undeclared.*
- **Control B** — every rule-ID token the manifest **mentions** is either registered or
  named in `NotCovered()`. *Guards: prose describing a rule that does not exist.*

### Both controls looked outward from the code; neither looked inward from the PRD

That is the finding this ADR records, and it is a shape of blindness, not a missing case.
Control A starts at `All()` and walks to the manifest. Control B starts at the manifest and
walks to `All()`. Both endpoints are **code**. `CLAUDE.md` names
`docs/PRD-codefit-v1.4.md` as the project's source of scope — "ante **cualquier** duda de
scope o diseño, consultarlo **antes** de decidir" — and *nothing* started there.

So a capability the PRD promises that was never built and never declared absent satisfies
both controls simultaneously: Control A never sees it, because it is not registered;
Control B never sees it, because the manifest never mentions it. It is invisible from both
ends, and the more completely it is forgotten, the more thoroughly it passes.

Measured on `main` @ `8657607`, extracting every `DB-###`/`DW-###` token from the PRD:

| | count |
|---|---|
| distinct DB/DW ids the PRD names | 31 |
| registered rules | 23 |
| **promised, not registered, not declared absent** | **7** |

`DB-021`, `DB-022`, `DB-023`, `DB-032`, `DB-101`, `DB-102`, `DB-201`. It is the only debt
in codefit's whole inventory that was not self-declared somewhere.

## Decision

### 1. A promised rule id must be answerable, and there are exactly three honest answers

1. **It is registered** — a real rule. Control A already forces it to have a
   `Deterministic()`/`Reasoning()` entry.
2. **It is named in `NotCovered()`** — declared absent, with its reason. This bucket means
   *not covered today*, never *never*: `DW-020` and `DW-021` both sat here and left when
   they shipped.
3. **It is delivered under another identifier** — the capability exists, but not as a rule
   carrying that id.

**Silence is not a fourth answer.** An id the manifest never mentions in any of the three
ways is a hole the agent cannot see.

### 2. The answer set has three members rather than two, and `DB-201` is why

Two buckets would have forced a dishonest answer. The PRD's RF-04 names `DB-201` for N+1
detection, and **N+1 ships** — since `v0.2.2`, as the `nplus1` surface category
(`codefit-surface-nplus1`, `internal/providers/typescript/nplus1.go`). It is a *provider
surface category*, discovered from the application's code, not a schema rule with an id;
`internal/core/dbrules/layering_test.go` already locks that `dbrules.All()` never gains an
N+1 entry, because a `dbrules.Rule` is handed a `*db.Schema` and never sees code.

With only two buckets the choice was:

- put `DB-201` in `NotCovered()` — **a lie**, the capability ships; or
- leave it out — **silence**, the failure this whole change closes.

So `dbcoverage` gains `DeliveredElsewhere()`, whose meaning is *this promised identifier is
delivered by this other thing*. Each entry carries **both names** — the promised id and
what actually delivers it — plus enough prose to follow the mapping. `coverage.Manifest`
gains the matching `DeliveredElsewhere` field, the TypeScript manifest composes it by
`append` exactly as it composes the other three, and `codefit-coverage` serves it: a
manifest entry no tool surfaces is a comment, not a capability.

### 3. Control B moves with the bucket, in the same change

`DB-201` is **covered**, so the DB manifest names it in `Reasoning()` — as a pointer, not
as a claim that this dimension detects it. But `Reasoning()` is exactly what Control B
scans, and Control B failed any mentioned token that was neither registered nor in
`NotCovered()`. Naming `DB-201` there without amending Control B makes the control report
a phantom capability *for a capability that ships*.

The bucket and the amendment are one change. The bucket alone leaves the tree red; the
amendment alone widens Control B's hole. What did **not** change: a token in no bucket at
all is still a phantom and still fails, and that line is held by its own test whose
mutation is "widen the predicate to accept anything".

### 4. The new control derives the promised set mechanically, from the PRD

The enforcement test reads `docs/PRD-codefit-v1.4.md` and extracts every rule-id token with
the **same regexp Control B already uses**. There is deliberately no hand-maintained list
of "ids the PRD promises".

This is not a stylistic preference. A second hand-maintained list is the same failure mode
one level down: it would drift from the PRD exactly the way the manifest drifted, and it
would pass its own test while doing so. Control A holds to this standard already
(`registeredIDs()` derives from `All()`, never from a literal).

**Consequence, chosen rather than discovered: editing the PRD can fail this test.** That is
intended. The PRD is the declared source of scope, so adding a rule id to it is a promise,
and a promise with no manifest answer is the defect.

### 5. The control guards correspondence, never accuracy

Control C in this package is deliberately unimplemented — documented rather than faked,
because a rule↔manifest correspondence test is *impossible* for `internal/core/paradigm/`,
which registers no rules. That discipline governs the new control too. It guards that an
answer **exists**, never that the answer is **true**: a `NotCovered()` entry that
misdescribes why a rule is absent, or a `DeliveredElsewhere()` entry naming the wrong
deliverer, passes it. The limit is stated in the test's own comment, exactly as Control A
states its own.

## Consequences

- **The seven are answered.** `DB-201` in `DeliveredElsewhere()`; `DB-021`, `DB-022`,
  `DB-023`, `DB-032`, `DB-101`, `DB-102` in `NotCovered()`, each stating what it would
  detect and why it is not covered yet. `DB-101`/`DB-102` are recorded as **surface
  candidates, never affirmations**, because the PRD itself promises them *"vía razonamiento
  del agente"* and a functional dependency is a fact about data and domain that no schema
  text establishes — recorded so a future implementer does not build them as deterministic
  rules.
- **No rule was built.** This change makes the manifest honest about the seven; it
  implements none of them. Their entries are declarations, not capabilities.
- **`DW-022` is untouched.** `DB-022` is declared against today's truth — not covered,
  refresh cadence lives in scheduler state static DDL does not carry — and points at
  `docs/roadmap.md` P4-3, where reframing it as *surface* is decided but not built. That
  reversal owes its own ADR and a `db.View` parser floor (the type has no way to say a view
  is materialized) and is deliberately not done here.
- **The agent-facing tool description changed.** `internal/mcp/server.go` is the only thing
  an agent reads before choosing a tool; `codefit-coverage` now names the fourth bucket and
  tells the agent to read it before concluding a rule id is not covered.
- **A correction to the spec, recorded here because a spec is a design contract and is not
  rewritten to match what shipped.** The spec states that "nothing in the manifest ever
  spells" `DB-201`. That is true of `dbcoverage` — the DB/DW namespace owner, and the only
  thing Controls A/B/D read — but **not** of the *composed* manifest an agent receives:
  `internal/providers/typescript/coverage.go` has named `DB-201` in its N+1 reasoning entry
  since `v0.2.2`, as do `COVERAGE.md` and the README. The defect is narrower than the spec
  says: an agent asking the **DB dimension** for `DB-201` finds nothing, and the DB
  dimension is where a `DB-` id is looked up. The fix is unchanged, and so is the reason
  for the third bucket.
- **The three sibling manifests have no such control.** `SEC-###` and `PRAC-###` live in
  separate sources whose PRD-inward asymmetry is very likely identical. Recorded in
  `docs/roadmap.md`, not fixed here.
