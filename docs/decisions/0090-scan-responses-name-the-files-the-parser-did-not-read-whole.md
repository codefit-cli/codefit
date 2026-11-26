# 0090 — Scan responses name the files the parser did not read whole

Date: 2026-11-26
Status: accepted
Follows [ADR 0089](0089-a-file-the-parser-did-not-read-whole-is-recorded-and-cached-as-such.md),
which built the detection and left it out of every response. Amends
[docs/specs/change-scope.md](../specs/change-scope.md) R1 and test case 5 (dated amendment).
Issue `#189`.

## Context

ADR 0089 made the security sensor record, per analysed file, whether the TypeScript parser
read it whole — `findings.SensorResult.ParseStatusReported` and `ParseIncomplete` — and kept
that status in the finding cache. It said, in its consequences, that **no tool response
changed**: the detection existed in the core, the sensor and the cache, and an agent could
not see it. A file the parser stopped reading, or read around an error, still produced fewer
findings, and fewer findings is still indistinguishable from a cleaner file to whoever reads
the response.

Two things were left open for this change. ADR 0089 deferred whether codefit should tell the
caller that parser settings are overridden, after measuring that under `GOT_C_RECOVERY=0`
broken code reads as complete. And its native review (informational finding
`R3-vacuous-status-reported`) noted that a run analysing **zero** files reported the status as
reported with an empty list — "every file was read whole" about no file at all.

## Decision

The repository owner approved the plan on 2026-11-26 with the words "si, aprobado, arrancá
con T3", including the two decisions below.

### The response shape

Three fields, reusing the JSON names `findings.SensorResult` already declares, side by side
the way `recognized_authz_helpers` and its `_note` sit (`mcp.ParseStatusBlock`):

- `parse_status_reported` (bool) — true only when the provider reported a status for every
  analysed file **and** at least one file was analysed.
- `parse_incomplete` — the files not parsed whole, sorted by path, each `{file, reason, line}`.
  **Always** a list: `[]` when none, never `null` and never absent where the block is present.
- `parse_status_note` — omitted only when there is nothing to say (status reported, nothing
  listed). With files listed it names the count and the possible reasons (`stopped:<reason>`,
  `error_root`, `error_node`, `missing_node`, `error_flag`).

Measured on the shipped handlers (`codefit-scan-security`, real parser), a clean TypeScript
file: `"parse_status_reported": true, "parse_incomplete": []`, no note, `scope.note` empty. A
file with `}}}}` on line 3 beside a clean one:

```json
"scope": {"mode": "full", "requested": 0, "audited": 2, "auditable_total": 2,
          "note": "1 audited file(s) were not parsed whole and are listed in `parse_incomplete`: each still counts as audited and in the score, but its findings and surface cover only what the parser read — from the listed line on, a missing finding is not evidence of a clean file."},
"parse_status_reported": true,
"parse_incomplete": [{"file": "src/broken.ts", "reason": "error_node", "line": 3}],
"parse_status_note": "1 analysed file(s) were not parsed whole and are listed in `parse_incomplete` with the reason and the first affected line. Each still counts as audited and in the score; … Reasons: …"
```

A listed file **still counts**: `scope.audited`, `score` and `blocked` are computed exactly as
before (S13). The status is a caveat, never an exclusion — excluding the file would hide what
the parser did read, including findings it did report.

| Tool | Where | When no status is reported |
|---|---|---|
| `codefit-scan-security` | top level | `false`, `[]`, and a note |
| `codefit-scan-all` | inside `security`, present only when `measured: true` (like `surface_coverage`); with files listed **and** the db dimension run, the note adds that the code×schema cross parses those files separately and reports no status | `security.measured: false` already says the code was not audited; the keys are absent |
| `codefit-scan-endpoint` | top level, narrowed to the requested file (0 or 1 entries) | `false`, `[]`, and a note — also when the pass did not analyse the requested file |
| `codefit-surface-idor`, `-authz`, `-overfetch`, `-nplus1` | top level, constant | always `false`, `[]`, and a note that these tools do not check and pointing to scan-security / scan-all (one string for response and description, like `helperScopeNote`) |

Each "not reported" note names its cause, so an absent status never reads as complete: the
provider reports no parse status (Go); no file was analysed in this pass; the requested file
was not among those analysed (scan-endpoint); the surface tools do not check.

