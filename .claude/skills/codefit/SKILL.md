---
name: codefit
description: Audit AI-generated code for security flaws agents miss — broken authorization — and for database schema risks in a Prisma schema or SQL migrations. Use when reviewing API endpoints or route handlers, auditing a database schema, or before merging AI-written backend code.
---

# codefit — audit AI-generated code

To audit code you MUST call `codefit-scan-all` FIRST. Do NOT audit by reading files
manually — codefit maps the surface (broken authorization) and your
job is to REASON over its output, not to replace it. It does NOT call an LLM — you do the
reasoning. Drive it through its MCP tools.

## When to run
- Reviewing or merging AI-generated endpoints, route handlers, controllers, or queries.
- Before a commit touching authorization, resource access, or which fields are returned.

## Run a full scan
Call `codefit-scan-all` with:
- `root`: absolute path to the repo root
- `language`: "go"

## Narrow a scan to the files you changed (optional)
Add `changed_files` — the project-relative paths you touched — to audit ONLY those files.
codefit never asks git which files changed; you already know, you just wrote them. Omitting
it (or passing an empty list) is a FULL audit — an empty list is never "audit nothing".
ALWAYS read the `scope` block before you report anything:
- `mode: "partial"` means `findings`, `score` and `blocked` describe ONLY the audited files.
  `blocked: false` then means "no critical in THAT SLICE", not "no critical" — say that to the
  human instead of reporting a clean project. `blocked: true` needs no caveat.
- `unmatched` lists paths the audit never reached (deleted, not an auditable extension, inside
  a skipped dir). Those were NOT audited, which is NOT the same as audited and clean.
- `by_dimension.db` is `null` (not measured) unless a configured schema path is in scope.
  Null is not 100 — the schema was not looked at.
- `by_dimension.security` is `null` (not measured) when no security provider resolves
  for `language` — the `security` section's `note` explains why (the db
  dimension may still be measured on its own). Null is not 100 — the code was not looked at.
- A partial scan is for a fast pass on a change. NEVER prune the baseline after one (below).

## Read `summary` as PER-DIMENSION counts, never as the project's state
Every count declares which dimension it counted:
- `summary.security` — `endpoints`, `deterministic_findings`, `surface_items`,
  `certain_concerns`. Security only.
- `summary.db` — `schema_sources` (how many schema sources were read),
  `deterministic_findings`, `surface_items`. The database dimension only.
- `summary.totals` — the roll-up of ONLY what means the same in both:
  `deterministic_findings` and `surface_items`. Never endpoints — a table has no route.

A sub-block is `null` when that dimension was NOT measured. **Null is not zero**: zero
means codefit looked and found nothing; null means nobody looked. So
`summary.security: null` is never a clean security result, and
`summary.db: null` is never a clean schema.

NEVER report the project as clean off `summary.security` alone. A schema-heavy project
can show `summary.security.surface_items: 0` while `summary.db.surface_items` holds dozens
of open questions. Read every non-null sub-block, and `summary.totals` for the whole.

The counts are the RAW population, taken BEFORE the baseline filter — that is why one can
exceed what the buckets and `db.surface` actually list. The difference is the surface the
baseline already tracks, not a contradiction; `summary.note` says so in the response.

## Read the three buckets in the response
Every bucket NAMES its endpoints; none of them inlines the concern text. That is on
purpose — inlining it made `codefit-scan-all` too big to return on a real project.
- `actionable` — a concern AND a local gap. Act on these. Each entry carries what you
  need to RANK it without fetching: `concerns`, `categories`, `gaps` (hardest kind
  first), `highest_certainty`, `has_affirmation`.
- `resolved_clean` — codefit found the expected check present locally (an
  authorization check / field selection) — no gap here, but it did NOT verify the check
  is sufficient. No immediate action.
- `frontier_pending` — codefit could NOT conclude locally (the check crosses
  files it does not follow). THESE NEED YOU. Not concluded is not clean —
  never discard them.

`deterministic_concerns` is the ONE thing that comes back in full, on the `actionable`
entry that has it: a deterministic finding is a fact codefit already concluded, not a
question. Never spend a second call to re-read one.

## Fetch an endpoint's full concerns
Pick the endpoints worth pursuing from the summaries, then for each call
`codefit-scan-endpoint` with:
- `root`, `language`: "go"
- `file`: the endpoint's file path
It re-runs the same stateless analysis, so what you get back is exactly the signals and
`reason_to_review` that `codefit-scan-all` left out. Then reason over that surface to
decide if the concern is real. Each surface item names WHAT to check (an id→resource
lookup, an authz decision, a returned object) and WHERE — codefit does not decide if
it's a vuln; you do.

