# Spec — The coverage manifest answers for every capability the PRD promises

**Status:** draft · **Phase:** 3, priority P0-1 (see `docs/roadmap.md`) · **Target:**
`v0.3.0-alpha` line

## The defect

`codefit-coverage` returns the manifest **to the agent**. It is the only thing an agent reads
to learn what codefit can and cannot detect. Everything the agent does afterwards — what it
compensates for, what it trusts codefit to have handled — rests on it.

The manifest has two mechanical controls today (`internal/core/dbcoverage/dbcoverage_test.go`):

- **Control A** — every rule registered in `dbrules.All()` / `dwrules.All()` /
  `crossrules.All()` has a manifest entry. *Guards: built but undeclared.*
- **Control B** — every rule-ID token the manifest **mentions** is either registered or named
  in `NotCovered()`. *Guards: prose describing a rule that does not exist.*

Both look **outward from the code**. Neither looks **inward from the PRD**, which is the
project's declared source of scope. So a capability the PRD promises that was never built and
never declared absent passes every control — it is invisible from both ends.

**Measured on `main` @ `8657607`.** Extracting every `DB-###`/`DW-###` token from
`docs/PRD-codefit-v1.4.md` and comparing against the registered rule set and `NotCovered()`:

| | count |
|---|---|
| distinct DB/DW IDs the PRD names | 31 |
| registered rules | 23 |
| **promised, not registered, not declared absent** | **7** |

The seven: `DB-021`, `DB-022`, `DB-023`, `DB-032`, `DB-101`, `DB-102`, `DB-201`.

This is the **only** debt in codefit's whole inventory that was not self-declared somewhere.

## The seven are not one problem — they are three

Reading each against the code changes what the fix has to be. A control that treats them
alike would force a dishonest answer for two of the three classes.

### Class 1 — delivered, under a different identifier (`DB-201`)

The PRD's RF-04 names `DB-201` for N+1 detection. **N+1 ships.** It has since `v0.2.2`, as
the `nplus1` surface category (`codefit-surface-nplus1`,
`internal/providers/typescript/nplus1.go`) — a *provider surface category*, not a schema rule
with an ID.

So the capability exists and the manifest describes it; only the PRD's identifier is absent.
An agent searching for `DB-201` concludes it is missing, and is wrong.

Note the related, already-handled case: the PRD's `DB-011` became `DB-011a` and `DB-011b`
when it shipped. That one is answered because the manifest prose happens to carry the parent
token. `DB-201` has no such luck — nothing in the manifest ever spells it.

> **Correction (implementation, measured — that last clause is false).** The **composed**
> manifest an agent actually receives has named `DB-201` since `v0.2.2`:
> `internal/providers/typescript/coverage.go:44` spells it in the N+1 reasoning entry, and so
> do `COVERAGE.md` and `README.md`.
>
> What is true, and narrower: **`internal/core/dbcoverage/` does not** — and `dbcoverage` is
> the DB/DW namespace owner and the only source Controls A, B and D read. So the defect is not
> "the agent is never told"; it is that **an agent asking the DB dimension for `DB-201` finds
> nothing**, while the same identifier is answered one composition level up.
>
> **The fix is unchanged and the third bucket is still required**, for the reason R3 gives:
> the control reads `dbcoverage`, and `NotCovered()` there would be a lie about a capability
> that ships. What changes is the severity — this one was less dangerous than the spec framed
> it, and the five genuinely-absent ids are the sharp end of P0-1.
>
> **Second correction, on the count.** The "seven" holds under *substring* matching, which is
> what the implemented control uses. Under the *whole-token* matching Controls A and B use it
> would be **eight**: `DB-011` is answered only because `DB-011a`/`DB-011b` contain it — the
> very leniency the paragraph above depends on. The choice is deliberate and its cost is
> declared in the test: a promised id that is a strict prefix of a longer one would be falsely
> answered. No id in the PRD or in the rule set has that shape today.
>
> Left uncorrected above so the original reasoning stays legible.

### Class 2 — decided, not built (`DB-022`)

Materialized view without a refresh. Its analytic twin `DW-022` is currently recorded as
**permanently** not covered, because refresh cadence lives in scheduler state that static DDL
does not carry.

That reasoning is correct about **affirmations** and wrong about **surface** — see
`docs/roadmap.md` P4-3. codefit cannot say "this view is stale"; it can enumerate the
materialized views and let the agent, which reads the cron and the migrations codefit never
sees, resolve it. Reversing `DW-022` is a separate change with its own ADR and a parser floor
(`db.View` has no way to say a view is materialized). **This spec does not do that** — it
only requires `DB-022` be declared honestly *today*: not covered, with the reason, pointing at
the decision.

### Class 3 — genuinely absent (`DB-021`, `DB-023`, `DB-032`, `DB-101`, `DB-102`)

