# ADR 0028 — Coverage honesty: the "detectable-without-dogfood" state and the fixture gap policy

**Status:** Accepted · **Date:** 2026-07-20 · **Phase:** 2.3 (routine-body rules)

## Context

The routine-body rule family (DB-030 dynamic SQL, DB-031 exception handling,
DB-040 trigger cross-table cascade, DB-041 trigger external call) shipped four
rules that read a routine's captured body over three SQL dialects (PostgreSQL,
MySQL, T-SQL). Standing them up forced two questions the earlier dimension work
had not, and the answers are doctrine the next dimension will meet again:

1. **How do you honestly represent a rule that CAN detect an antipattern, on a
   dialect where that antipattern is real but structurally rare?** DB-041 (a
   trigger making an external-effecting call) is the case: the scanner recognizes
   MySQL's external vocabulary (`sys_exec`/`sys_eval`), but a MySQL trigger that
   reaches outside the database is non-idiomatic (MySQL has no `NOTIFY`, external
   effects need exotic UDFs). Calling it "covered" overstates the dogfood
   evidence; calling it "not covered" is a lie (the rule would fire).

2. **When the real reference schemas are CLEAN of an antipattern, how do you
   prove the positive fire path without faking evidence?** The vendored corpora
   (Sakila, Pagila, AdventureWorks) are well-designed — most of the routine-body
   antipatterns simply are not present. DB-020 and DB-011b already hit this in
   0.2.2 and constructed their positives; 0.2.3 needed a uniform policy across
   four rules and three dialects.

Both are corollaries of the honesty doctrine: ADR 0004's warning against
**mutilated rules that give false confidence**, and ADR 0005's **honest red over
a false green**. This ADR makes them operational for the coverage manifest and
for fixtures. The four rules themselves get NO individual ADR — each is a
straightforward application of the bounded-surface-scanner mold established by
ADR 0017 / DB-020 and DB-031 (a string/comment-aware token scanner over
`Body.Text`, gated on `Body.Complete`, surface never affirmation). Only the
doctrine below is new.

## Decision

### 1. A THIRD coverage state: "detectable, without dogfood"

The coverage manifest recognizes three states per (rule × dialect), not two:

| State | Meaning |
|---|---|
| **Covered** | The rule detects the antipattern AND a real or constructed case dogfoods both the positive and the negative fire path. |
| **Detectable, without dogfood** | The rule DETECTS the antipattern (the scanner would fire), but no case is dogfooded because the antipattern is **structurally rare / non-idiomatic** on that dialect. The capability exists; only the evidence is absent, by the dialect's nature. |
| **Not covered** | The rule **cannot** detect the antipattern — a structural impossibility, not a missing fixture. (DB-012 never-used-index is the archetype: it needs runtime query telemetry a static, DB-less model can never read — ADR-level permanent, see the DB-012 coverage note.) |

"Detectable-without-dogfood" is distinct from "not covered" and must be stated as
its own state, plainly — never collapsed into either neighbor. Collapsing it into
"covered" fakes evidence; collapsing it into "not covered" hides a real
capability. The motivating case is **DB-041 on MySQL**. A future dimension whose
detection outruns the idiomatic reality of a language/dialect reuses this state
rather than distorting the manifest.

### 2. The fixture gap policy (positive/negative dogfood)

For each (rule × dialect) cell, evidence is sourced in this fixed order:

1. **Real, if it exists.** Vendor the real upstream object (verbatim, byte-diffed,
   license-attributed) when the reference corpus genuinely contains the case.
2. **Constructed-and-declared-synthetic, when the corpus is clean.** When the
   antipattern is realistic on the dialect but the reference schema simply does
   not contain it, hand-write a minimal fixture — with a header that **declares
   its synthetic origin and why**, never implied to be upstream. This is the
   0.2.2 precedent (DB-020's positive, DB-011b's positive, DB-031's PG negative)
   made a uniform rule.
3. **Not covered, ONLY for structural impossibility.** A clean corpus is NOT a
   reason to declare "not covered" — the rule detects the case, the schema just
   lacks it (which is itself an honest finding about how real schemas are
   written). "Not covered" is reserved for cases the rule cannot detect at all.

A precise rule is what makes this policy safe: DB-041's **strict vocabulary** —
an external-effecting call *leaves the database* (shell/OLE/email/remote/notify);
a plain `EXECUTE`/`CALL` of an internal stored procedure does not — is why its
constructed positive is a genuine antipattern and its real trap
(`uPurchaseOrderDetail`, which only calls internal logging procs) is a genuine
negative. A loose rule would blur that line and make both the constructed
positive and the real negative meaningless.

## Alternatives considered

- **Two states only (covered / not covered).** Rejected: it forces DB-041/MySQL
  into a lie either way. The manifest's whole value is that a blind spot is
  *declared and known* (PRD §10); a two-valued manifest cannot express "detects
  it, but no idiomatic case to show".
- **Construct the missing MySQL DB-041 positive anyway.** Rejected: a synthetic
  MySQL trigger doing something no real MySQL trigger does would be *unrealistic
  evidence* — the same false confidence ADR 0004 warns against, one level down.
  Honest is "detectable, structurally rare", not a staged fixture.
- **Declare a clean-corpus cell "not covered".** Rejected: it hides a working
  rule and mislabels an honest property of real schemas (they lack the
  antipattern) as a codefit gap.

## Consequences

- The coverage source (`internal/core/dbcoverage/dbcoverage.go`) and its mirror
  (`COVERAGE.md`) carry per-dialect matrices that use all three states, and mark
  every constructed fixture as synthetic.
- Constructed fixtures live beside the real ones under
  `internal/providers/sqlddl/testdata/`, each with a `CONSTRUCTED / SYNTHETIC`
  header stating what it is and why it was needed.
- The next dimension inherits both the third state and the sourcing order; it
  does not re-litigate them.
- This ADR records doctrine only. The four routine-body rules carry no individual
  ADR — they apply the existing ADR-0017/DB-020 surface-scanner mold.

## Related

- ADR 0004 — deterministic rule vs mapped surface (mutilated rules give false confidence).
- ADR 0005 — an honest red over a false green.
- ADR 0016 — dimension lifecycle and the honesty bar for the coverage manifest.
- ADR 0017 — name-heuristic rules as pure surface (the scanner mold these rules reuse).
- ADR 0027 — T-SQL routine-body de-truncation (the parser prerequisite that made the four rules readable).
