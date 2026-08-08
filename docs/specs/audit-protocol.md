# Spec — The audit protocol

**Status:** draft (constitution; no code in this document) · **Target:** `v0.3.0` line

This is the contract for what an *audit* is in codefit: its layers, the law that binds them,
the invariants every audit must satisfy, and how each invariant is held by a mechanical
control rather than by anyone's memory.

## Why this exists, and why it is not invented

codefit had tools, a score, a baseline and a manifest — and no statement of what a complete
audit **is**. The consequence was not theoretical. Over one working session, six distinct
defects were found, and every one of them turns out to be the same shape: **a layer asserting
something the layer below it never established.**

The protocol below is *extracted* from those defects, not designed in the abstract. Each law
carries the defect it came from, so nobody has to take it on faith.

## The five layers

```
 L4  AGREEMENT   who decided what, and why
      ▲
 L3  DELIVERY    the report reached its reader — proven, not assumed
      ▲
 L2  VERDICT     findings, surface, score
      ▲
 L1  EVIDENCE    what was read, and what could be proven from it
      ▲
 L0  SCOPE       what will be audited, and what will not
```

### The layering law

> **A layer may assert only what the layer beneath it established.**

That sentence was first written for a single rule family — *"a practices rule affirms only
what it checked"* (`practices-dimension.md` R1). It is not a property of that dimension. It is
the law of the whole system, and every defect below is a violation of it at a different height.

## The six invariants

| | invariant | the defect it came from |
|---|---|---|
| **I1** | Nothing is declared **measured** that was not read | the SQL-DDL parser silently dropped a real column, and the fixture had been shrunk to hide it |
| **I2** | **Not measured is never clean** | `scan-all` refused the DB dimension over a language that dimension never needed |
| **I3** | **Memory does not advance without confirmed delivery** | a response the client rejected had already written 373 items as seen |
| **I4** | A **partial** result declares itself partial | a 313 KB response that did not fit, and a budget that could truncate silently |
| **I5** | What is **not covered** is declared | seven rule ids the PRD promised, neither built nor declared absent |
| **I6** | A **declared limit is re-verified** | a limit announced as live for four days after a guard had closed it |

### I1 — Nothing is declared measured that was not read

The evidence layer must be able to say *"I could not prove this"* and the verdict layer must
respect it. codefit already has the vocabulary — `db.Table.Complete`, `MarkUnproven`, the
absence-based rules that abstain on an unproven table — and it earned it the hard way: a rule
affirmed "no primary key" over DDL that declared one.

The failure mode this forbids is subtler than a wrong answer: it is a **corpus shaped to avoid
the question**. A fixture trimmed so a known limit cannot be measured leaves every test green
and every claim unfalsifiable.

### I2 — Not measured is never clean

A dimension that did not run reports `null`, never a score. A section that did not run says so
in prose a reader cannot mistake for a pass. Silence is not an answer, and neither is a zero
that means "nobody looked".

The strong form: **the absence of a section must never be ambiguous.** If a section can vanish
when everything is fine, its absence carries three meanings at once — "ran and was clean", "an
older codefit", "not applicable" — and a reader cannot tell them apart.

### I3 — Memory does not advance without confirmed delivery

**This is the layer that does not exist yet.** It is specified in full below.

### I4 — A partial result declares itself partial

Established by the change-scope work: the scope block, the mode/note pair validated in both
directions, and the budget that names how many entries it withheld and on what ordering. The
principle generalises past those two mechanisms: **a result that is a prefix of the truth must
say which prefix**, or a reader will take it for the whole.

### I5 — What is not covered is declared

The coverage manifest is what an agent reads to learn what codefit cannot do; a capability
that is neither built nor declared absent is a hole the agent cannot compensate for. The
control that enforces this derives the promised set **mechanically from the PRD**, because a
hand-maintained list of promises is the same drift one level down.

### I6 — A declared limit is re-verified

A limit ages exactly like a promise. codefit is rigorous about declaring what it does not
cover and had **nothing** re-checking that a declared limit was still true — so it announced a
fabrication for four days after a guard had closed it, and the fixture that would have caught
it had been trimmed to avoid the shape.

A declared limit that no test exercises is a belief, not a limit.

---

## L3 in full — the delivery layer

### The mistake in one sentence

`known` currently means *"codefit computed this"*. It must mean *"the reader received this"* —
**an assertion about the reader, which codefit cannot make on its own.**

