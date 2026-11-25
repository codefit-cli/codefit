# 0086 — SEC-001 does not affirm a value that proves it is something else

Date: 2026-11-23
Status: accepted
Closes the VALUE half of the defect [ADR 0075](0075-sec-001-affirms-only-what-the-name-established.md)
closed for the name, for **Go**. TypeScript is a declared follow-up (issue `#148`).

## Context

ADR 0075 (Go) and [ADR 0085](0085-the-credential-vocabulary-is-shared-not-copied.md)
(TypeScript) made the NAME gate correct: an identifier is a credential only when
it carries a member of the shared `namematch` vocabulary as a name component.
Neither constrained the VALUE beyond "a non-empty string literal". So:

```go
const secretFile = "lib/config.ts"   // SEC-001, severity high, confidence 1.0
```

The name matched on the component `secret`. The value is a path to a file. This
is the same failure ADR 0075 was written about, through the other operand: an
**affirmation at confidence 1.0** — the certainty class the baseline refuses to
auto-silence (ADR 0011) — telling the reader something that is not true.

**The census, and what it decided.** Recorded on 2026-09-28 with the real sensor
(the name gate driven through the actual AST path, not a static probe), and kept
with the predicate in `internal/core/valueshape/doc.go`:

- **Go: 320** name-gate findings over the Go standard library (`net/http`,
  `crypto`, `database/sql`, `cmd/go`), `golang.org/x/oauth2`, `golang.org/x/sys`,
  the MCP Go SDK, `gotreesitter`, and codefit's own tree. Read by a human, by the
  shape of the value: URL 103, short test literal 65, URN 46, short identifier
  36, SCREAMING_SNAKE environment-variable name 35, hex/base64 blob 14, contains
  `/` 10, text with spaces 4, header name 2, placeholder 2, path with an
  extension 1, other 2.
- **TypeScript: 52** name-gate findings over three real projects (1,545 files;
  identities withheld, counts and forms only). URL, URN, environment-variable
  name and path with an extension: 0. Text with spaces: 2. The rest is short
  literals and identifiers, mostly under test paths.

Four of those buckets are values that say what they are. The others are values
that could be a secret.

Every figure dated 2026-09-28 in this document is the one recorded with the
predicate. This change re-measured only what its Consequences section states.

## Decision

### 1. One veto, in the core, applied by each provider

`internal/core/valueshape.NotACredential(value)` answers one question: does this
literal's own shape **prove** it is something other than a credential? It
returns the form that proved it, or nothing. It is a leaf — it imports only
`internal/core/namematch` and the standard library — so both providers can
consume it and neither reaches the other through it.

It is applied **per provider**, not in the sensor. Detection is per-language
(ADR 0083), and a sensor-level filter would have nothing to filter on: a
`findings.Finding` carries the identifier's name in its description and no
value. The provider is the last place that still holds the literal.

Go applies it in one function, `hardcodedSecret` (`internal/providers/golang/security.go`),
shared by the three AST shapes that reach SEC-001 — `*ast.AssignStmt`,
`*ast.ValueSpec`, `*ast.KeyValueExpr` — so the three cannot disagree. The order
is fixed: the name gate first, unchanged; the value veto only for a name that
already matched.

### 2. A veto, not a classifier

The predicate fires only on positive proof, and on nothing else returns "this
establishes nothing" — the name gate's affirmation stands exactly as before.
It never ranks, scores or suspects. Four forms:

| form | proves it when | keeps firing when |
| --- | --- | --- |
| `url` | a scheme and a host | it carries userinfo (`scheme://user:pass@…`); a credential-named query or fragment parameter has a value (`?api_key=…`); a host label, path segment, query key or value, or fragment key or value is token-like |
| `urn` | `urn:<namespace>:<string>` | — |
| `env-var-name` | SCREAMING_SNAKE with at least one underscore | there is no underscore (an access key ID, a base32 TOTP secret) |
| `file-path` | a narrow path charset and a trailing extension from a closed list — no separator required, so a bare `word.ext` qualifies | the trailing segment is not a known extension (a JWT, `secret.notanext`), or the value contains `+`, `=`, `:` |

A classifier would have to decide the ambiguous middle, and every wrong answer
there is a silenced credential. A veto does not enter the middle at all. When a
guard's own evidence is ambiguous — a query string that does not parse — it
resolves to **not** vetoing.

**A vetoed literal emits nothing.** Not a finding at a lower severity, not one at
a lower confidence, not a surface item. A demoted finding would still be a
statement about a credential, made about a value that has just proved it is not
one. What keeps firing keeps firing at confidence 1.0.

### 3. The predicate reads the value, not its source text

The Go provider hands the predicate the string the literal **denotes**
(`strconv.Unquote`), not the characters between its quotes. The two differ when
the source uses an escape, and the difference is not cosmetic: with an escaped
`=`, the source text of a URL carrying `api_key=…` shows a credential-named
parameter with no value, and would be vetoed. Pinned by a test in both
directions.

### 4. The prose guard was dropped before it existed