The zero-file case is fixed at the source: the sensor reports a status only when the provider
is a `FileAnalyzer` **and** at least one file was analysed.

### Decision 1 — `scope.note` under `mode: full`

S13 requires a non-empty `scope.note` whenever the list is non-empty. `ScopeBlock.Validate`
and change-scope R1 forbade any note under a full scan ("a full audit has nothing to caveat").
A full audit whose files were not all read whole does have something to caveat, so the rule is
amended, not bypassed: under full, a note is allowed **only** when the incomplete count is
above zero; a full scan with a note and a count of zero still fails; a count above zero with
an empty note fails under either mode. The count travels in the block as a never-serialized
field (`json:"-"`) so `Validate` can hold the note to it. Under partial, the sentence is
appended to the partial note.

This is the only existing output that changes. A run with nothing listed keeps its scope byte
for byte (`withParseIncomplete(0)` returns the block unchanged, pinned by a test).

### Decision 2 — overridden parser settings, by name only

A new optional provider interface, `providers.SettingsOverrideReporter`, returns the sorted
NAMES of the settings outside the binary that are set in this process. The TypeScript provider
implements it over the same prefix loop as its cache-key fingerprint (ADR 0089, D2) — one
function, `parserEnv`, so the two cannot disagree about which settings are set. The MCP layer
resolves it by type assertion; `internal/core` never sees the prefixes. Scan responses carry
`parser_settings_overridden` (omitted when none is set) and one sentence in the note.

Names, never values: a value is whatever the user put in their environment, and a response
that carried it would publish it. The name is enough for the reason the field exists — under
a non-default setting, broken code can read as complete (ADR 0089, "Not covered") — and the
agent can ask the human for the value if it matters.

The fingerprint is part of the cache key, so sharing its loop must not change it: its output
is pinned byte for byte against values produced by the pre-change code (`main@22617b8`).

## What was measured

- **Differential against `main@22617b8`**, scratch only: `codefit-scan-security`,
  `codefit-scan-all`, `codefit-scan-endpoint` and `codefit-surface-idor` over a copy of the
  TypeScript provider's committed testdata (64 files), default settings, JSON compared key by
  key. The only differences are the new keys and `scope.note` (empty → the parse sentence) in
  scan-security and scan-all, where 20 files are listed — every still-an-error row of the
  parser-form fixtures, 8 as `error_node` and 12 as `missing_node`, all on line 1. Findings, surface,
  scores, buckets, the baseline delta and the budget are identical.
- **Size**: a measured scan-all response with nothing listed grows by 51 bytes
  (`"parse_status_reported":true,"parse_incomplete":[]`); on the budget test's fixture the
  withheld-everything floor moves 4,745 → 4,796 bytes and the full response 5,741 → 5,792.

## Consequences

- Agents can see which files were not read whole, why, and from which line; the tool
  descriptions teach the fields by the names the responses carry, verified by a test that
  reflects the JSON tags and reads the live descriptions over `tools/list`.
- `scope.note` is no longer always empty under a full scan. A consumer that treated a
  non-empty full-scan note as an error must read it instead.
- A pass that analyses no file now reports `parse_status_reported: false` (it was `true` in
  the sensor result, which no response carried).
- **The skill `codefit init` generates is not updated by this change.** The repository
  commits its rendered copy (`.claude/skills/codefit/SKILL.md`), locked by
  `TestCommittedSkillMatchesRenderedSkill`, and that file was outside this change's authorized
  edit surface. Until the skill is updated, an agent learns the fields from the tool
  descriptions only.

## Not covered, declared

- **Valid code the parser misreads into an error-free tree** reads as complete and is not
  listed (ADR 0089): the status trusts the parser's own marks.
- **Broken code the parser accepts with no mark under the default settings**: a double comma
  inside an object literal (`const o = { a: 1,, b: 2 };`) reads as complete and is not listed.
- **`GOT_C_RECOVERY=0`** and other non-default settings: broken code can read as complete.
  This change names the setting (`parser_settings_overridden`); it cannot list the file.
- **Paths that do not go through the security sensor** report no status: the
  `codefit-surface-*` tools (they say so, constantly), the code×schema cross in scan-all's `db`
  section (`ExtractQueryFilters`; the security note says so when files are listed and db ran),
  and every direct caller of `Parse`.
- **Go**: the Go provider reports no parse status, and the responses say "not checked"; its
  provider still aborts a whole scan on one syntax error, which is a separate follow-up.