### The asymmetry that decides the design

| error | consequence |
|---|---|
| over-reporting — calling "new" something already seen | noise; irritating, harmless |
| under-reporting — calling "seen" something never seen | **a false all-clear; unforgivable** |

Every uncertainty therefore resolves toward over-reporting. That single bias determines the
rest of the design.

### The states

| state | means | re-reported? |
|---|---|---|
| `new` | computed for the first time | yes |
| **`pending`** *(new)* | computed N times, delivery never confirmed | **yes, carrying the count** |
| `known` | **delivery confirmed** | no |
| `acknowledged` | a human decision, with its reason | no |
| `gone` | no longer observed | — |

`pending` suppresses nothing. It exists so codefit can say something it cannot say today:
*"this is the third time I have computed this and I have never been able to confirm you read
it."* That is not noise — it is information currently lost entirely.

### The matrix this covers

```
                        DID THE READER RECEIVE THE REPORT?

                   YES (proven)      UNKNOWN           NO (error)
                 ┌────────────────┬────────────────┬────────────────┐
 DID CODEFIT SI  │     known      │    pending     │    pending     │
 COMPUTE IT?     │  not re-sent   │   RE-REPORTS   │   RE-REPORTS   │
                 ├────────────────┼────────────────┼────────────────┤
             NO  │       —        │       —        │  not measured  │
                 │                │                │  (never clean) │
                 └────────────────┴────────────────┴────────────────┘

 TODAY: the three upper cells collapse into `known`.
```

### Promotion is implicit, and that is the point

`pending → known` requires **no new obligation on a well-behaved agent**. Any call that
references something only the report contained is proof the report arrived: fetching the
detail of an endpoint the response named, accepting a finding by its fingerprint, confirming a
surface item's verdict. Only a reader could do that.

An agent that received an error and retried references nothing. Its items stay `pending` and
are re-reported. **Correct by construction, requiring nobody to behave well.**

An explicit acknowledgement remains available for an agent that read the report and chose to
act on none of it — but it is the exception, not the protocol's load-bearing path.

### Case coverage

| case | outcome |
|---|---|
| everything works normally | the agent works, references the report, promotion happens by itself — **zero friction** |
| the report never arrives | nothing is referenced; everything is re-reported — **nothing is lost** |
| the agent reads it and acts on none of it | stays `pending`, re-reported with its count — **noise, never blindness** |
| an older agent that does not know the protocol | everything stays `pending` — irritating, **never blind** |
| the session dies mid-way | identical to "never arrived" |

**No path ends in a false all-clear.** That is the acceptance criterion for this layer, and it
is the only one that matters.

### What it costs — stated, not discovered later

- **The baseline's shape changes.** Committed baselines need a migration, or every existing
  item begins as `pending` and is re-reported once. One noisy run, and it must be declared,
  not sprung on anyone.
- **codefit must record which fingerprints left in which response**, in order to recognise a
  later reference. That is new state, and it needs its own retention answer.
- **The generated skill and the tool descriptions must teach it.** They are the only things an
  agent reads before choosing a tool; a protocol they do not describe is a protocol no agent
  follows.

---

## How the protocol is enforced

> **An invariant without a mechanical control is not an invariant. It is an intention.**

Each of I1–I6 is held by a named control — a test that fails when the invariant is violated —
and there is a **registry mapping invariant → control, with a test that fails when an
invariant has no control**.

That mechanism is not novel here; it is the same one already built for the coverage manifest,
which derives the promised set from the PRD and fails when a promise has no answer. This
applies it one level up: to the protocol itself.

The current control coverage of I1–I6 is **not asserted in this document**. Some invariants
are fully held, some partly, some not at all — and stating which from memory would violate the
layering law this document exists to establish. It is measured against the code in the census
that follows this spec, and recorded there.

## Out of scope, stated

- **No implementation.** This document is the contract; the state machine, the reference
  tracking and the migration are specified and built after the census.
- **The exact acknowledgement surface** — whether an explicit ack tool is added, and its shape
  — is a design decision, not a constitutional one.
- **The `codefit-scan-security` / `codefit-scan-endpoint` semantics** are unchanged here.
- **The PRD** is updated when the protocol is implemented, not when it is written. The PRD is
  the source of scope and design, so a protocol of this size belongs in it — but it records
  what codefit *is*, and until the layer exists this document is a proposal about it.
