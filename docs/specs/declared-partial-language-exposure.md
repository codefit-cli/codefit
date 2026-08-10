# Spec — A language may be exposed only if the response declares what it lacks

**Status:** draft · **Implements:** roadmap **P4-1** (the Go exposure decision) · **Target:**
`v0.3.0` line

## The measurement this rests on

Go's exposure was flipped locally and the real binary was driven over stdio against a Go
project containing a SQL injection. What came back:

```
scan-all on a Go project
  score      90         security: {"measured": true}
  findings   1 deterministic  (it found the SQL injection)
  surface    1 item
  endpoint   handler.go   categories: [security, authz]

codefit-coverage for "go"
  ERROR: no coverage manifest for language "go"
```

**It works.** It found a real defect in real Go code. That is the case for exposing it.

**And that is exactly the problem.** `surface_items: 1` means *authz was mapped and nothing
else was*. IDOR, over-fetching and N+1 were never looked for. Nothing in that response says
so. An agent reads a 90 with one surface item and sees an audited project.

And the one tool built to answer *"what do you cover for this language?"* — the tool an agent
would use to find out — **returns an error**.

## The rule this establishes

> **A language may be exposed only if the response declares what it does not cover for that
> language.**

This is the layering law of `docs/specs/audit-protocol.md` applied to language reach, and the
concrete form of invariant **I2** (*not measured is never clean*) and **I5** (*what is not
covered is declared*). Exposure without declaration is a partial audit that reads as a
complete one — the failure class this project exists to prevent.

The data already exists: `LanguageProvider.Capability()` declares the mapped surface
categories as an enumerated subset of a vocabulary locked to its const block. Nothing consumes
it yet on the response path.

## R1 — `codefit-coverage` answers for every registered language

A registered language with no hand-written prose manifest must still get a truthful answer
derived from its `Capability()`: the security rule ids it declares, the surface categories it
maps, **the ones it does not**, and whether a prose manifest exists.

`no coverage manifest for language "go"` is the wrong answer to *"what do you cover?"* — the
absence of prose is not the absence of capability, and the tool exists precisely so an agent
does not have to guess.

The hand-written TypeScript manifest stays authoritative where it exists; the derived answer
is the floor, not a replacement.

## R2 — the scan response declares the surface gap

Every response that carries mapped surface must state, for the language it audited, **which
categories were mapped and which were not**. Machine-readable, because an agent branches on
it; and in prose, because the prose is what survives into an agent's reasoning.

The failure to forbid, stated concretely: a Go project returning `surface_items: 1` with no
statement that three of four categories were never searched. `1` and `1 of 4` are different
claims and today the response makes only the first.

## R3 — the locks change meaning; they are not deleted

Locks A, B and C exist to keep Go unexposed, and flipping its exposure turns **seven** tests
red. That is them working — the boundary was crossed. Now that crossing it is a deliberate
decision, **the boundary moves rather than disappears**:

- what was *"the resolvable set is exactly TypeScript"* becomes the exposed set, whatever it
  is, asserted explicitly;
- and a **new lock replaces the old guarantee**: *an exposed language must declare a
  non-empty capability, and any surface category it does not map must appear in the
  response's not-covered statement.*

Deleting a lock because the thing it guarded became allowed is how a guarantee is lost. The
guarantee here was never "Go stays out" — it was **"nothing is exposed without being
declared"**, and that one still holds.

## R4 — what Go declares, and what it is not

Measured at `main` @ `810b816`: **6** security rules (`SEC-001, 010, 013, 040, 050, 052`),
**1 of 4** surface categories (`authz`), **4** practices rules, **no** prose manifest.

Exposing Go is **not** a claim of parity. `codefit-scan-security` and `scan-all` on a Go
project run six rules and map one category; the README and the generated skill must say that
where a user reads them before installing, exactly as the per-dimension reach statement does
today.

## Out of scope, stated

- **No new Go rules or surface categories.** This exposes what exists; growing it is separate
  work, and each addition is a capability-declaration change the controls already cover.
- **`internal/providers/golang/coverage.go`** — a prose manifest for Go. R1 makes it
  unnecessary for correctness; if it is written later it becomes authoritative over the
  derived answer. **P1-4b's `PRAC-004` entry now has a landing site in the declared rule
  lists** and may be taken here or after.
- The `db-`/`dw-` prefix partition residual, declared in ADR 0064.

## Test contract

Each proven by **mutation** — break the exact behaviour, watch it fail, restore, watch it
pass.

1. **The end-to-end case that motivated this**, driven through the real handler: a Go project
   with a SQL injection returns the finding **and** a statement that three of four surface
   categories were not mapped. *(Mutation: drop the not-mapped statement — the test must fail
   on its absence, not on its wording.)*
2. `codefit-coverage` for `go` returns a derived answer, never an error, and it names the
   unmapped categories.
3. TypeScript's answer is **unchanged** — its prose manifest still wins. Compare against a
   real `git worktree` of the pre-change commit, field for field.
4. The not-mapped set is **derived** from the capability and the locked vocabulary, never a
   literal: adding a fifth category to the vocabulary without mapping it must make it appear
   in the not-covered statement with no other edit.
5. R3's replacement lock fails when a language is exposed while declaring an empty capability,
   and when a mapped-category gap is missing from the response.
