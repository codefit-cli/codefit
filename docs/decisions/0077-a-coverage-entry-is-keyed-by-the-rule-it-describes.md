# 0077 — A coverage entry is keyed by the rule it describes

- Status: accepted
- Date: 2026-08-15
- Supersedes: nothing. Completes the entry shape ADR 0076 recorded, and exercises
  the append-only clause that shape came with.

## Context

ADR 0076 replaced a 143 KB prose golden with an ids-only one and recorded the
rule that comes with naming things: **entry ids are append-only, and a rename or
a removal is a breaking change that needs an ADR**. This is that ADR.

The shape landed in two steps on purpose. The first wrapped each existing DB
prose blob in exactly one entry, 1:1, so the payload reduction could be measured
without any prose being cut. That left a gap it named at the time: a rule id like
`DB-011a` lived *inside* another entry's detail. It was in the payload, so it was
not lost — but it was not in the index, so an agent could not name it, and asking
for it came back unrecognized.

Measured on the tree before this change, over the 32 DB entries:

| fact | value |
|---|---|
| distinct rule ids the DB prose declares | 32 |
| rule ids that were an entry of their own | 0 |
| entries carrying more than one rule id | 21 |
| entries whose prose describes more than one rule as its subject | 5 |
| largest such blob | 11,747 bytes, five rules |

`DB-051`, `DB-052`, `DB-053` and `DB-003` all came out of one blob. So did
`DB-001`, `DB-011a` and `DB-002`. An agent auditing a schema and holding a
`DB-053` item in its hand had no way to ask this manifest what `DB-053` covers.

## Decision

**An entry whose prose describes exactly one rule is keyed by that rule's id.**
A blob describing several rules is cut into one entry per rule. Prose that
belongs to a family rather than to any one rule — a shared contract, a dogfood
status, a scope limit — keeps a namespaced `db.*` slug and stays in the index as
an entry of its own.

Consequences, stated rather than left to be discovered:

- 17 entries were **renamed** from a `db.*` slug to the rule id they describe
  (`db.never-used-index` → `DB-012`, `db.trigger-cross-table-cascade` → `DB-040`,
  and 15 more). This is the breaking change this ADR exists to record.
- 5 blobs were **cut** into 21 entries: 15 keyed by a rule id, 6 by a family slug.
  The arithmetic is the check: 17 renamed + 15 cut = the 32 rule-keyed entries the
  next-but-one bullet claims, and 10 untouched slugs + 6 family = 16 slugs. An
  earlier draft said 16 + 5, which summed to 33 rule-keyed and contradicted its own
  next-but-one bullet — measured against the committed id golden before merge.
- The DB block goes from 32 entries to 48; the TypeScript index from 52 to 68.
- Every one of the 32 rule ids the prose declares is now an entry of its own, in
  both directions: no declared rule id is missing an entry, and no entry is keyed
  to a rule id the prose never declares.

**No released artifact ever carried the renamed ids.** The entry shape landed
after `v0.2.8` and has not been tagged (`git tag --contains` on its first commit
is empty), so the rename costs no shipped consumer anything. That is why it is
recorded and taken now rather than deferred behind a compatibility alias: an
alias would be permanent debt bought to protect nobody.

## The cut is a partition, not a rewrite

Every slice is a **contiguous, non-overlapping, gap-free** cut of its original
blob. Concatenating a blob's slices in order reproduces the blob. Nothing is
paraphrased, summarized, or authored into a `Detail`; new authoring happens only
in the one-line `Claim`, which is what a claim is for.

This is stricter than the plan required. The plan allowed an
`authoredDuringSplit` allowlist for shared preambles a split would have to
duplicate. **The allowlist is empty**: where a preamble is shared, it stays whole
in a family entry of its own instead of being copied into each rule's detail. A
duplicated preamble would have been text the corpus check had to be told to
forgive; a family entry is text the agent can ask for by name.

## How the split is proved rather than trusted

Hand-cutting ~126 KB of prose is exactly the work where content disappears
quietly, because nobody diffs 126 KB line by line in review. Three checks:

- **C1 — the rule-id census, committed.** The multiset of rule ids per bucket,
  captured from the tree *before* the first edit, must equal the multiset over
  `Claim + Detail` after. It is committed at
  `internal/core/dbcoverage/testdata/ruleid_census.json` and asserted by
  `TestRuleIDCensus_IsConservedByTheEntryShape`.
  Alongside it, `TestEveryRuleIDInTheCensus_IsAnEntryOfItsOwn` reads the same
  census both ways and is the control for this ADR's decision.
- **C2 — no invention.** Every whitespace-normalized `Detail` must be a substring
  of the pre-change corpus.
- **C3 — nothing dropped.** Every pre-change blob must be *tiled completely* by
  the entries cut from it, and the byte floor is derived from the junctions that
  tiling actually finds.

C2 and C3 need the 126 KB pre-change corpus. Committing that corpus would
re-import the exact defect ADR 0076 deleted, so **only C1 is committed**; C2 and
C3 ran once, in-PR, from a scratchpad harness against a worktree of the
pre-change tree, and their output is recorded in the pull request. That trade is
deliberate: it buys a one-time proof without leaving a 126 KB golden behind.

### What the mutations showed, including about the checks themselves

Each check was broken on purpose and watched to fail:

- Deleting `DB-053`'s split entry turned C1 red — *`Reasoning: rule id DB-053
  appeared 2 times before the change and 1 times after`* — and turned the
  entry-of-its-own control red naming `DB-053`.
- Paraphrasing one word inside `DB-053`'s detail turned C2 red, naming the entry
  and the byte at which it diverges from the corpus.
- Deleting one 123-byte sentence from `DW-001`'s detail turned C3 red, quoting
  the sentence and reporting the 124-byte delta. **C2 stayed green through that
  mutation**, which is why both exist: a deleted sentence leaves a substring.

The C3 mutation also exposed a weakness in the byte floor as originally planned.
A pure floor of `corpus − allowlist` cannot distinguish a lost sentence from the
single space a cut consumes at each junction, and when a mutation changes the
junction count the floor moves with it — under the paraphrase mutation the floor
arithmetic reported delta 0 while content had in fact changed. So the floor is
not the load-bearing half: **the tiling is**, and the floor's junction allowance
is derived from the tiling rather than assumed.

## Alternatives rejected

- **Keep the slugs and add a rule-id alias table.** A second name for the same
  entry is a second thing to keep in sync, and the repo has been burned by
  exactly that shape of drift twice. The index should have one name per entry.
- **Split every entry that merely mentions a rule id.** 21 entries mention more
  than one; only 5 describe more than one. Splitting on a mention would key an
  entry to a rule whose paragraph it does not carry — an index that names
  something the manifest does not describe, which is the failure the
  entry-of-its-own control checks for in its second direction.
- **Split the 34 KB SQL-DDL dialect-limits entry too.** Its subject is the
  parser's declared limits, not a rule. It keeps its slug. Splitting it would be
  a different decision about a different axis, and it is not taken here.
