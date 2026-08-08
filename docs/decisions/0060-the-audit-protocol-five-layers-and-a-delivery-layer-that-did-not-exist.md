# 0060 — The audit protocol: five layers, one law, and a delivery layer that did not exist

**Status:** accepted · **Date:** 2026-08-07 · **Supersedes:** nothing · **Contract:**
`docs/specs/audit-protocol.md`

## Context

codefit had tools, a score, a baseline, a coverage manifest and a filtering pyramid — and no
statement of what a complete *audit* is. There was no name for the states an audit passes
through, no invariant that had to hold at each one, and therefore nothing that could be
violated loudly.

Over one working session, six defects were found and closed or recorded. Reading them
together, they are not six unrelated bugs. They are one bug at six different heights:

| defect | what asserted what |
|---|---|
| the SQL-DDL parser dropped a real column, and the Pagila fixture had been trimmed to omit the table that would prove it | the verdict asserted structure the evidence never established |
| `scan-all` refused the DB dimension over a language that dimension never consulted | a refusal asserted an incapacity codefit did not have |
| a 313 KB response that did not fit the client | a result asserted completeness it could not deliver |
| seven rule ids the PRD promised, neither built nor declared absent | the manifest asserted a coverage boundary it had never checked |
| a limit announced as live four days after a guard closed it | a declared limit asserted a present that had already changed |
| **a response the client rejected had already written 373 items as seen** | **memory asserted a reader had read what no reader received** |

The last one has no partial mitigation anywhere in the codebase, and it is the only one whose
failure mode is a **false all-clear** produced by the failure path itself.

## Decision

### 1. The audit is layered, and layers may only assert downward

```
 L4  AGREEMENT   who decided what, and why
 L3  DELIVERY    the report reached its reader — proven, not assumed
 L2  VERDICT     findings, surface, score
 L1  EVIDENCE    what was read, and what could be proven from it
 L0  SCOPE       what will be audited, and what will not
```

> **A layer may assert only what the layer beneath it established.**

That sentence already existed in this repository, written for one rule family: *"a practices
rule affirms only what it checked"* (ADR 0056). This ADR promotes it from a rule about rules
to the law of the system. Every defect in the table above is a violation of it.

### 2. Six invariants, each traceable to the defect that taught it

**I1** nothing is declared measured that was not read · **I2** not measured is never clean ·
**I3** memory does not advance without confirmed delivery · **I4** a partial result declares
itself partial · **I5** what is not covered is declared · **I6** a declared limit is
re-verified.

They are numbered so a control can name the invariant it holds, and so a gap can be pointed
at rather than described.

### 3. L3 exists, and `known` changes meaning

`known` means *"codefit computed this"*. It is redefined to mean *"the reader received this"*
— **an assertion about the reader, which codefit cannot make on its own.** A new `pending`
state carries the difference: computed, delivery unconfirmed, **re-reported every run** with
the count of how many times it has been computed unseen.

The design bias is stated once and applies everywhere the protocol is ambiguous:
over-reporting is noise; under-reporting is a false all-clear. **Uncertainty resolves toward
over-reporting.**

### 4. Promotion is implicit

`pending → known` needs no new obligation on a well-behaved agent. Any call referencing
something only the report contained — an endpoint it named, a fingerprint it carried, a
surface verdict — proves the report arrived, because only a reader could produce it. An agent
that hit an error and retried references nothing, so its items stay `pending`.

This is chosen over an explicit acknowledgement tool as the load-bearing path **because it
cannot be forgotten.** An explicit ack remains available for the agent that read a report and
acted on none of it, but the protocol does not depend on it.

### 5. An invariant without a mechanical control is an intention

Each invariant is held by a named control, and a registry maps invariant → control with a test
that fails when an invariant has none. This is the mechanism already built for the coverage
manifest in ADR 0057 — which derives the promised set from the PRD rather than from a
hand-maintained list — applied one level up, to the protocol itself.

## Consequences

- **The baseline's shape changes**, and committed baselines need a migration or one noisy run
  in which every existing item begins as `pending`. Declared here, not sprung later.
- **codefit must record which fingerprints left in which response**, to recognise a later
  reference. New state, owing its own retention answer.
- **The generated skill and the tool descriptions must teach the protocol.** They are the only
  thing an agent reads before choosing a tool; a protocol they do not describe is a protocol no
  agent follows.
- **The control coverage of I1–I6 is deliberately not asserted in this ADR.** Some invariants
  are fully held today, some partly, some not at all — and stating which from memory would
  violate the very law this ADR establishes. It is measured against the code in the census that
  follows, and recorded there.
- **The PRD is updated when the protocol is implemented, not now.** The PRD is the source of
  scope and design and a protocol of this size belongs in it, but it records what codefit *is*.
  Until L3 exists, this ADR and its spec are a proposal about it.

## Alternatives considered

**Write the baseline, mark the items "delivered-unconfirmed", self-heal on the next run.**
Rejected as a standalone answer: if a provisional item is re-reported next time, writing it
buys nothing the `pending` state does not already buy — and if it is *not* re-reported, it is
the current defect with a new name. The useful half of this idea survives as `pending`'s
storage; what it lacked was a sound promotion rule.

**An explicit acknowledgement tool as the only path.** Rejected as load-bearing: an agent that
forgets it leaves memory frozen forever, and every agent written before the protocol existed
forgets it by definition. Safe (it over-reports) but it makes correctness depend on
cooperation. Kept as the exception, not the rule.

**Stop writing the baseline on the read path entirely.** Principled — memory as a purely human
decision — but it discards the noise reduction `known` exists to provide, and it answers the
wrong question: the problem was never *that* codefit remembers, it was that it remembered
something it had no basis to claim.
