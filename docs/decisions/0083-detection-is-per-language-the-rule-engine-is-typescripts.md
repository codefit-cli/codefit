# 0083 — Detection is per-language; the rule engine is TypeScript's

**Status:** accepted · **Date:** 2026-09-03 · **Decided by the architect** after five
language simulations, before the third language rather than during it.
· **Corrects** the PRD §17/§1067 portability claim (see D4).
· **Ends a drift**: Go shipping with zero YAML rules was never decided — it was
expedient. This ADR makes the state a choice.

## Context

codefit has two detection mechanisms in production for the deterministic layer:

- **TypeScript**: declarative YAML rules (a Semgrep-format subset) matched by
  `internal/core/ruleengine` over the neutral AST (`internal/core/syntax`).
- **Go**: hand-written `go/ast` analysis. Zero YAML rules.

A proposal was on the table to unify them: make rules the default for every
language, demand the neutral AST from every provider, treat hand-written code as
a declared exception admissible only when a check needs symbol/import/type
resolution, and add a container-scoped ellipsis to the matcher.

The architect asked the right question before accepting it: **does this hold for
the next five languages?** Five independent simulations — Java, Python, C#, Ruby,
Rust — stress-tested the proposal against the real matcher, the real vendored
grammars, and each language's real security checks. The verdict was unanimous:
**BREAKS**, for convergent reasons.

## The evidence, verified independently of the simulations

**The pattern language is TypeScript, and the code says so.**
`internal/core/ruleengine/matcher.go` documents its own premise: *"In TS/JS a
metavariable parses as an ordinary identifier whose text is `$X`."* That is a
TypeScript lexical fact, and it fails differently in every language tested:

| language | what happens to `$X` |
|---|---|
| C# | the lexer consumes the `$`; **measured with the real engine**: every metavariable rule returns zero findings, silently, `HasError=false` |
| Python | same silent deletion; `yaml.load($X)` misses `yaml.load(f)` and HITS a literal `yaml.load(X)` |
| Rust | `$` is not legal in identifiers — patterns do not parse |
| Ruby | `$UPPERCASE` is the language's own global-variable namespace (`$SAFE`, `$LOAD_PATH`), so the rule "flag `$SAFE = 0`" is inexpressible |
| Java | patterns are not standalone compilation units — a field pattern needs a wrapping class, which changes what it matches |

**The core hardcodes TypeScript node types.** `isTrivialWrapper` in
`internal/core/ruleengine/engine.go` accepts `program`, `expression_statement`,
`parenthesized_expression`. Rust roots at `source_file`, C# at
`compilation_unit`, Python at `module` — so every rule for those languages
compiles against the file root and matches nothing, silently. Making it work
means editing the core per language, which is the exact condition
`docs/PRD-codefit-v1.4.md:1264` declares a design failure: *"si agregar Java
requiere tocar el núcleo, el diseño falló."*

**The exception criterion fails in both directions.** Scored against each
language's canonical checks: in Rust the exception is the majority (4–5 of 8
need symbol/type resolution); in Ruby it is zero of 8 — not because rules
suffice but because Ruby has no static import graph or types to resolve, making
the criterion vacuous. Meanwhile the code Ruby genuinely needs by hand —
walking a heredoc's *sibling* body, reading a `Gemfile.lock` version — is
syntactic and manifest work the criterion does not name.

**The parser was never the blocker.** All five grammars (and 200 more) are
already vendored and already ship inside the binary via the embedded grammar
set. The blocker is entirely the matcher's design.

**The economics, measured four times independently:** the TypeScript provider is
137 lines of YAML against 3,575 lines of Go, of which **2,808 lines (79%) are
framework-aware surface mapping** — which is hand-written by necessity and which
no rule engine touches. The proposal optimized ~4% of the per-language cost and
priced a matcher rewrite, a core edit and a breaking provider-contract change
against it.

## Decision

### D1 — The rule engine is TypeScript's implementation detail