## Check the response's `budget` block
`codefit-scan-all` writes to a declared byte budget so it always RETURNS. Read
`budget` before you conclude anything about the project:
- `withheld: 0` — you have every endpoint codefit classified.
- `withheld: N` — N endpoints are NOT in the lists. Each bucket's `count` is still
  the complete number and its `withheld` says how many are missing, so what you have
  is a PREFIX of `budget.ordering`, not the whole audit. Say so to the human, and
  re-run narrowed with `changed_files` to reach the rest. Never report a withheld
  endpoint as clean — it was not shown, which is not the same as audited and fine.

## Audit the database schema
Call `codefit-scan-db` with `{root, language: "go"}` to audit the schema on its
own. `codefit-scan-all` runs it too, but ONLY when `database.schema_paths` is set in
`.codefit.yaml`. It reads a Prisma `schema.prisma` OR a directory of SQL-DDL/Flyway
migrations (PostgreSQL, MySQL, SQL Server), and classifies the schema as transactional or
a warehouse — on a warehouse it adds the star-schema/SCD, columnar-index and partitioning
checks.
- A table with no primary key is deterministic. Everything else — foreign keys with no
  covering index, duplicate/redundant indexes, sensitive columns in the clear, risks in
  procedure/trigger bodies — is SURFACE: yours to reason about, exactly like the endpoint
  surface.
- `measured: false` means codefit could NOT read the schema (none configured, no parser for
  it, or every source unreadable). That is NOT clean — read the `note`, fix the config, re-run.

## Dependencies and declared limits
- `codefit-check-cves` `{root}` — known CVEs in dependencies via OSV.dev, read from exact
  lockfile versions. `codefit-scan-all` does NOT run it; call it separately.
- `codefit-coverage` `{language}` — what codefit audits deterministically, what it maps as
  surface, and what it does NOT cover. Read it before telling the human something is out
  of scope; do not assume the boundary. For `go` today: 1 of 4 surface categories mapped (authz); not mapped: idor, overfetch, nplus1 — these were never searched for, not merely absent from this scan.
  It answers with an INDEX, not with prose: every entry as `{id, claim, status, has_detail}`,
  and it withholds NOTHING from that index (`withheld` is always 0 and says so). To read one
  entry in full, call it again with `detail: ["<id>"]` — MANY ids in one call, e.g.
  `{language: "typescript", detail: ["DB-050", "db.sqlddl-dialect-limits"]}`. Rule ids
  (`DB-050`, `DW-011`, `SEC-001`) are entry ids, so a rule you saw in a finding is askable
  by name. An id that matches nothing is NAMED back to you as unrecognized, never answered
  with an empty success. Ask for the ids you need rather than all of them: the response
  declares its own `bytes` and tells you when the detail you asked for crossed the budget —
  complete either way, never truncated.

## Baseline (don't re-review what's already tracked)
codefit records the audited surface in `.codefit-baseline` (committed). `codefit-scan-all`
manages it and reports a delta — act on what's new:
- Focus on `baseline.new` and `baseline.changed`. `known` surface is already tracked — don't re-review it.
- Deterministic findings (confidence 1.0, e.g. a hardcoded secret) are NOT auto-silenced:
  they show on every scan until accepted. `baseline.affirmations_shown` counts them.
- To act on items, get their fingerprints from `codefit-baseline-list` (use
  `filter: "known"` for items not yet accepted) — do NOT read the `.codefit-baseline` file.
- When the HUMAN decides an item is a false positive or accepted debt, call
  `codefit-baseline-accept` with its `fingerprint` and the human's reason. ONLY when the
  human said so in the conversation — NEVER accept an item on your own.
- After a refactor, items the fix removed appear in `baseline.gone_candidates`. Confirm with a
  FULL `codefit-scan-all` (no `changed_files`), then call `codefit-baseline-prune` to drop them.
  NEVER prune off a partial scan: a scan that did not open a file has no evidence its items are
  gone, so a partial run never proposes them, and `codefit-baseline-prune` takes no
  `changed_files` at all — it always re-scans in full. Scanning may be partial; forgetting
  may not.
Narrate every baseline operation to the human (what you accepted or pruned, and why).
codefit never edits code — only its baseline.

## Custom authz helpers (teach codefit your project's auth)
codefit knows NextAuth-style helpers (`getServerSession`, `auth`) but NOT your project's own
(e.g. `requirePermission`, `getCurrentUser`). When many authz items show `known_authz_detected:
false` yet they DO call a project auth function, reason about whether that function is a
real authz helper, then PROPOSE registering it to the human ("N items call `X`, which looks
like your authz helper — register it?"). ONLY when the human approves, call
`codefit-baseline-register-authz-helper` (`{root, language, helper_name, reason}`); reverse with
`codefit-baseline-unregister-authz-helper`. NEVER register a helper on your own — registering
silences the authz gap on EVERY item that calls it (dozens at once), far more reach than
accepting one. Registering changes a FACT ("this helper is present"), not a verdict: it
clears the AUTHZ gap (permission) only. Keep reviewing anything else this project's surface maps.