Not built, no decision recorded. Each gets a `NotCovered()` entry stating what it would
detect and why it is not covered yet. `DB-101`/`DB-102` are marked in the PRD itself as
*"vía razonamiento del agente"* — surface, never affirmations — and the entry says so, so a
future implementer does not build them as deterministic rules.

## R1 — The manifest answers for every capability the PRD promises

A rule ID the PRD names must be **answerable** from the manifest. There are exactly three
honest answers, and no fourth:

1. **It is registered** — a real rule. Control A already forces it to have an entry.
2. **It is named in `NotCovered()`** — declared absent, with its reason. This bucket means
   *not covered today*, not *never*: `DW-020` and `DW-021` both sat here and left when they
   shipped.
3. **It is delivered under another identifier** — the capability exists but not as that ID.

Silence is not an answer. An ID the manifest never mentions in any of the three ways is a
hole the agent cannot see, and it is precisely the failure this spec exists to close.

## R2 — The control derives the promised set mechanically, from the PRD

The enforcement test reads `docs/PRD-codefit-v1.4.md` and extracts every rule-ID token. It
does **not** consult a hand-maintained list of "IDs the PRD promises".

This is not a stylistic preference. A second hand-maintained list is the same failure mode one
level down: it drifts from the PRD exactly the way the manifest drifted, and it would pass its
own test while doing so. The existing Control A is mechanical for the same reason
(`registeredIDs()` derives from `All()`, never from a literal), and this control must hold to
the same standard or it is theatre.

Consequence, stated so it is chosen and not discovered: **editing the PRD can fail this
test.** That is intended. The PRD is the declared source of scope (`CLAUDE.md`), so adding a
rule ID to it is a promise, and a promise with no manifest answer is the defect.

## R3 — The third answer needs its own bucket, and Control B must move with it

Answer 3 has nowhere to live today, and putting it in the wrong place breaks an existing
control.

`DB-201` is **covered** — it belongs in `Reasoning()`, with the N+1 entry. But
`Reasoning()` is exactly what Control B scans, and Control B fails any mentioned token that is
not registered and not in `NotCovered()`. So naming `DB-201` beside the N+1 entry makes
Control B report a phantom capability for a capability that ships.

Putting it in `NotCovered()` instead would be a lie: N+1 is covered.

So the manifest gains a **third bucket** whose meaning is *this promised identifier is
delivered by this other thing* — carrying both names and enough prose to follow the mapping.
And **Control B is amended in the same change** to accept an ID answered there. Shipping the
new bucket without amending Control B leaves the tree red; shipping the amendment without the
bucket widens Control B's hole. They are one change.

## R4 — The controls stay honest about what they guarantee

Control C is deliberately unimplemented, documented rather than faked, because
`internal/core/paradigm/` registers no rules and a correspondence test is *impossible* there
— not merely unwritten. That discipline governs this control too: it guards **correspondence
(an answer exists)**, never **accuracy (the answer is true)**.

A `NotCovered()` entry that misdescribes why a rule is absent passes this control. That limit
is stated in the test's own comment, exactly as Control A states it. Claiming otherwise would
be the same over-promising manifest this effort exists to kill.

## Out of scope, stated

- **Building any of the seven rules.** This spec makes the manifest honest about them; it
  implements none.
- **Reversing `DW-022` to surface.** Decided (roadmap P4-3), owed its own ADR and a
  `db.View` parser floor. Here `DB-022`/`DW-022` are only *declared* against today's truth.
- **`SEC-###` and `PRAC-###` IDs.** `dbcoverage` owns the DB/DW namespace; the security and
  practices manifests are separate sources with their own (absent) controls. The same
  asymmetry very likely exists there — recorded in `docs/roadmap.md`, not fixed here.
- **The Go coverage manifest.** It does not exist; roadmap P1-3.

## Test contract

Each proven by **mutation** — break the exact behaviour, watch it fail, restore, watch it
pass. Both runs written into the commit message.

1. **The new control fires on today's tree before the entries are added.** This is the red
   that must exist first: run it against the current manifest and watch it name all seven.
   *(A control introduced together with the entries that satisfy it has never been seen to
   fail, and is an ornament.)*
2. **The control is not vacuous.** If PRD extraction yields zero IDs, the test fails loudly
   rather than passing — the same positive control `TestManifest_CurrentRuleSet_Passes`
   provides for Control A.
3. **The negative fires on a fabricated ID specifically.** Append `DB-998` to the derived
   promised set and assert it is reported as the *only* unanswered ID — exercising the real
   check loop, not a description of it.
4. **Each of the three answers satisfies the control**, proven one at a time: a registered
   ID, an ID in `NotCovered()`, and an ID in the new bucket.
5. **Control B accepts the new bucket and still rejects a true phantom.** A fabricated token
   in `Reasoning()` that appears in no bucket must still fail Control B after the amendment.
   *(The mutation: widen Control B to accept anything.)*
6. **`codefit-coverage` returns the new bucket to the agent.** A manifest entry no tool
   surfaces is a comment, not a capability — the response is what the agent reads.
