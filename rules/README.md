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

Supported operators (core subset): `pattern`, `pattern-either`, `patterns`,
`pattern-not`, `pattern-inside`, metavariables (`$VAR`), `metavariable-regex`.
Not supported: `mode: taint`, `pattern-sources`/`pattern-sinks`/
`pattern-sanitizers` — that role is covered by the agent reasoning over mapped
surface.

> **Status: skeleton.** The rule loader and matcher are implemented in Fase 1.
> No rule files live here yet; this directory documents the format and reserves
> the layout.