A fifth form — "text containing whitespace" — was proposed and removed on
2026-09-28, before any code implemented it. A passphrase is a secret with spaces
(`correct horse battery staple`), and the measured yield of vetoing prose was
**4 of 320** Go findings and **2 of 52** TypeScript findings. Six lines of noise
do not buy a class of silenced passphrases. Prose keeps firing, as declared
noise.

### 5. The token-like guard

A URL can clear every other guard and still be the secret. Five real shapes do
exactly that — two chat-webhook URLs, a presigned object-storage URL, a
shared-access-signature URL and a bot-token URL — because the secret sits in a
path segment, or in a query value whose key (`sig`, a signature header name) the
credential vocabulary was never meant to recognise. Measured against the
predicate before the guard existed, **all five were vetoed** as URLs.

So a URL is not vetoed when anything in it is **token-like**: at least 16 bytes,
with at least one ASCII letter **and** one ASCII digit. Both are required, and
each alone was measured too permissive: a long all-letter segment is a
descriptive endpoint name (`openid-configuration`), and a long all-digit segment
is a numeric id. Over the 320-value corpus the guard moved the vetoed count from
**167 to 165**; the two it returns to firing are occurrences of one OAuth
endpoint whose file name is itself token-shaped. That is accepted noise.

**Every position is read.** The guard first read only path segments (of the
decoded path), query values and `key=value` fragment values. An independent
verification found token-bearing URLs that passed through the positions it did
not read: a function URL whose first host label is the secret, a hash-route
invite link (`#/invite/<token>`), a bare token as the whole fragment, a calendar
feed whose token is a bare query key (`feed.ics?<token>`), and a token whose
path segment carries an encoded `%2F`, which split into short pieces once the
path was decoded. All of them fired before the veto existed. Asked whether to
inspect those positions or declare them, the decision was to inspect. So the
guard now reads:

- every **host label** of the host name — the port is not a label, and an IP
  literal is skipped whole, since an IPv6 address is long and mixes hex letters
  and digits;
- every **path segment**, split on the escaped path and decoded one segment at
  a time, so an encoded separator stays inside its segment;
- every **query key and value**. A bare key parses as a key with an empty
  value. Keys that carry a value are read too: nothing in the grammar keeps a
  token out of the key position, and the cost is a long parameter name, which
  is noise and not a silenced secret;
- every **fragment key and value**, read like the query. A fragment that is not
  a list of pairs reads as one valueless key and is inspected whole, which also
  covers each of its pieces: a token-like piece makes the whole token-like. The
  fragment is read from its escaped form, like the query, so it is split on its
  literal `&` and `=` before it is decoded: a recheck found that reading the
  already-decoded fragment turned an encoded `%26` or `%3D` into a real
  separator and cut the token (`#<10 bytes>%26<10 bytes>` was silent while the
  same token after `?` fired).

Measured with the real sensor over the same census corpus before and after this
widening: of the **100** URL values the veto silences there, **none** fires
again, and the SEC-001 count is unchanged in every tree. The same probe fires on
every token-bearing URL above, at all three AST shapes, so the zero is not a
blind measurement. It is a narrow one, and says so: none of those 100 values has
a query string, a fragment or a percent-escape, and their longest host label is
15 bytes. The corpus therefore measured the host-label position, one byte under
the threshold, and offers no evidence either way on the noise of the other three.

### 6. Go applies it now; TypeScript reaches it through an operator

TypeScript's SEC-001 is a YAML rule, and a rule can only express what the rule
engine's operators express. Copying the guards into a regex is not possible —
RE2 cannot parse a URL or consult a vocabulary — and copying them into a second
implementation is a second place to be wrong. TypeScript will reach the **same
function** through a rule-engine operator, the way `metavariable-name` reaches
`namematch` (ADR 0085), with cross-provider parity controlled by a value-axis
case table.

Until that lands the two providers **diverge**, and the divergence is declared
rather than hidden: `const secretFile = "lib/config.ts"` is silent in Go and
still fires in TypeScript (measured on this change: all four forms fire there at
confidence 1.0). It is written in `COVERAGE.md`, in `CHANGELOG.md` and in
`docs/roadmap.md`.

## What the veto does not see

Declared in code (`limitValueVeto`, cited by reference from the Go provider's
`Capability()` and rendered by `codefit-coverage` on SEC-001's own entry) and
locked by tests through the real provider.

**Not reported, although it may be a secret** — the cost of the veto:

- an all-caps value containing an underscore: it has the shape of an
  environment-variable name;
- a bare file name with a known extension and no separator (`Hunter2024.db`): a
  secret that ends in a dot plus a listed extension has the shape of a file
  name;
- a URL whose embedded token is shorter than 16 bytes, lacks either a letter
  or a digit, or is cut by the URL's own literal separators (dots between host
  labels, slashes between path segments, `&` or `=` in a query or fragment)
  into pieces each shorter than 16 bytes: the token-like guard has no shape to
  act on. Only an encoded separator inside one piece is undone, in every
  position: each is split on its literal separators first and each piece is
  decoded once. A token whose decoded form has no digit stays
  vetoed even when its encoded form shows hex digits (`%2F`): those digits are
  the encoding, not the content.

