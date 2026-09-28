# rules/

> **Scope (ADR 0083):** this rule format and its matcher are the **TypeScript
> provider's** detection mechanism, not codefit's cross-language architecture.
> Each language owns its detector; Go, for example, uses hand-written `go/ast`
> analysis and has no rule files.

Declarative detection rules in a **subset of the Semgrep rule format** (see PRD
section 17). These are codefit's deterministic security/best-practice rules:
versioned with the binary, contributed by the community without writing Go, and
matched by codefit's own pure-Go matcher (`internal/core/ruleengine`) over the
provider's AST — the OCaml Semgrep/OpenGrep engine is **not** embedded.

Layout (per language):

```
rules/
  go/          # rules for the Go provider
  typescript/  # rules for the TypeScript provider (Fase 1)
  ...
```

Supported operators (core subset): `pattern`, `pattern-either`, `pattern-not`,
`pattern-inside`, metavariables (`$VAR`), `metavariable-regex`,
`metavariable-name`. **No `mode:
taint`** / `pattern-sources`/`sinks`/`sanitizers` — that role is covered by the
agent reasoning over mapped surface.

### `metavariable-name`: match an identifier by name COMPONENT

Not a Semgrep operator — codefit's own, and it exists because a regex cannot do
the job.

```yaml
metavariable-name:
  $NAME: credential
```

`$NAME` matches only if the identifier carries a member of the named vocabulary
as a **name component**: `accessToken` and `API_KEY` fire, `tokenizer` and
`subtokenizer` do not.

**Why not a regex.** SEC-001 used an unanchored `metavariable-regex` for `$NAME`,
so any identifier merely *containing* a credential word was affirmed at certainty
1.0 — `const tokenizer = "whitespace"` was reported as a hardcoded secret. Fixing
it needs the word anchored to a component boundary, and Go's regexp is **RE2: no
lookbehind, no lookahead**. Asserting what precedes `token` is exactly what RE2
cannot express, and enumerating the case variants multiplies every alternative by
every boundary.

**The vocabularies are a closed set** — today only `credential`, which is
`namematch.Credential()`, the same words Go's name gate consumes. An unknown
vocabulary is a **compile error**, because a rule that named one and matched
nothing would tell nobody. Adding a vocabulary means adding it to
`metavarVocabularies` in `internal/core/ruleengine/engine.go`.

**It composes.** A metavariable may carry a name constraint and a regex at once;
both must hold. And like `metavariable-regex`, it is consulted at bind time, so
it steers the object-subset search rather than filtering its first answer.

### The one ellipsis: object properties, spelled `...$REST`

Semgrep's general ellipsis is still **not** supported. Objects are the single
exception, because without it an object pattern was unusable in practice: the
matcher compares the property **count**, so `({apiKey: $V})` reached only an
object with exactly one property. A census across two real TypeScript projects
measured every object holding a credential-named string property at arity **5,
5, 5 and 3** — four of four multi-property, none at arity 1.

```yaml
- "({...$REST, $NAME: $VALUE})"   # any position, object of any size
```

**It is spelled `...$REST`, not Semgrep's `...`, and that was measured rather
than chosen.** Since the compile gate, a pattern whose parse tree contains an
ERROR node is rejected — and the TypeScript parser cannot parse a bare ellipsis
inside an object literal:

| written as | parses |
| --- | --- |
| `{..., $NAME: $VALUE}` | **HasError** → rejected at compile |
| `{$NAME: $VALUE, ...}` | **HasError** → rejected at compile |
| `{...$REST, $NAME: $VALUE}` | clean → `object[spread_element, pair]` |

A spread of a metavariable is ordinary TypeScript, so it survives the gate.

Three things to know when you use it:

- **It is zero-or-more**, so one alternative covers `{apiKey: "x"}` and a
  credential buried in a five-property config object. Never write a separate
  single-pair alternative.
- **The marker does not bind.** `$REST` is punctuation; a `metavariable-regex`
  naming it constrains nothing.
- **It is opt-in per pattern.** A pattern without a spread keeps exact-arity
  matching, unchanged. That is what makes it safe: adding it changed the
  behaviour of no existing rule.

Full semantics and the reasoning are in `[objectsubset]` in
`internal/core/ruleengine/matcher.go`.

## When is something a rule, and when is it surface? (read before adding a rule)

A deterministic rule and mapped surface draw a hard line (ADR 0004):

- A **rule** matches a dangerous pattern that is **conclusive over a local
  subtree**, with metavariables filling the holes of single nodes. `md5($X)` is a
  rule: it matches `md5(password)`, `md5(someVar)`, `md5(anything)` — the
  metavariable generalizes the argument, and md5 is weak regardless of it.
- **Surface** is anything that, to conclude, must **follow the data** through
  variables or intervening code (taint). `const q = "..." + x; db.query(q)` is
  surface — the rule cannot follow `q` back to its construction.

**The guardrail:** if, while writing a rule, you find yourself wanting to express
"and somewhere inside/after" (arbitrary intervening code) or "and this value
comes from such a source" (follow a variable), that is the signal that the case
is **surface, not a rule**. A rule that tries to follow data it cannot follow is
narrow and dishonest — a mutilated rule is worse than an absent one, because it
gives false confidence. Send it to surface.

A category can split: SQL injection built inline in the call is a rule; SQL
assembled through an intermediate variable is surface. Both halves are declared
in the coverage manifest.

## Rule fields

```yaml
rules:
  - id: SEC-052
    message: "MD5/SHA-1 is weak for security"
    suggestion: "Use SHA-256+ for integrity, bcrypt/argon2 for passwords"
    severity: medium          # critical | high | medium | low | info
    dimension: security
    pattern-either:           # one of: pattern | pattern-either
      - "md5($X)"
      - "sha1($X)"
    # optional conditions: pattern-not, pattern-inside, metavariable-regex
```

Severity is the rule's *natural* severity; the sensor adjusts it by
`path_criticality` (a finding in a test file is re-weighted by
`sensors.security.test_severity` — forced to `info` by default, RF-10), so rules
never encode path context.

## A pattern that does not parse is rejected, not accepted

Every pattern string is parsed with the provider's own parser at compile time,
and **a pattern whose parsed tree contains a syntax error fails the whole
compile**, naming the rule id, the operator, and the offending pattern text.

This is worth stating because the failure it prevents is invisible. tree-sitter
is error-*recovering*: given `"eval($X"` it does not fail — it returns a tree
containing an ERROR node. Without the check the rule compiles cleanly and then
matches **nothing**, for the life of the process, with no error anywhere. A
silent rule is a silent vulnerability, so a typo in a pattern is loud instead.

Practical consequence for rule authors: if a rule fails to compile, the pattern
is not valid code in that language. Wrap a fragment in whatever syntax makes it
a complete expression — the way the XSS rules write an object literal as
`"({__html: $Q})"`, since the parentheses are semantically transparent and get
peeled off before matching.
