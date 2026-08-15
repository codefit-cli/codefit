# 0076 — A coverage golden locks identity, not prose

- Status: accepted
- Date: 2026-08-15
- Supersedes: nothing. Narrows the scope of the golden introduced with ADR 0065's
  derived-manifest work.

## Context

`internal/mcp/testdata/coverage_ts_prechange.json` was a byte-for-byte capture of
the entire TypeScript coverage response.

Measured, not estimated:

| fact | value |
|---|---|
| golden file size | 143,557 bytes |
| longest single line in it | 34,226 characters |
| pre-change wire payload of `codefit-coverage {language: typescript}` | 143,293 bytes |
| longest single string inside that payload | 34,080 characters |

That longest line is one declared limit — the SQL-DDL dialect limits — serialized
into a JSON string. A single entry was 24% of the whole response.

The test's own doc comment stated a much narrower contract than its assertion:
that R1's DERIVED fallback must never replace a language that already has a
hand-written prose manifest. That contract is one boolean.

The gap between the stated contract and the assertion was not theoretical. While
re-shaping the manifest, renaming a struct field — a change that altered no prose
at all, added no entry and removed none — turned the golden red:

```
coverage_regression_test.go:48: typescript's coverage manifest changed from the
pre-change (810b816) response
```

A golden that goes red on a rename is not protecting the contract it claims to
protect. It is protecting every word of every sentence, which makes editing a
declared limit — the thing this repo most wants people to do honestly and often —
feel like breaking a test.

## Decision

The 143,557-byte prose golden is DELETED. The contract is asserted in two pieces,
each matched to what it actually guards:

1. **The stated contract**, directly: `Derived == false` for TypeScript, plus a
   non-vacuity check so "Derived is false" cannot also pass over an empty answer.
2. **The identity of the entry set**, by an ids-only golden —
   `internal/mcp/testdata/coverage_entry_ids.json`, 5,506 bytes, ids and answer
   statuses for both TypeScript and Go, no prose. It fails naming the ids that
   appeared and the ids that vanished, and it treats a status move as its own
   failure.

**Re-capturing a full-prose golden of the new shape is FORBIDDEN.** It would
re-import the exact defect this ADR removes, one shape further along.

Prose is not left unguarded, it is guarded by something better suited to it:

- `internal/core/dbcoverage/testdata/ruleid_census.json` (1,793 bytes) holds the
  multiset of rule ids per bucket, captured from the tree BEFORE the first edit.
  A conservation test re-tokenizes claim-plus-detail and must reproduce it
  exactly. That catches prose being LOST, which is the failure that matters,
  without caring how a sentence is worded.
- `MaxClaimBytes` caps a claim at authoring time, so no declared limit can grow
  back into a payload-sized string in an index.

## Consequences

- Editing the wording of a declared limit no longer requires regenerating a
  143 KB fixture. Adding, removing or renaming an ENTRY still requires a
  deliberate golden update, and the diff is readable.
- The ids golden is where the entry count becomes visible, which is what makes
  `MaxClaimBytes`'s derivation re-checkable: past roughly 85 entries the worst
  case crosses the response budget and the derivation must be re-run.
- One control was retired rather than migrated:
  `TestHandleCoverage_Go_StatesSEC001Limit` asserted that SEC-001's declared limit
  travels on SEC-001's own line and no other. It is superseded in place by
  `TestCoverage_GoDerivedFloorKeepsSEC001sLimitWeldedToItsOwnEntry`, which asks
  the same question of the shape an agent now receives and two more besides: that
  SEC-001's index claim itself warns the rule is qualified, and that the limit
  appears on no other entry's claim OR detail. The old version checked only the
  middle one. ADR 0075's placement rule is unchanged; only where the text sits on
  the entry moved, because the limit is roughly 870 bytes and a claim may be 400.
- STATED, not implied: the census and the ids golden are weaker than a
  byte-for-byte lock in one specific way. A sentence inside a declared limit can
  be reworded without any test noticing, as long as no rule id count changes and
  no entry disappears. That is deliberate. The alternative cost 143 KB, went red
  on renames, and its longest line was itself the defect.

## Measured result

| | before | after |
|---|---|---|
| `codefit-coverage` structured payload | 143,293 B | 16,006 B |
| same payload on the wire (see the duplication note below) | 286,586 B | 32,012 B |
| longest single string | 34,080 chars | under 400 |
| entries served | 52 | 52 |
| entries withheld | 0 | 0 |

No entry was dropped. The count is identical on both sides, which is the point:
this change alters how a correct answer is RENDERED, never what codefit detects.

## Recorded here because it was found here: the wire carries every response twice

`internal/mcp/server.go`'s `addTool` returns `nil` for the `*CallToolResult`, and
the go-sdk then serializes the same output JSON into a `TextContent` block. The
integration test now proves this empirically on both trees: the text block and
the structured payload are byte-identical, before the change and after it.

This is FILED, NOT FIXED, and it belongs to the roadmap rather than to this
change. One caveat travels with it and must not be dropped: **which copy the
client meters is UNMEASURED.** ADR 0062's calibration bracket is in
codefit-payload bytes and its ~1.93× ratio must NOT be doubled on the strength of
this finding.
