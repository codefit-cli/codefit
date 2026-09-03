# 0084 — The rule engine gets ONE ellipsis, scoped to object properties

Date: 2026-09-03
Status: accepted
Supersedes nothing. Narrows the "no ellipsis" clause of the Semgrep subset
declared in `rules/README.md`; ADR 0003 anticipated exactly this reconsideration.

## Context

`SEC-001` declared a single shape, `const $NAME = $VALUE`. A credential written
any other way was **silent** — not a low-confidence finding, not surface for the
agent: nothing. That is under-reporting, the direction the audit protocol's I3
calls unforgivable.

The obvious fix — add `({$NAME: $VALUE})` to `pattern-either` — was proposed,
and it is wrong. `matchNode` compares the named-child **count**, so an object
pattern with one pair matches only an object with exactly one pair.

A census settled it. Across two real TypeScript projects, using codefit's own
parser, every object holding a credential-named string property:

| project | such objects | their arity | objects with any string prop, arity 1 | arity > 1 |
| --- | --- | --- | --- | --- |
| A (209 files) | 3 | 5, 5, 5 | 447 | 1030 (70%) |
| B (155 files) | 1 | 3 | 72 | 376 (84%) |

**Four of four are multi-property. None is at arity 1.** The single-pair pattern
would have reached zero of them. This is not a rule fix.

The same probe surfaced a second, undeclared consequence: `SEC-079`/`SEC-080`
(XSS) had the identical limit and nobody had written it down. They fired only
when `__html` was the object's sole property. The canonical JSX shape
`dangerouslySetInnerHTML={{__html: h}}` happens to be a one-property object, so
the common case worked **by coincidence** and the gap stayed invisible.

## Decision

**Objects get an ellipsis. Nothing else does.**

A pattern `object` containing a spread-of-metavariable matches a **subset**:
every non-ellipsis member must match some member of the code object, in any
order; everything else in the code object, including its own real spreads, is
ignored.

```yaml
- "({...$REST, $NAME: $VALUE})"
```

Four properties define it, and each one is load-bearing:

1. **Spelled `...$REST`, not Semgrep's `...`.** Measured, not preferred. Since
   the compile gate (PR #173) a pattern whose tree contains an ERROR node is
   rejected, and the TypeScript parser cannot parse a bare ellipsis inside an
   object: `{..., $K: $V}` and `{$K: $V, ...}` both produce `HasError=true` and
   would be rejected at compile. `{...$REST, $K: $V}` parses clean as
   `object[spread_element, pair]`. A spread of a metavariable is ordinary
   TypeScript. **We diverge from Semgrep's surface syntax because the alternative
   is a feature that cannot be shipped.**
2. **Zero-or-more.** One alternative covers `{apiKey: "x"}` and a credential
   buried in a five-property config object, so a rule never carries a separate
   single-pair alternative.
3. **The marker does not bind.** `$REST` is punctuation, not a capture.
4. **Opt-in per pattern.** A pattern without a spread keeps exact-arity matching
   untouched. This is what made the change safe to ship: the behaviour of zero
   existing rules changed. It is pinned by a test whose only job is to go red if
   the extension ever leaks.

### The consequence that was not obvious

A subset match has **many** valid assignments where an exact-arity match had
exactly one, and the engine used to return the first and let the rule apply
`metavariable-regex` afterwards. Correct while one assignment existed; wrong the
moment the ellipsis shipped. For `{...$R, $NAME: $VALUE}` over a config object,
the first assignment binds `$NAME` to whatever property comes first, the regex
rejects it, and **the credential three properties later is never considered** —
under-reporting produced by the fix for under-reporting.

So the constraint now **steers** the search: a binding that violates its
`metavariable-regex` is rejected at bind time, and the search backtracks.
Bindings never change once committed inside an attempt, so rejecting early is
both safe and complete.

This was not foreseen in the design. It was found by a test written from the
census — the multi-property row — which is the entire argument for measuring the
shape before closing a detector.

## Consequences

- `SEC-001` reaches `const`, `let`, `var`, type-annotated `const`/`let`, and an
  object property at any position in an object of any size.
- `SEC-079`/`SEC-080` reach `__html` in an object of any size. An undeclared
  false-all-clear is closed.
- **One shape gap remains and is declared**: a class field. The same census
  counted 316 class-field string assignments, ~10× the `const` shape. It is
  **not** the arity problem — measured: `class $C { $NAME = $VALUE }` finds zero
  even in a class with a single field, because `unwrap` peels only `program`,
  `expression_statement` and `parenthesized_expression`, so a rule cannot address
  the `public_field_definition` node on its own. Closing it needs a different
  mechanism, not another alternative, and it is tracked separately.
- The Semgrep subset is now: no general ellipsis, no `mode: taint`, and one
  scoped object ellipsis. `rules/README.md` states it with the parse table.
- ADR 0003 noted `pattern-inside`'s expressiveness was bounded without an
  ellipsis and said it would be "reconsidered when a real rule needs it". This is
  that reconsideration, and it deliberately grants the narrowest thing that
  answers the measurement — not a general ellipsis.
