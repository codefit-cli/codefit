# ADR 0004 — The boundary between a deterministic rule and mapped surface

**Status:** Accepted · **Date:** 2026-06-22 · **Phase:** 1 (TS security rules)

## Context

codefit's rule engine matches a subset of the Semgrep format and deliberately
**excludes ellipsis** (`...`) (ADR-recorded in PRD §17). Building `pattern-inside`
surfaced what that exclusion means in practice: without ellipsis the structural
matcher cannot express "code arbitrary in the middle" — e.g. "inside a class with
any method" is not expressible, because a bare metavariable parses as one node
(a field), not "any intervening structure".

The risk: if that limit is misread, the deterministic rules of slice 7 could be
written too narrowly and produce **false negatives** — or, worse, try to follow
data they cannot follow and become mutilated rules that give false confidence.

## The key distinction (corrected)

A first framing — "a rule can only match the literal exact case (`md5(password)`
but not `md5(someVar)`)" — is **wrong**, and the distinction matters:

- A **metavariable generalizes the hole of one node.** `md5($X)` matches
  `md5(password)`, `md5(someVar)`, and `md5(anything)`. It is *not* narrow.
- What the matcher cannot do is **follow the data** — resolve `someVar` back to
  its definition (`someVar = password`). That is taint tracking.

For `md5`, following the data does not matter: md5 is weak regardless of its
argument, so the rule is conclusive over the local subtree without following
anything.

So the real boundary is **not** "literal vs ellipsis". It is:

> **Deterministic rule** — the dangerous pattern is **conclusive over a local
> subtree**, with metavariables covering the holes of single nodes.
>
> **Mapped surface** — to conclude, you must **follow the data** through
> variables or intervening code (taint).

`md5($X)` is a rule. `const q = "..." + x; db.query(q)` is surface.

## Decision

Deterministic rules cover only what is **syntactically local and conclusive**.
Anything that requires following the data to a source goes to **surface mapping**
(Prompt 1.3), where the agent reasons it. This is the same boundary as
"no taint analysis" (PRD §10, §17), seen from another angle: ellipsis is what
syntactic taint would need, and taint is exactly what we send to surface. The
absence of ellipsis and the absence of taint are one decision.

### Applying it to the slice-7 security categories

| Category | Rule or surface |
|---|---|
| Hardcoded secrets (key-shaped; `const $SECRET = "literal"` + metavariable-regex) | **Rule** — local, conclusive; the value is a literal. |
| Weak crypto (`md5($X)`, `sha1($X)`, `$TOKEN = Math.random()`) | **Rule** — weak regardless of argument. |
| `eval` / `new Function` with a non-literal argument | **Rule** — dynamic eval is conclusively dangerous. |
| SQL injection | **Split.** Concat/template **inline in the call** (`db.query(\`...${x}...\`)`) = rule. Assembled through an intermediate variable (`q = "..."+x; db.query(q)`) = surface (follow `q`). |
| XSS `dangerouslySetInnerHTML` | **Split.** `__html` built inline = rule. `__html={{__html: $X}}` with `$X` an arbitrary variable (sanitized?) = surface (the agent reasons whether `$X` is safe). |

Three categories are pure rules; two split between the local case (a rule, 1.2)
and the follow-the-data case (surface, 1.3).

### The slice-7 guardrail

> When writing a rule, if you find yourself wanting to express "and somewhere
> inside/after" (arbitrary intervening code) or "and this value comes from such a
> source" (follow a variable), that is the signal that the case is **not** a
> deterministic rule — it is surface. Leave it to 1.3; the rule covers only the
> syntactically local, conclusive part.

A rule that tries to follow data it cannot follow would be narrow and dishonest.
**A mutilated rule is worse than an absent one — it gives false confidence.**
Sending it to surface is the honest move.

## Consequences

- The slice-7 rules are written with this boundary explicit, not discovered
  mid-implementation.
- The two split categories (SQL, XSS) are declared in the coverage manifest in
  **both** the deterministic and the reasoning lists, in plain human prose, so a
  non-technical evaluator sees the division of labor, not a gap.
- `rules/README` states the bar for contributors: a rule that needs to follow the
  data is surface, not a rule.