**Still reported, although it is often not a secret** — the noise the veto
leaves, because the shape cannot be told apart from a secret's:

- short test literals and short identifiers. `signingKey = "s3cr3t"` must fire: a
  credential has no length floor (ADR 0075);
- prose (section 4);
- a path with no known extension — the form anchors on a closed list and never
  guesses from structure. A bare `/` proves nothing: AWS's published example
  secret access key contains one;
- header names, and placeholders that are not SCREAMING_SNAKE with an
  underscore (`changeme`, `<token>`, `TODO`, `__SECRET__`).

Hex and base64 blobs, JWTs and STS-style tokens also keep firing. They are not
noise: they are what the rule is for.

## Consequences

- **Fires that stop (Go).** A credential-named identifier bound to a URL, a URN,
  an environment-variable name or a file path with a known extension. Measured
  with the pre-change and the post-change sensor over the same trees: codefit's
  own tree goes from **24 to 23** SEC-001, the one site being the reproduction
  above (counted before the planted self-audit site below was added; it fires
  under both sensors, so with it the pair reads 25 and 24); a planted file with one case per constraint and one per form goes from
  **21 to 9** — the 12 that stop are the four forms at each of the three AST
  shapes, and every constraint case keeps firing at confidence 1.0.
- **Fires that start.** None.
- **Baseline.** An entry recorded for a finding that stops is no longer observed
  and becomes `gone`; `codefit-baseline-prune` covers it. Nothing that keeps
  firing changes.
- **The self-audit lost its last natural layer-2 site**, and with it the evidence
  its vacuum guard relied on to know the name gate was running. One site is now
  planted for that purpose — `signingKey = "s3cr3t"` in the self-audit test
  itself — and pinned in the census, so ADR 0075's constraint is also exercised
  through the whole sensor over the real tree.
- **Go and TypeScript diverge on the value axis** until the follow-up lands.

## Alternatives rejected

- **Demote instead of veto** (lower severity or confidence, or a surface item).
  The value proved it is not a credential; any finding about it is still the
  false statement, quieter.
- **A length floor.** ADR 0075 removed one because it was anti-correlated with
  the property it claimed to filter. `s3cr3t` is six bytes.
- **Veto anything containing `/`, or anything path-like without an extension.**
  The census's "contains `/`" bucket holds ten values, and one of them is the
  example secret key a cloud vendor publishes to show what a real one looks like.
- **Veto prose.** Section 4.
- **Infer "test literal" from the name or the path.** Path criticality already
  re-weights findings on test paths (RF-10); the shape of the value adds nothing
  the path does not already say, and off a test path it is a guess.
- **Apply the veto in the sensor.** A finding does not carry the value.
- **A second implementation for TypeScript.** Section 6.

## Notes

The name gate, its vocabulary and layer 1 (the regex over raw text for known
API-key formats) are untouched. Layer 1 never looks at a name, and no value
shape vetoes it.

2026-11-24: TypeScript now reaches this predicate through the rule-engine operator `metavariable-value-veto`, and the divergence §6 declared is closed — see [ADR 0087](0087-typescript-reaches-the-value-veto-through-a-rule-operator.md).

2026-11-25: The Go provider no longer serves a hand-written copy of this ADR's limits. Its `SEC-001` value-veto limit (`limitValueVeto`) is now one Go-specific sentence followed by `valueshape.DeclaredLimit`, the text the TypeScript manifest already cited, so both providers serve one text. That text replaces the wording of "Still reported" above on one point: a placeholder keeps firing when it does not match the environment-variable-name pattern (an upper-case letter first, at least one underscore, none leading, trailing or doubled). "Not SCREAMING_SNAKE with an underscore" did not explain `__SECRET__`, which has underscores but only leading and trailing ones. All four placeholders (`changeme`, `<token>`, `TODO`, `__SECRET__`) are now pinned through the real provider in both languages.

2026-11-25: §3 now holds for the name gate too. The gate's `stringLit` trimmed quote and backtick characters from the source text while the veto decoded with `strconv.Unquote`, so the provider read one literal two ways: a literal made only of quote characters (`` `""` ``, or an interpreted literal holding only backticks) read as empty at the gate and emitted nothing. `stringLit` now decodes, once, and the veto reads the value it returns; the veto's input did not change. Those literals now fire. Measured over every Go file of this repository plus a 4,361-file generated corpus: 180 findings added, all quote-only literals under a credential name, none removed.

2026-11-25: "Not reported" above names "an all-caps value containing an underscore". That is wider than what the environment-variable-name form vetoes, and it contradicts "Still reported": `__SECRET__`, `A__B` and `ABC_DEF_` are all-caps with underscores, and all three fire. What is not reported is a value that matches the environment-variable-name pattern (an ASCII upper-case letter first, then ASCII upper-case letters and digits, at least one underscore, none leading, trailing or doubled), whatever it holds. `valueshape.DeclaredLimit`, the text both providers serve, now says so, and `internal/core/valueshape`'s own tests pin each of those malformed names, plus `_SECRET`, `<token>`, `changeme` and `TODO`, as keeps-firing at the predicate.
