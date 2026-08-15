# 0075 — SEC-001 affirms only what the name established

Date: 2026-08-14
Status: accepted

## Context

SEC-001 (Go) decided "this value is a hardcoded credential" from the variable's
name, using two arms:

```go
for _, kw := range []string{"secret", "passwd", "password", "token", "apikey"} {
    if strings.Contains(l, kw) { return true }      // Arm A: raw substring
}
return strings.Contains(l, "key") && valueLen >= 16 // Arm B: bare "key" + length
```

An AST census over codefit's own tree — the security sensor driven through the
real `go/ast` parser, because no static probe can enumerate `*ast.KeyValueExpr`
or a multi-name `*ast.ValueSpec` — found 27 SEC-001 findings, from two
detectors sharing the id. Five came from this name gate. **Four of those five
came from Arm B, and all four were false.** They were enum constants and
descriptive names: a category constant, a signal-name constant, and two test
fixtures. Each was reported at `Confidence: 1.0` with the message "looks like a
hardcoded credential".

Two further facts shaped the decision, and both are measurements rather than
arguments.

**Arm B's length guard is inverted.** Its comment says the length "limits false
positives". Both production false positives are past 16 bytes *because
descriptive kebab/snake values grow with descriptiveness*, while a real
credential has no length floor at all. The gate admits the false-positive class
and rejects a true-positive one. Measured directly: `accessKey :=
"AKIAIOSFODNN7"` (13 bytes), `SIGNING_KEY = "s3cr3t"` (6 bytes) and
`encryptionKey = "ek_abc"` (6 bytes) were all REJECTED by the arm that accepted
a 29-byte category name.

**Arm B was load-bearing, so deleting it alone would have been a regression.**
Arm A substring-matches `apikey`, and `lower("API_KEY")` is `"api_key"`, which
does not contain `apikey`. Measured over six real credential spellings, five
fired ONLY through Arm B:

| name | Arm A | Arm B |
|---|---|---|
| `apiKey` | fires | — |
| `API_KEY`, `api_key`, `accessKey`, `SIGNING_KEY`, `privateKey` | — | fires |

## Decision

**SEC-001 decides by name COMPONENT, never by raw substring and never by value
length.** The matcher moves to `internal/core/namematch`, a stdlib-only leaf,
and the adjacent-pair join (`api`+`key` → `apikey`) is what makes Arm B's
deletion safe rather than a silent false negative.

ADR 0056 §1 allows exactly two responses to a check that cannot support its
claim: teach it to check, or drop it. It forbids a third — declaring the false
fact as a limit. Arm B's fact is FALSE, not merely uncertain, so it is dropped.
This is the line that separates it from md5: md5's declared fact (*this is an
md5 call*) is true and only its relevance is uncertain, which is why declaring
md5's limit is honest and declaring Arm B's would have been a lie.

### One matcher, three vocabularies

```
credentialShared (9) + piiShared (6)          = DB053Union()  -- frozen
credentialShared (9) + securityOnlyTokens (3) = Credential()  -- free to grow
securityValueTokens (8)                       = SecurityValue()
```

The three-set split is load-bearing. With a two-set split (credential as a
subset of the union), every SEC-001 widening would silently become a DB-053
change — and DB-053 is measured across 29 corpora in ADR 0047 that are no
longer cloned. Here DB-053's vocabulary is provably byte-identical while
SEC-001's moves.

`securityOnlyTokens = {accesskey, signingkey, encryptionkey}` are
**restorations of measured current behaviour, not speculative widening**: Arm B
affirms all three today at Confidence 1.0, and TypeScript's `$NAME` already
carries `accesskey`. Refusing them would have traded one false affirmation for
three silent misses. `publickey` is deliberately excluded, with its reason: a
public key is not a credential.

SEC-050 joins the matching CONVENTION and keeps its OWN vocabulary. Crypto
material is not a credential. Its bare `key` component is admissible only
because SEC-050 additionally requires a `math/rand` call — a second condition
SEC-001 has no equivalent of. It was strictly looser than SEC-001 before this:
`monkeyIndex := rand.Intn(n)` fired.

### The plural fold, and why it is an ADD and not a STRIP

`Credential()` folds the regular `+s` plural of every entry. `DB053Union()` and
`SecurityValue()` do not: the fold answers a SEC-001 narrowing, and letting it
reach DB-053 would move a frozen vocabulary as a side effect of credential work
— the coupling the three-set split exists to prevent.

It is here because substring matching had been carrying the plurals for free
(`"passwords"` contains `"password"`) and component matching turns each plural
into ONE component outside the set. Measured on the real analyser over both
trees, nine spellings went silent — `passwords`, `secrets`, `tokens`, `apiKeys`,
`apikeys`, `privateKeys`, `refreshTokens`, `mySecrets`, `userPasswords` — eight
of them at ANY value length, so this was a loss from replacing Arm A and was
orthogonal to the Arm B deletion. Six carry no `key` at all and five carry a
camelCase boundary, so the all-lowercase-concatenation limit never described
them: it was an UNDECLARED narrowing, and `var passwords = []string{…}` is
ordinary Go.

