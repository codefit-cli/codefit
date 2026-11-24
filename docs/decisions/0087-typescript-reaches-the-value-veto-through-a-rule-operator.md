# 0087 — TypeScript reaches the value veto through a rule operator

Date: 2026-11-24
Status: accepted
Closes the TypeScript half of the value defect
[ADR 0086](0086-sec-001-does-not-affirm-a-value-that-proves-it-is-something-else.md)
closed for Go (issue `#148`). Adds one operator to the subset declared in
`rules/README.md`, and makes the rule loader strict.

## Context

ADR 0086 made Go's `SEC-001` stay silent when a literal's own shape proves it is
a URL, a URN, an environment-variable name or a file path, through one core
predicate, `valueshape.NotACredential`. TypeScript's `SEC-001` is a YAML rule,
and a rule can only say what the engine's operators say. Until this change it
said "a credential-named binding holding a quoted literal", so

```ts
const secretFile = "lib/config.ts";   // SEC-001, severity high, confidence 1.0
```

still fired in TypeScript while Go was silent on the same value. ADR 0086 §6
declared that divergence and named the way out: reach the **same function**
through an operator, the way `metavariable-name` reaches `namematch`
([ADR 0085](0085-the-credential-vocabulary-is-shared-not-copied.md)). A regex
cannot do it (RE2 cannot parse a URL or consult a vocabulary), and a second
implementation would be a second place to be wrong.

## Decision

### 1. `metavariable-value-veto`

```yaml
metavariable-value-veto:
  $VALUE: not-a-credential
```

The match is **dropped** when the source text bound to the metavariable is one
JavaScript string literal and the named predicate rejects its decoded value.
`not-a-credential` is `valueshape.NotACredential`. In every other case the match
is kept, unchanged, at confidence 1.0.

The predicates are a **closed registry** (`valueVetoes` in
`internal/core/ruleengine/valueveto.go`), like `metavariable-name`'s
vocabularies. The core stays provider-free: `ruleengine` imports
`internal/core/valueshape`, which imports only `namematch` and the standard
library.

### 2. The literal is decoded, strictly, before the predicate reads it

A bound value is source text, quotes included. The predicate must read what the
literal **denotes**, for the reason ADR 0086 §3 gives for Go:
`"…?api_key\x3dabc"` denotes a URL carrying `api_key=abc`, and the characters
between its quotes show a parameter with no value, which would be vetoed.

The decoder (`decodeJSStringLiteral`, unexported) accepts text that starts with
`'` or `"` and whose first unescaped matching quote is its last byte. It decodes
`\n \r \t \b \f \v`, `\0` not followed by a digit, `\xHH`, `\uHHHH` (a high
surrogate only when a low one follows), `\u{…}` up to U+10FFFF, line
continuations, and identity escapes (`\/`, `\q`). It **refuses** legacy octal
(`\00`, `\1`–`\7`), `\8` and `\9`, a lone surrogate, a malformed `\x` or `\u`, a
raw line break, and anything that is not one literal — a template, a
concatenation, an unterminated or mismatched quote.

**A refusal proves nothing, and the match is kept.** Every failure falls toward
"keeps firing", the direction ADR 0086 chose for ambiguous evidence.

### 3. It runs at bind time

The veto is a third arm of the metavariable constraint that already carries the
regex and the name vocabulary, so it is consulted while a binding is made and
steers the object-subset search. In
`{secretFile: "lib/config.ts", apiKey: "s3cr3t"}` the first pair is rejected while
it is being bound, and the search goes on to `apiKey`. A filter applied after the
match would have bound the first pair, dropped the whole object and reported
nothing: a false all-clear. Pinned by a test that fails under exactly that
mutation.

### 4. Every way it could do nothing is a compile error

- an unknown predicate name;
- a key that is not a metavariable (`VALUE`, `$value`);
- a metavariable that some base alternative never binds, the `...$REST` ellipsis
  marker included. The engine treats a constrained metavariable that was never
  bound as a rejection, so such a key would make that alternative — or the whole
  rule — silent forever.

### 5. The rule loader rejects unknown keys

