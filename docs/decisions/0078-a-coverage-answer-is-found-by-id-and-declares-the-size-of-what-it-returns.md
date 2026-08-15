# 0078 — A coverage answer is found by id, and declares the size of what it returns

- Status: accepted
- Date: 2026-08-15
- Supersedes: nothing. Closes the entry-shape work ADR 0076 opened and ADR 0077
  continued: 0076 gave an entry an identity, 0077 gave every rule its own, and
  this one makes that identity the thing the controls and the human mirror read.

## Context

Three controls guard the DB coverage manifest, and until this change all three
answered their question by **searching joined prose for a rule-id-shaped token**:

- **Control A** — every rule registered in `dbrules.All()` / `dwrules.All()` /
  `crossrules.All()` is declared by the manifest.
- **Control B** — every rule-id token the manifest mentions is answered: it is
  registered, declared absent, or delivered under another identifier.
- **Control D** — every rule id the PRD promises is answered by one of those three.

That was the only implementation available when a rule id existed nowhere but
inside a paragraph. It came with a prefix hazard that needed its own defence —
`containsWholeToken`, whose entire purpose was keeping `DB-011` from being
satisfied by a manifest that only says `DB-011a` — and with a much larger hole
underneath it, which the prefix defence did not touch: **a rule id merely
MENTIONED inside another rule's paragraph satisfied the control.**

That hole is not hypothetical here. Measured on `main` before this change, 21 of
the 23 registered rule ids appear inside the prose of some OTHER entry: `DB-050`
is spelled inside `DW-002`'s and `db.table-structural-completeness`'s paragraphs,
`DW-005` inside six other entries. Any one of them could have lost its own entry
with every control staying green.

ADR 0077 removed the reason to search prose at all: every rule the manifest
describes is now an entry keyed by its own rule id.

Separately, verification of the first slice found a second defect of the same
family — a claim that does not describe the thing it is about. `codefit-coverage`
answered a `detail:[…]` request with a **170,446-byte** response declaring
`bytes: 15728, over_budget: false`. Nothing was withheld and nothing was
truncated, so it was neither a regression nor an ADR 0054 breach. It was a
**response misreporting its own size**, because the size was measured over the
index while the response carried the index *and* the detail. That is byte for
byte the defect class PR #128 fixed for `scan-all`'s budget note.

## Decision

**1. Controls A, B and D ask the id set, not the prose.** `containsWholeToken`
and `isAlnum` are deleted. The question each control asks is unchanged; what
counts as an answer is not. An answer is now an **entry an agent can name**, not
a sentence that happens to spell an id somewhere.

This is strictly stronger, and it was proved by mutation rather than asserted.
Re-keying `DB-050`'s entry to a slug while leaving its prose untouched:

- the old Control A **passed** — `DB-050` is still spelled in two other entries;
- the new Control A **failed**, naming `DB-050`.

The same shape holds for Control B (re-key `DB-201`; its prose still spells the
id, so the old prose branch still returned true) and for Control D (re-key
`DB-012`; the old substring search over ~128 KB of joined prose still found it).

Control B **keeps its `ruleIDToken` regex**. It hunts phantom MENTIONS, mentions
live in prose, and prose still lives in `Entry.Detail`. Only its three answer
branches moved to id lookup.

Control D **keeps a prefix match**, and this is a deliberate exception with one
live case: the PRD promises `DB-011` and codefit shipped it split into `DB-011a`
and `DB-011b`, so a parent id is answered by the children it shipped as. The
limit that accepts — a promised id that is a strict prefix of a longer entry id
would be answered by that entry — is now scoped to the manifest's ~68 ids rather
than to all of its prose, and the alternative (equality plus a hand-maintained
parent→children alias table) reintroduces exactly the hand-maintained list the
coverage spec forbids.

Control E's *finder* moved too, for a different reason: it located its subject by
scanning every entry for an anchor phrase and taking the first hit, which
silently retargets the moment another entry quotes the phrase. It now names the
entry (`db.table-structural-completeness`) and keeps the anchor for locating the
sentence inside it. A finder that can match the wrong line is a control that can
redden over something that was never its subject.

