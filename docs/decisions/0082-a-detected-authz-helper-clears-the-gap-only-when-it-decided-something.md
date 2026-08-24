# 0082 — A detected authz helper clears the gap only when it decided something

**Status:** accepted · **Date:** 2026-08-24 · **Closes** [issue #149](https://github.com/codefit-cli/codefit/issues/149)
· **Amends** [ADR 0006](0006-scan-all-endpoint-synthesis.md)'s authz gate, which
`known_authz_detected` alone used to satisfy. Everything else in 0006 is unchanged.
· **Applies** [ADR 0067](0067-every-surface-producer-emits-non-nil-structural-facts.md)'s
absence discipline to a second fact.

## Context

`report.surfaceGap` cleared the **access gap** for an authz concern on one
condition:

```go
case "authz":
    if !it.StructuralFacts["known_authz_detected"] {
        return true, gapAccess
    }
```

And `known_authz_detected` was true for *any* call to a recognized helper,
however the call was written. So this handler left the `actionable` bucket:

```ts
export async function GET(req: Request, { params }: { params: { id: string } }) {
  await requireOwner(params.id);          // result discarded — decides nothing here
  return Response.json(await prisma.order.findUnique({ where: { id: params.id } }));
}
```

The guard is *mentioned*. It gates nothing at this site. codefit reported the
endpoint as resolved and clean.

That is **under-reporting**, the direction `docs/specs/audit-protocol.md`'s I3
calls *"a false all-clear; unforgivable"* — and it was reachable in ordinary use:
reproduced against the shipped binary before this ADR was written, with
`known_authz_detected: true` on a handler whose guard's result went nowhere.

**How it survived.** An existing control encoded it. The end-to-end barrier test
`TestHandleScanAll_RegisteredHelperClearsAuthzNotIDOR` used
`await requirePermission("admin");` — the discarded shape — as its fixture while
testing something orthogonal (a registered helper clears authz and never IDOR).
The suite was green *because* the bug was in a fixture. That is exactly the
failure `CLAUDE.md` warns about, arriving from the fixture side rather than the
assertion side.

## The decision that is NOT obvious

**A discarded result does not mean the guard is absent.** `await requireAuth()` is
a common shape for a helper that **throws** or **redirects**, and such a helper
gates correctly with its result unused. The helper's body is usually in another
file — the frontier (ADR 0005) — so codefit cannot answer it from the handler.

So the fix is **not** to flip `known_authz_detected` to false. That would trade
under-reporting for a different inaccuracy: asserting "no known authorization was
detected" where a call demonstrably happened, and hiding from the agent the one
fact it needs to reason.

## Decision

### D1 — a new fact, not a changed one

`known_authz_detected` keeps its meaning: **a recognized helper WAS called here.**
It stays true for a discarded call, because that is true.

A second fact is added, TypeScript only for now:

```
authz_result_used   the detected guard DECIDED something here — its result
                    reached a branch, a return, an assignment or another call,
                    or a middleware guard ran before the handler.
```

`known_authz_detected: true` beside `authz_result_used: false` is the precise
statement of what codefit saw: *the guard was called and its answer went
nowhere.* It is a fact, never a verdict — the signal says so in words and hands
the agent the exact question ("check what the helper does on failure").

### D2 — the gap gate requires both

The access gap clears only when a detected guard also decided something.
Over-reporting a handler whose guard throws is noise; under-reporting one whose
guard decides nothing is a false all-clear, and I3 settles which way to err.

### D3 — usage is decided by SPAN, not by a parent pointer

`syntax.Node` deliberately exposes byte ranges and no `Parent()` — a choice the
interface documents, because `go/ast` has no native parent. A call is
**discarded** when its byte range is exactly the expression of an
`expression_statement`, unwrapping one `await`. Anything else — a condition, an
initializer, an argument, a `return` — consumes the value.

### D4 — a middleware guard is exempt, and the reason is not a special case

A middleware guard runs **before** the handler by construction. There is no
result for the body to consume, so asking whether one was used is the wrong
question, and answering "no" would turn every middleware-protected route
actionable. `authz_result_used` is therefore true when a middleware guard is
present.

### D5 — ABSENCE is not false. This is the load-bearing half

The Go provider omits `known_authz_detected` entirely when no helper set was
searched, because *"a false against an empty searched set would be the vacuous
claim the spec forbids"* (ADR 0067). It emits no `authz_result_used` at all.

A naive `if !facts["authz_result_used"]` reads an absent key as false, so it
would make codefit assert *"the guard's result was not used"* about a producer
that **never looked** — inventing a fact out of a missing one, for a whole
language, silently.

The gate therefore raises the gap only when the fact is **present and false**:

```go
if used, stated := it.StructuralFacts["authz_result_used"]; stated && !used {
    return true, gapAccess
}
```

Locked by mutation M2 below. Go's behaviour is byte-identical to before.

## Mutations

Each run with `build` and `vet` clean first, so no red is a compile error.

| # | mutation | expected RED | result |
|---|---|---|---|
| M1 | the span never matches, so nothing is ever seen as discarded | both `discarded` cases of `TestAuthzResultUsed` | FAIL → restore ok |
| M2 | drop the presence check, letting absence read as false | `TestAuthzGapRequiresTheGuardToDecideSomething/PRODUCER_NEVER_LOOKED` | FAIL → restore ok |
| M3 | remove the gap gate entirely — the original bug, restored | `…/guard_called,_result_DISCARDED` | FAIL → restore ok |
| M4 | remove the signal, making the fact invisible to the agent | `TestAuthzDiscardedResultIsStatedInTheSignals` | FAIL → restore ok |

M1's first attempt left an unused variable (`vet` = 1). A red that is a compile
error proves nothing, so it was re-run against an anchor that keeps the code
valid.

## What this does NOT establish

- **TypeScript only.** The Go provider does not compute result usage, and D5 is
  precisely what keeps that honest rather than guessed. Adding it to Go is a
  separate change, and until then Go's authz gap behaves exactly as before.
- **Nothing about whether the helper throws.** codefit reports that the result
  went nowhere. Whether that is safe is the agent's question, by design.
- **No claim about middleware correctness.** D4 exempts a detected middleware
  guard from the result rule; it does not verify that the middleware actually
  covers the route.

## Consequences

- Handlers whose only guard has a discarded result move from `resolved_clean`
  back to `actionable`. On a project using throwing guards this is **more**
  actionable items than before — accepted, and stated in the CHANGELOG as a
  behaviour change rather than discovered by a user.
- `TestHandleScanAll_RegisteredHelperClearsAuthzNotIDOR`'s fixture now uses a
  gating shape, with the reason recorded in the test itself so a later reader
  does not mistake it for a loosened control.