`LoadFS` decoded with plain `yaml.Unmarshal`, which skips keys the `Rule` type
does not declare. A misspelled `metavariable-valu-veto:` would therefore have
loaded cleanly and the rule would have silently lost its veto. The loader now
decodes with unknown keys rejected, at every level, and the error names the file,
the line and the key. An empty file still loads as no rules. Before the switch,
every shipped rule file was loaded on its own under strict decoding; none relied
on an unknown key.

### 6. One served copy of the limits

What the veto does and does not see is served to agents by `codefit-coverage`.
The TypeScript manifest (`ts.hardcoded-secrets`) states how TypeScript reaches
the veto and then **cites** `valueshape.DeclaredLimit` instead of retyping it.
The constant names no provider. Go still serves its own copy
(`limitValueVeto`); it is left untouched by this change. The two texts differ in
one place: the shared text says why `__SECRET__` keeps firing (the
environment-variable-name pattern needs an upper-case letter first and allows no
leading, trailing or doubled underscore), where Go's text says "not SCREAMING_SNAKE
with an underscore".

## What the veto does not see in TypeScript

The veto's own limits are ADR 0086's, unchanged. What is TypeScript's:

- **Shapes `SEC-001` does not reach are neither vetoed nor reported**: a class
  field, a template literal, a parenthesized or `as`-cast value, a bare
  assignment, a destructuring default. This change did not widen the rule's
  shapes. Each is pinned as a negative fixture.
- **A concatenation of literals reaches the veto and keeps firing**: `$VALUE`
  binds `"a" + "b"`, which the quote regex admits, and the decoder refuses it.
- **An escape the decoder refuses keeps firing**, whatever the value would have
  decoded to.

## Measured

With the pre-change provider (`main` at `0597adc`) and the post-change one, over
the same inputs:

- a planted file with the four vetoed forms and `"s3cr3t"` at each of the six
  shapes (`const`, `let`, `var`, typed `const`, typed `let`, object property):
  **30 → 6** `SEC-001`. The 24 that stop are the four forms at the six shapes;
  the six controls keep firing at confidence 1.0;
- the in-repo TypeScript testdata (26 files): **0 → 0** `SEC-001`, and no finding
  of any other rule moved;
- the census of the three foreign projects ADR 0086 measured was **not re-run**:
  the procedure that produced their copies is not recorded, so there is nothing
  to re-run it from. Pending.

## Left for the value-axis parity table (`#148`, next task)

The two providers now ask the same function, but they do not hand it the same
thing in every case. The parity table must enumerate these, each with a verdict
in both columns:

- **The TypeScript sites**: the six alternatives above. Go's are the three AST
  shapes in ADR 0086.
- **Escape grammars.** `\/`, `\u{…}` and line continuations exist only in
  JavaScript. `\a`, `\U00…` and octal `\NNN` exist only in Go — and Go's octal
  decodes where JavaScript's legacy octal is refused, so the same digits veto in
  Go and keep firing in TypeScript. `\xHH` is a byte in Go and a code point in
  JavaScript, so a value with `\x80`–`\xff` decodes to different strings.
- **Single quotes.** A single-quoted JavaScript literal is a string and is
  vetoed; in Go it is a rune and never reaches `SEC-001`.
- **Raw and template literals.** A Go raw string reaches the veto; a JavaScript
  template never reaches `SEC-001`.
- **Concatenation.** Go's `SEC-001` accepts only a basic literal, so a
  concatenation is silent there; in TypeScript it reaches the rule and keeps
  firing.

## Alternatives rejected

- **Filter after the match.** Section 3: it reports nothing for an object whose
  first credential-named pair is vetoed.
- **Trim the quotes instead of decoding.** Section 2: it vetoes a URL whose
  credential parameter is written with an escape.
- **A lenient decoder that guesses at malformed escapes.** A guess that decodes
  to a vetoed form silences the match; a refusal only costs a line of noise.
- **Keep the lenient loader and lint the rule files instead.** The loader is the
  only code that runs on every rule file in every build; a separate lint is a
  second place to forget.