**2. `COVERAGE.md` gains the ids and keeps its full prose.** The human mirror
carries a bracketed `id:` marker next to each block — several, comma-separated,
where one block mirrors several entries. The prose is untouched: a human has no
token limit and abridging the mirror is pure loss. The marker is a **sigil rather
than a bare id** on purpose. `COVERAGE.md` already spells rule ids inside running
prose in a dozen places, so a control that searched for the id would find one of
those, report the mirror complete, and have read a line that was never its
subject.

`TestCoverageMirror_NamesEveryEntry` checks the correspondence in both
directions: a declared entry named nowhere in the mirror fails, and a marker
naming an id no entry carries fails. It guarantees **correspondence, never
accuracy** — a marker beside the wrong prose passes — which is the same limit
Control A states about itself.

Writing it found the gap it was written to find: `ts.sql-injection-via-intermediate`
and `ts.xss-inner-html-from-variable` were declared entries that the mirror never
carried at all, while two deterministic bullets pointed at them with "is
**surface** (below)". Both are now mirrored.

**3. A coverage response declares the size of what it is returning.** `bytes` is
the whole payload — index plus any detail asked for — and `index_bytes` is the
index's share. Nothing is withheld: coverage authorizes no withholding under any
condition, and detail was asked for by id, so every requested entry comes back
whole. An over-budget detail request **says so**:

```
BEFORE  detail:[all 68 ids] -> 182,440 B response declaring bytes: 21,951, over_budget: false
AFTER   detail:[all 68 ids] -> 182,848 B response declaring bytes: 182,152,
                               index_bytes: 21,951, over_budget: true, withheld: 0
```

The two budget notes are different text because the instruction is different. An
over-budget **index** is an authoring problem — shorten a claim, never drop an
entry. An over-budget **detail** is not: nothing was authored too long, the
caller asked for a lot at once, and telling them to shorten a claim would send
them to fix the wrong thing.

## What this does NOT do

- It does not fix the wire duplication (roadmap **P0-14**), or resolve which copy
  a client meters. Both remain filed and unmeasured.
- It does not truncate, sample, or page a coverage answer. `over_budget: true`
  with a complete payload is the whole behaviour.
- It does not move the structural response cap for `scan-all` (roadmap P0-4).

## The corrected figures, and where they came from

The previous slice published three numbers that did not describe the tree, all
in a **merged, immutable PR body** and in the change's planning notes. No
committed artifact carried them — probed with a positive control over `docs/`,
`CHANGELOG.md`, `COVERAGE.md`, `README.md` and `internal/` — so there is nothing
to correct in the repository and no history to rewrite. They are corrected here
instead, each measured on this tree:

| published | measured | how |
|---|---|---|
| longest DB claim 391 B, headroom 9 | longest DB claim **386 B** (`DW-011`, tied by `DB-001`); longest claim overall **392 B** (`ts.hardcoded-secrets`), headroom **8** | `TestCoverage_IndexCarriesNoPayloadSizedString` now logs it every run |
| payload 22,309 B / index 22,031 B / wire 44,618 B | **22,249 / 21,951 / 44,498** | the committed integration test over a real transport pair |

The measured headroom is deliberately **not** written into a comment beside
`MaxClaimBytes`. A figure recorded in prose drifts the moment a claim is edited —
which is exactly how the wrong one was published — so the test logs it and the
comment points at the test.

`MaxClaimBytes`'s derivation was re-run against the entry count the split
actually produced rather than the ~70 the first pass assumed. **The entry count
did not move in this change: 68 before, 68 after.** `40,000 × 0.85 / 68 ≈ 500`,
still rounded down to **400**; worst case `68 × (400 + ~60 framing) ≈ 31 KB`,
under the 40,000 budget. The cap is unchanged.

## Alternatives rejected

**Keep the prose search and add the id lookup beside it.** Two controls asking
almost the same question, one of them known-weaker, is how a green suite comes to
mean less than it did. The weaker one goes.

**Give `COVERAGE.md` the ids by restructuring it 1:1 with the entries.** It would
split blocks a human reads as one explanation, for a machine that already has the
1:1 structure in the index. The mirror exists for the human; the marker is enough
to join the two.

**Withhold detail once a request crosses the budget.** This is the option the
budget explicitly does not authorize for coverage (I5). An agent that asked for
eight declared limits and silently received five is worse off than one that
received eight and was told the response is large.
