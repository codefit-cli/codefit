# 0056 — A practices rule affirms only what it checked, and PRAC-004 is dropped

**Status:** accepted · **Date:** 2026-08-04 · **Phase:** 3 (RF-05, thread H1, slice S2) ·
**Builds on [ADR 0017](0017-name-heuristic-db-rules-as-pure-surface.md) and
[ADR 0055](0055-practices-is-its-own-dimension-and-carries-the-smallest-weight.md)**

## Context

The Go provider has shipped five best-practice rules since Phase 0
(`internal/providers/golang/practices.go`). Every one of them emits at `Confidence: 1.0`
with no `Probabilistic` flag. That is the **affirmation** channel — the one whose whole
value is that its statements are facts, and the one ADR 0017 fenced off from anything that
merely guesses. Practices has no surface channel to demote a guess into: the dimension is
deterministic by spec, with no `codefit-surface-practices` family.

Read against their own code, three of the five said more than they had established:

| id | what the message claimed | what the code checked |
|---|---|---|
| PRAC-004 | the goroutine was started "without a visible WaitGroup or channel to synchronize it" | that a `go` statement existed. **No synchronization detection existed anywhere in the file.** |
| PRAC-001 | "**Possibly** ignored error" | that the last LHS of a multi-assign was `_` and the RHS was a call — it hedged at certainty 1.0, and it never established the discarded value was an `error` |
| PRAC-003 | "interface{}/any discards type information" | every empty `interface{}` node, including generic constraints and variadic sinks where `any` is the only thing that can be written |
| PRAC-005 | "library code should return errors instead" | any `panic` outside a `_test.go` file — it never distinguished a library from a command, and it carried its own private notion of a test file |
| PRAC-002 | a `defer` governed by a loop | exactly that |

PRAC-004 is the sharpest case. It asserted a fact it never established — the precise
defect codefit exists to catch in other people's code. Shipping it would make codefit an
auditor that does the thing it audits for.

## Decision

### 1. The rule-level bar: a message may state only what its code established

A rule that cannot check what its message claims has exactly two honest outcomes — **teach
it to check, or drop it.** "Soften the wording to match the weaker check" is not a third
one. A finding nobody can act on is noise, and noise in an affirmation channel is worse
than silence: it teaches the reader to skim the channel that is supposed to be believed
without reading.

### 2. PRAC-004 is dropped, not taught

Dropping was chosen over teaching because a **sound** affirmation is not reachable here:

- This provider parses with `go/ast` only. There is no `go/types` information, so a
  candidate WaitGroup or channel cannot be resolved to its type.
- Synchronization routinely lives outside the `go` statement's own scope: in the callee
  (`go w.run()`), in a struct field, in the caller, or in an `errgroup` — which is not
  even a `go` statement.
- A conservative enclosing-scope check would therefore keep producing false affirmations,
  just fewer of them, and the dimension has no probabilistic channel to demote them into.

The rule and its `case *ast.GoStmt:` branch are removed outright — no commented-out body,
no disabled flag. `TestPracticeUnsynchronizedGoroutineRuleIsGone` locks it: over both a
bare goroutine and a goroutine correctly guarded by a `sync.WaitGroup`, no `PRAC-004` is
ever emitted.

**PRAC-004 is permanently not covered by the Go provider**, on the same footing as `DB-012`
and `DW-022`.

### 3. PRAC-001 is retitled to the fact it decides

`Possibly ignored error` → **`Discarded return value`**. The description already named the
syntactic fact and is unchanged; the suggestion still points at the error case as the
common reason to look, without asserting this value is one. The hedge is gone, and with it
the mismatch between "possibly" and `Confidence: 1.0`.

### 4. PRAC-003 excludes the positions where the empty interface is unavoidable

It no longer fires on a **generic type-parameter constraint** (`func F[T any]`,
`type S[T any]`) or on a **variadic parameter's element type** (`...any`,
`...interface{}`). In both, `any` is what the language requires; the author discarded
nothing they could have kept. It still fires on an ordinary variable type, field type,
non-variadic parameter, result, slice element and map value.

Two consequences of the real AST shapes, verified with a probe rather than assumed:

- **`any` parses as `*ast.Ident`, not `*ast.InterfaceType`** — it is a predeclared
  identifier, not a keyword. The rule therefore never fired on the `any` spelling at all,
  only on `interface{}`. Excluding the idiomatic positions without also recognising `any`
  would have left the message ("interface{}/any …") claiming coverage the code did not
  have, so the `any` spelling is now recognised in type positions. This is the one place
  this slice makes a rule fire *more* often; it is what makes the existing message true.
- Because `any` is an identifier, it can be shadowed. The rule therefore ignores the `any`
  spelling in a file that declares its own top-level `any`, and requires the identifier to
  sit in a type position (a conversion `any(v)` is an expression, not a type). **Declared
  limit:** the redeclaration check is file-scoped, so a redeclaration in a sibling file of
  the same package is not seen. The `interface{}` spelling cannot be redeclared and is
  unaffected either way.

### 5. PRAC-005 establishes "library", and stops deciding what a test file is

Two narrowings:

- It fires only when the parsed file's package is **not `main`**. `package main` is a
  command; anything else is importable. That is the only library/command distinction the
  AST can make, and it is exactly the one the message rests on. The title follows the
  check: `panic in production code` → **`panic in library code`** — "production" was not
  established either once the path check was removed.
- The `strings.HasSuffix(p.path, "_test.go")` hardcode is **removed**. Per the spec's R2,
  no rule carries its own notion of a test file: `config.PathCriticality` exists, the Go
  provider already declares `Test: ["**/*_test.go"]`, and the practices **sensor** will
  apply criticality on the way out exactly as the security sensor does for RF-10. The
  sensor arrives in S3; in S2 the rule simply stops making the decision.

Removing the hardcode changes no user-visible behavior today: nothing in the product
consumes `AnalyzePractices`. `providerForLanguage` (`internal/mcp/scanall.go`) maps only
`typescript`/`ts`/`tsx` and returns `nil` for everything else, and the only caller of the
Go `AnalyzePractices` in the repository is `golang_test.go`.

### 6. PRAC-002 is unchanged, and locked

Its claim already matched its check. It gains a third fixture — a `defer` with no
enclosing loop — so "unchanged" is proven rather than assumed.

### 7. No coverage manifest is created for Go in this slice

The three-level coverage chain (rules → `coverage.go` / `dbcoverage.go` → `COVERAGE.md`)
has no Go link: `internal/providers/golang/coverage.go` does not exist, because the Go
provider is unreachable through MCP (§ Out of scope of the
[spec](../specs/practices-dimension.md)). Creating one is out of scope for S2, so the
PRAC-004 drop is recorded **here and in the CHANGELOG** instead. When a Go manifest is
created, it must carry the drop.

## Consequences

- The practices rules that survive say only what they decided, which is the precondition
  for wiring the dimension into an affirmation channel at all.
- **No user-visible detection changes.** No `scan-all`, `scan-security` or `check-db`
  response moves, no baseline fingerprint moves, no baseline action is needed — the Go
  provider is not reachable through MCP.
- PRAC-004's coverage is a permanent hole that a future Go manifest must state, not
  silently omit.
- Each kept rule now has the two-fixture pair the spec's test contract requires (the thing
  the message names → fires; the same shape without the claimed property → silent), and
  every one of them was proven by mutation.
- The idiomatic-position exclusions are conservative: PRAC-003 under-fires in a few exotic
  type positions (an explicit type argument `F[any](…)`, for instance) rather than risk a
  false affirmation. Under-detection is a gap; a false affirmation is a lie.