`internal/core/ruleengine` and `rules/typescript/` remain exactly what they are:
the mechanism the TypeScript provider chose. They are not the cross-language
detection architecture, and no future language is expected to use them. The
engine keeps being improved **for TypeScript's benefit** (the compile gate, the
operator skeleton, and a possible ellipsis are all worth it there), with no
obligation to generalize.

### D2 — Each language owns its detection mechanism

What Go did by expedience is now the rule: a `LanguageProvider` owns how it
detects, the way it already owns its parser (ADR 0002's precedent). The
contract stays `AnalyzeSecurity(src) → findings` — a black box. A provider that
wants declarative rules may build or reuse a matcher; one that wants direct AST
analysis writes it. Neither needs permission and neither touches the core.

### D3 — What IS shared, because it is what actually generalizes

Three assets are cross-language today, work, and are the investment target:

1. **The neutral models** — `findings`, `surface`, the baseline, scoring. Every
   provider's output flows through them; they are why `scan-all`, the buckets,
   the score and the closing protocol needed zero changes per language.
2. **The vocabulary** — `internal/core/namematch` (what counts as a credential
   name, component matching, plural folding), bound across engines by the
   cross-provider case table where every divergence must be written down.
3. **The census discipline** — `TestShapeCensus` and the CLAUDE.md rule behind
   it: before a detector ships, measure which real-world shapes it reaches and
   declare the ones it does not, each silence with a written reason.

A new language must plug into all three. It brings its own detector.

### D4 — The PRD's portability claim is corrected, not reinterpreted

PRD §1067 argues rules port across languages because *"tree-sitter usa el mismo
formato de queries para todos los lenguajes."* codefit does not use tree-sitter's
query language — it parses patterns as source and compares structurally, which is
precisely what does not port. The claim described a design that was not the one
shipped. The PRD stays as designed scope (it is exempt from reflect-today), but
this ADR is the standing correction, and the extensibility recipe in §1032
("las reglas determinísticas en formato Semgrep" as a step of adding a language)
is superseded by D2.

### D5 — What reopening this would take, so it costs a read and not a rediscovery

The simulations converged on the narrower restatement that COULD work, recorded
here deliberately: per-language metavariable spelling and trivial-wrapper
vocabulary owned by the provider; the `patterns:` AND operator actually
implemented (declared today, consumed by nothing); set semantics for
order-insensitive containers (C# object initializers, attribute lists); opt-in
container ellipsis; path-prefix normalization (Rust); and a context object so a
rule can consult an import map. That is a materially different and more
expensive engine than the one that exists — and it would still leave the 79%
untouched. Anyone proposing to reopen starts from this list.

## Consequences

- **The third language arrives with its eyes open**: its cost is the surface
  mapping plus its own detector, and the shared assets in D3 are ready for it.
  The parser is already in the binary.
- The cross-provider case table stays load-bearing indefinitely — it is the
  instrument that keeps N detectors honest about one vocabulary.
- `rules/README.md` and `internal/core/ruleengine/doc.go` describe a TypeScript
  mechanism and should say so on their next touch; this ADR is the authority
  meanwhile.
- Two engine defects found by the simulations were fixed independently of this
  decision (#173: a pattern that does not parse is now rejected at compile time;
  #174: the matcher now sees operators), because they bite TypeScript today.

## Alternatives considered

**Unify on the rule engine** (the original proposal). Rejected on the evidence
above: five simulations, unanimous, with the falsifying measurements reproduced
first-hand rather than trusted.

**Unify on hand-written analysis and delete the rule engine.** Rejected:
TypeScript's rules work, are cheap to review, and the census now pins their
reach. Deleting a working mechanism to achieve symmetry is symmetry worship.

**Decide nothing.** Rejected by the architect explicitly: *"no quiero volver
sobre esto en el tercer lenguaje."* An undecided drift would have been re-argued
at the worst possible moment — mid-language.
