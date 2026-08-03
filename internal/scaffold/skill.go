package scaffold

import (
	"bytes"
	"fmt"
	"text/template"
)

// skillTemplate is codefit's own skill, kept deliberately THIN: it triggers and
// points at the MCP tools, it does not restate what codefit already knows. The
// detected language is baked into the example commands so they are copy-paste
// exact. (Industry finding: bloated/LLM-generated skills lower agent success and
// raise cost — density over prose.)
//
// The frontmatter follows the Anthropic Agent Skills spec: name + description.
// The description leads with trigger words so progressive disclosure loads the
// skill only when the task is one codefit audits — endpoints OR a database
// schema. The trigger set is part of the contract, not decoration: a description
// that names only endpoints means a database task never loads the skill at all.
//
// This template is an AGENT-FACING SOURCE, like the MCP tool descriptions in
// internal/mcp/server.go: it is what an agent reads BEFORE choosing a tool, so a
// capability missing here is a capability the agent never learns exists. It fell
// two phases behind once — the DB dimension shipped across v0.2.0–v0.2.5 while
// this template still described only endpoint security. TestSkillNamesEveryRegisteredTool
// (internal/mcp) is the lock: every tool NewServer registers must be named here
// or carry a declared reason for staying out.
var skillTemplate = template.Must(template.New("skill").Parse(`---
name: ` + SkillName + `
description: Audit AI-generated code for security flaws agents miss — IDOR, broken authorization, data over-fetching — and for database schema risks in a Prisma schema or SQL migrations. Use when reviewing API endpoints or route handlers, auditing a database schema, or before merging AI-written backend code.
---

# codefit — audit AI-generated code

To audit code you MUST call ` + "`codefit-scan-all`" + ` FIRST. Do NOT audit by reading files
manually — codefit maps the surface (IDOR, broken authorization, over-fetching) and your
job is to REASON over its output, not to replace it. It does NOT call an LLM — you do the
reasoning. Drive it through its MCP tools.

## When to run
- Reviewing or merging AI-generated endpoints, route handlers, controllers, or queries.
- Before a commit touching authorization, resource access, or which fields are returned.

## Run a full scan
Call ` + "`codefit-scan-all`" + ` with:
- ` + "`root`" + `: absolute path to the repo root
- ` + "`language`" + `: "{{.Language}}"

## Narrow a scan to the files you changed (optional)
Add ` + "`changed_files`" + ` — the project-relative paths you touched — to audit ONLY those files.
codefit never asks git which files changed; you already know, you just wrote them. Omitting
it (or passing an empty list) is a FULL audit — an empty list is never "audit nothing".
ALWAYS read the ` + "`scope`" + ` block before you report anything:
- ` + "`mode: \"partial\"`" + ` means ` + "`findings`" + `, ` + "`score`" + ` and ` + "`blocked`" + ` describe ONLY the audited files.
  ` + "`blocked: false`" + ` then means "no critical in THAT SLICE", not "no critical" — say that to the
  human instead of reporting a clean project. ` + "`blocked: true`" + ` needs no caveat.
- ` + "`unmatched`" + ` lists paths the audit never reached (deleted, not an auditable extension, inside
  a skipped dir). Those were NOT audited, which is NOT the same as audited and clean.
- ` + "`by_dimension.db`" + ` is ` + "`null`" + ` (not measured) unless a configured schema path is in scope.
  Null is not 100 — the schema was not looked at.
- A partial scan is for a fast pass on a change. NEVER prune the baseline after one (below).

## Read the three buckets in the response
- ` + "`actionable`" + ` — a concern AND a local gap, full detail included. Act on these.
- ` + "`resolved_clean`" + ` — codefit found the expected check present locally (an
  authorization check / field selection) — no gap here, but it did NOT verify the check
  is sufficient. Named only; no immediate action.
- ` + "`frontier_pending`" + ` — codefit could NOT conclude locally (the check crosses
  files it does not follow). Named only. THESE NEED YOU. Not concluded is not clean —
  never discard them.

## Follow a frontier endpoint
For each ` + "`frontier_pending`" + ` entry, call ` + "`codefit-scan-endpoint`" + ` with:
- ` + "`root`" + `, ` + "`language`" + `: "{{.Language}}"
- ` + "`file`" + `: the endpoint's file path
Then reason over the returned surface to decide if the concern is real. Each surface
item names WHAT to check (an id→resource lookup, an authz decision, a returned object)
and WHERE — codefit does not decide if it's a vuln; you do.

## Audit the database schema
Call ` + "`codefit-scan-db`" + ` with ` + "`{root, language: \"{{.Language}}\"}`" + ` to audit the schema on its
own. ` + "`codefit-scan-all`" + ` runs it too, but ONLY when ` + "`database.schema_paths`" + ` is set in
` + "`.codefit.yaml`" + `. It reads a Prisma ` + "`schema.prisma`" + ` OR a directory of SQL-DDL/Flyway
migrations (PostgreSQL, MySQL, SQL Server), and classifies the schema as transactional or
a warehouse — on a warehouse it adds the star-schema/SCD, columnar-index and partitioning
checks.
- A table with no primary key is deterministic. Everything else — foreign keys with no
  covering index, duplicate/redundant indexes, sensitive columns in the clear, risks in
  procedure/trigger bodies — is SURFACE: yours to reason about, exactly like the endpoint
  surface.
- ` + "`measured: false`" + ` means codefit could NOT read the schema (none configured, no parser for
  it, or every source unreadable). That is NOT clean — read the ` + "`note`" + `, fix the config, re-run.

## Dependencies and declared limits
- ` + "`codefit-check-cves`" + ` ` + "`{root}`" + ` — known CVEs in dependencies via OSV.dev, read from exact
  lockfile versions. ` + "`codefit-scan-all`" + ` does NOT run it; call it separately.
- ` + "`codefit-coverage`" + ` ` + "`{language}`" + ` — what codefit audits deterministically, what it maps as
  surface, and what it does NOT cover. Read it before telling the human something is out
  of scope; do not assume the boundary.

## Baseline (don't re-review what's already tracked)
codefit records the audited surface in ` + "`.codefit-baseline`" + ` (committed). ` + "`codefit-scan-all`" + `
manages it and reports a delta — act on what's new:
- Focus on ` + "`baseline.new`" + ` and ` + "`baseline.changed`" + `. ` + "`known`" + ` surface is already tracked — don't re-review it.
- Deterministic findings (confidence 1.0, e.g. a hardcoded secret) are NOT auto-silenced:
  they show on every scan until accepted. ` + "`baseline.affirmations_shown`" + ` counts them.
- To act on items, get their fingerprints from ` + "`codefit-baseline-list`" + ` (use
  ` + "`filter: \"known\"`" + ` for items not yet accepted) — do NOT read the ` + "`.codefit-baseline`" + ` file.
- When the HUMAN decides an item is a false positive or accepted debt, call
  ` + "`codefit-baseline-accept`" + ` with its ` + "`fingerprint`" + ` and the human's reason. ONLY when the
  human said so in the conversation — NEVER accept an item on your own.
- After a refactor, items the fix removed appear in ` + "`baseline.gone_candidates`" + `. Confirm with a
  FULL ` + "`codefit-scan-all`" + ` (no ` + "`changed_files`" + `), then call ` + "`codefit-baseline-prune`" + ` to drop them.
  NEVER prune off a partial scan: a scan that did not open a file has no evidence its items are
  gone, so a partial run never proposes them, and ` + "`codefit-baseline-prune`" + ` takes no
  ` + "`changed_files`" + ` at all — it always re-scans in full. Scanning may be partial; forgetting
  may not.
Narrate every baseline operation to the human (what you accepted or pruned, and why).
codefit never edits code — only its baseline.

## Custom authz helpers (teach codefit your project's auth)
codefit knows NextAuth-style helpers (` + "`getServerSession`" + `, ` + "`auth`" + `) but NOT your project's own
(e.g. ` + "`requirePermission`" + `, ` + "`getCurrentUser`" + `). When many authz items show ` + "`known_authz_detected:\nfalse`" + ` yet they DO call a project auth function, reason about whether that function is a
real authz helper, then PROPOSE registering it to the human ("N items call ` + "`X`" + `, which looks
like your authz helper — register it?"). ONLY when the human approves, call
` + "`codefit-baseline-register-authz-helper`" + ` (` + "`{root, language, helper_name, reason}`" + `); reverse with
` + "`codefit-baseline-unregister-authz-helper`" + `. NEVER register a helper on your own — registering
silences the authz gap on EVERY item that calls it (dozens at once), far more reach than
accepting one. Registering changes a FACT ("this helper is present"), not a verdict: it
clears the AUTHZ gap (permission), NOT the IDOR/ownership gap. An IDOR endpoint stays
actionable — the helper proves "is the caller permitted?", never "does the caller own
THIS resource?". Keep reviewing those.
`))

// RenderSkill renders codefit's SKILL.md for the detected project, baking in the
// language so the example commands are exact.
func RenderSkill(info ProjectInfo) ([]byte, error) {
	lang := info.Language
	if lang == "" {
		lang = "typescript"
	}
	var buf bytes.Buffer
	if err := skillTemplate.Execute(&buf, struct{ Language string }{Language: lang}); err != nil {
		return nil, fmt.Errorf("rendering skill: %w", err)
	}
	return buf.Bytes(), nil
}