Adding the suffix and stripping it are equivalent in effect — a component
matches iff it is a member or a member plus `s` — but only the ADD is
ENUMERABLE. The folded set is a value a test prints and pins, and the
cross-provider table's no-silent-widening control forces every folded token to
carry a declared TypeScript verdict. A strip is a predicate: it admits an open
set and nothing can enumerate what it just accepted. The fold widens the
SPELLINGS of the vocabulary, never the vocabulary — `publickey` is refused, so
`publicKeys` is refused too. No `-es`/`-ies` rule: no entry ends in s, x, z, ch,
sh or consonant+y, so it would add reach and no coverage.

The false-positive side is measured, not argued: the AST census over codefit's
own tree adds ZERO sites under the fold (positive control: an injected
`probeUserPasswords` string is reported as unpinned), and across the 53-spelling
differential nothing fires that did not fire before the change except `pwd` and
its plural `pwds`, already declared.

### The declared limit has a source in code

Component matching cannot split an all-lowercase concatenation: `secretkey` is
one component, because there is no boundary in it to find. That gap is real and
is DECLARED, because under-detection may be declared and a false affirmation
may not.

`namematch.LimitLowercaseConcatenation` sits beside the tokenizer that causes
it. `providers.RuleSet` gains `Limits []RuleLimit` with `ValidLimits()`, the
INVERSE of `ValidExclusions()`: an excluded id must NOT be in `Declared`
(it names a rule that will never exist), a limit id MUST be
(it qualifies one that does). `golang/capability.go` cites the const **by
reference, never by copy** — a compile-time dependency cannot drift.

`deriveManifest` appends the limit to that rule's OWN `Deterministic` line
rather than to a sibling array, so an agent cannot read "SEC-001 declared"
without the caveat: they are one string, and no summariser between codefit and
the model can keep the claim while dropping the qualification.

### Cross-provider divergence is a control, not a comment

The core cannot import a provider, and TypeScript's vocabulary is an RE2
metavariable regex in YAML. So the two vocabularies are BOUND by a case table
that loads TS's `$NAME` from the real embedded YAML — never a copy, which would
be a fifth vocabulary whose drift would be invisible. Four controls: verdict
fidelity, agreement-or-reason, no silent widening, no ghost reasons. The
table's scope is the NAME GATE only; TS additionally requires
`const $NAME = $VALUE` while Go fires on assign/valuespec/keyvalue.

## Consequences

**Fires that STOP**, in two groups with different causes. *Only at a 16+ byte
value* — names carrying `key` as a substring or as a non-credential component,
which never fired any other way: enum constants, category names, `keyboard`,
`keyword`, `monkeyId`, `textKey`, `publicKey`, `turnkey`, `donkey`,
`sessionKey`. *At any value length* — a credential word inside a longer
lowercase run: `tokenizer`, and the concatenations `secretkey`, `dbpassword`,
`mypassword`, `authtoken`, `clientsecret`. On codefit's own tree this is 4
findings going to 0, with no other census entry changing. Plural spellings are
NOT in either group; see the plural fold above.

**Fires that START.** `pwd` and `pwds`, `accessKey`, `SIGNING_KEY`,
`encryptionKey` and their plurals, and any credential name whose value is under
16 bytes.

Both directions are user-visible and both are in `CHANGELOG.md`. Baseline
entries for findings that stop are stale, not wrong, and
`codefit-baseline-prune` already covers them; findings that start are new.
`Fingerprint(dim/id, path, line)` is unchanged for anything that keeps firing.

**The self-audit gained the direction it lacked.** `TestSelfAudit` asserted only
`len(Findings) != 0` and `!IsBlocked` — it printed no findings and asserted no
count, so deleting a detector left it green. It is now backed by a pinned
census that fails in BOTH directions.

## Alternatives rejected

- **Delete Arm B and stop.** Buys the false-positive fix with five real,
  undeclared false negatives. The vocabulary swap is not an enhancement; it is
  what makes the deletion safe.
- **A productive rule instead of a list.** ADR 0047 replaced ADR 0046's list
  only because the rule was measured byte-identical over 29 corpora plus a
  purpose-built positive control. No credential-name corpus exists, and the
  error directions are opposite: DB-052's admission SILENCES, SEC-001's
  AFFIRMS. Deferred with its reason.
- **Reuse `ExcludedRule` for the limit.** SEC-001 *is* covered, so recording it
  as an exclusion would invent a hole that does not exist — ADR 0066's phantom
  exclusion pointed backwards.
- **A new `Manifest.DeclaredLimits` field.** Honest, but droppable by a
  summariser, and it widens the response JSON mid-flight of a separate open
  change.
- **COVERAGE.md alone.** Inverts the source-to-mirror chain CLAUDE.md forbids —
  a mirror more truthful than its source, which has already happened twice.
- **Declaring the plural loss as a limit instead of closing it.** A limit is for
  a gap the mechanism cannot close. Component matching closes this one exactly,
  with an enumerable fold, so declaring it would have been a choice to ship a
  smaller Go for no gain — and the constraint is that Go ends strictly stronger.
- **A trailing-`s` strip in the matcher.** Same verdicts, but a predicate rather
  than a value: nothing could print what it admits, so the pinned set and the
  cross-provider control would both lose their subject.
- **A Go `coverage.go`.** Out of scope: ADR 0065's derived floor stands.
- **Adding `credential`, `authtoken`, `clientsecret` to Go's set.** TS carries
  them; Go's admission into an affirmation channel needs measurement first.
  Recorded as divergence rows rather than inferred.

## Notes

Go's exposure (ADR 0065) is untouched. The defect predates exposure; exposure
only made it user-visible. Go ends this change strictly stronger, never
smaller.
