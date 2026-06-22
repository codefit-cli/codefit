# rules/

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
`pattern-inside`, metavariables (`$VAR`), `metavariable-regex`. **No ellipsis
(`...`)** and **no `mode: taint`** / `pattern-sources`/`sinks`/`sanitizers` —
that role is covered by the agent reasoning over mapped surface.

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
`path_criticality` (a finding in a test file is downgraded), so rules never
encode path context.
