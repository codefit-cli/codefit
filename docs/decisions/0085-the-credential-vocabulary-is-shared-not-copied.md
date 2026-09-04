# 0085 — The credential vocabulary is SHARED, not copied

Date: 2026-09-04
Status: accepted
Closes the TypeScript half of the defect [ADR 0075](0075-sec-001-affirms-only-what-the-name-established.md)
fixed for Go. Adds one operator to the subset declared in `rules/README.md`.

## Context

`SEC-001` constrained `$NAME` with an **unanchored** regex, so any identifier
merely *containing* a credential word matched:

```ts
const tokenizer = "whitespace";   // SEC-001, severity high, confidence 1.0
```

That is not just a false positive. It is an **affirmation at confidence 1.0** —
the certainty class the baseline refuses to auto-silence (ADR 0011) — so it
reappeared on every scan until a human accepted it by hand. The loudest and
stickiest thing codefit emits, on a tokenizer.

Go had fixed exactly this in ADR 0075 by matching name **components** through
`internal/core/namematch`. TypeScript never got the fix, because a rule could
only express a regex. The cross-provider case table recorded the divergence
faithfully — declared, not hidden — but a declared false all-clear is still one.

The Go work also measured the trap, and this decision reproduced the measurement
rather than trusting it. Over 41 names, comparing the substring regex against
`namematch.MatchSet`:

| | count | names |
| --- | --- | --- |
| false positives the component matcher kills | **9** | `tokenizer`, `tokenize`, `tokenized`, `secretariat`, `secretaryId`, `passwordless`, `tokenizerConfig`, `subtokenizer`, `credentials_note` |
| true positives lost by switching | **1** | `credential` |

`API_KEY` is the case ADR 0075 warned about and it survives: `lower("API_KEY")`
is `api_key`, which does *not* contain `apikey`, so the substring arm was the
only thing carrying it — but the component matcher carries it too.

## Decision

### 1. A new operator, `metavariable-name`

```yaml
metavariable-name:
  $NAME: credential
```

The metavariable matches only if the identifier carries a member of the named
vocabulary as a name **component**.

**It is an operator and not a better regex, and that was measured.** Go's regexp
is RE2: no lookbehind, no lookahead. Anchoring `token` to a camelCase boundary
requires asserting what *precedes* it — `accessToken` yes, `subtokenizer` no —
and RE2 cannot. Enumerating the case variants multiplies every alternative by
every boundary and is unreadable long before it is correct.

Vocabularies are a **closed set** and an unknown one is a **compile error**: a
rule that named one and silently matched nothing is precisely the defect the
compile gate (PR #173) exists to prevent.

**Why `namematch` is reachable from the core.** It names no provider. It is the
shared vocabulary the cross-provider table binds, and the rule engine already
lives beside it in `internal/core`. ADR 0083 holds: detection is per-language and
the rule engine is TypeScript's — but the *words* are the same words Go looks
for, and keeping them in one place is the entire point.

### 2. `credential` joins the vocabulary, in `securityOnlyTokens`

It is the one true positive the switch would have cost. It goes in
`securityOnlyTokens` and **not** `credentialShared`, because `credentialShared`
feeds `DB053Union()`, frozen name for name across 29 corpora (ADR 0047) that can
no longer be re-measured. A credential is not a DB-053 sensitive column name.
`TestDB053UnionFrozen` proves the union did not move rather than assuming it.

This also closes a **silent gap in Go**: `const credential = "abc123"` fired
nothing there. The cross-provider table had that row as `goFires: false` with the
reason "deferred to measured widening work rather than added by inference". This
is that work.

## Consequences

- Nine false affirmations at confidence 1.0 stop, in TypeScript.
- `credential` and `credentials` now fire in **both** providers.
- **The cross-provider divergence list is empty**, and the class cannot return by
  drift: the two columns are now the same function of the same set. It can return
  only by editing SEC-001's declaration, which `tsNameVocabulary` fails on with a
  message naming the defect.
- **One declared narrowing.** An all-lowercase concatenation carries no boundary
  to tokenize on, so `secretkey` — which TypeScript's substring matcher happened
  to catch — is now silent in both. `secretKey`, `secret_key` and `SECRET_KEY`
  all fire. Accepted because the mechanism that carried that single spelling is
  the same one that affirmed `tokenizer`; keeping it meant keeping nine false
  affirmations with it. Declared in the case table and in `COVERAGE.md`.
- The table did not become redundant when the mechanisms converged. Control 3 —
  every vocabulary token must be exercised by a row — is what forced
  `credentials` to carry a declared verdict before this could ship.
