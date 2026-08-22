package scaffold

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// categoryPhrases maps each surface.ProviderCategories entry to the human
// phrase the skill's DERIVED slot renders for it. Locked exhaustive over
// surface.ProviderCategories, both directions, by
// category_phrase_test.go's C2b — a category added to the vocabulary with no
// phrase here is a hard test failure, not a blank spot in the agent's skill.
var categoryPhrases = map[surface.Category]string{
	surface.CategoryIDOR:      "IDOR",
	surface.CategoryAuthz:     "broken authorization",
	surface.CategoryOverfetch: "data over-fetching",
	surface.CategoryNPlus1:    "N+1 query patterns",
}

// surfaceClause renders D5's derived slot: a comma-joined list of phrases for
// the given declared categories, walked in surface.ProviderCategories order
// (deterministic — never map iteration order). Empty when declared has no
// provider-vocabulary category — the caller omits the clause WHOLE (never an
// empty dash pair), which is why this returns "" rather than a placeholder.
func surfaceClause(declared []surface.Category) string {
	set := make(map[surface.Category]bool, len(declared))
	for _, c := range declared {
		set[c] = true
	}
	var phrases []string
	for _, c := range surface.ProviderCategories {
		if set[c] {
			phrases = append(phrases, categoryPhrases[c])
		}
	}
	return strings.Join(phrases, ", ")
}

// hasCategory reports whether declared contains want.
func hasCategory(declared []surface.Category, want surface.Category) bool {
	for _, c := range declared {
		if c == want {
			return true
		}
	}
	return false
}

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
description: Audit AI-generated code for security flaws agents miss{{if .SurfaceClause}} — {{.SurfaceClause}} —{{end}} and for database schema risks in a Prisma schema or SQL migrations. Use when reviewing API endpoints or route handlers, auditing a database schema, or before merging AI-written backend code.
---

# codefit — audit AI-generated code
{{if .Detected}}
To audit code you MUST call ` + "`codefit-scan-all`" + ` FIRST. Do NOT audit by reading files
manually — codefit maps the surface{{if .SurfaceClause}} ({{.SurfaceClause}}){{end}} and your
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
- ` + "`by_dimension.security`" + ` is ` + "`null`" + ` (not measured) when no security provider resolves
  for ` + "`language`" + ` — the ` + "`security`" + ` section's ` + "`note`" + ` explains why (the db
  dimension may still be measured on its own). Null is not 100 — the code was not looked at.
- A partial scan is for a fast pass on a change. NEVER prune the baseline after one (below).

## Read ` + "`summary`" + ` as PER-DIMENSION counts, never as the project's state
Every count declares which dimension it counted:
- ` + "`summary.security`" + ` — ` + "`endpoints`" + `, ` + "`deterministic_findings`" + `, ` + "`surface_items`" + `,
  ` + "`certain_concerns`" + `. Security only.
- ` + "`summary.db`" + ` — ` + "`schema_sources`" + ` (how many schema sources were read),
  ` + "`deterministic_findings`" + `, ` + "`surface_items`" + `. The database dimension only.
- ` + "`summary.totals`" + ` — the roll-up of ONLY what means the same in both:
  ` + "`deterministic_findings`" + ` and ` + "`surface_items`" + `. Never endpoints — a table has no route.

A sub-block is ` + "`null`" + ` when that dimension was NOT measured. **Null is not zero**: zero
means codefit looked and found nothing; null means nobody looked. So
` + "`summary.security: null`" + ` is never a clean security result, and
` + "`summary.db: null`" + ` is never a clean schema.

NEVER report the project as clean off ` + "`summary.security`" + ` alone. A schema-heavy project
can show ` + "`summary.security.surface_items: 0`" + ` while ` + "`summary.db.surface_items`" + ` holds dozens
of open questions. Read every non-null sub-block, and ` + "`summary.totals`" + ` for the whole.

The counts are the RAW population, taken BEFORE the baseline filter — that is why one can
exceed what the buckets and ` + "`db.surface`" + ` actually list. The difference is the surface the
baseline already tracks, not a contradiction; ` + "`summary.note`" + ` says so in the response.

## Read the three buckets in the response
Every bucket NAMES its endpoints; none of them inlines the concern text. That is on
purpose — inlining it made ` + "`codefit-scan-all`" + ` too big to return on a real project.
- ` + "`actionable`" + ` — a concern AND a local gap. Act on these. Each entry carries what you
  need to RANK it without fetching: ` + "`concerns`" + `, ` + "`categories`" + `, ` + "`gaps`" + ` (hardest kind
  first), ` + "`highest_certainty`" + `, ` + "`has_affirmation`" + `.
- ` + "`resolved_clean`" + ` — codefit found the expected check present locally (an
  authorization check / field selection) — no gap here, but it did NOT verify the check
  is sufficient. No immediate action.
- ` + "`frontier_pending`" + ` — codefit could NOT conclude locally (the check crosses
  files it does not follow). THESE NEED YOU. Not concluded is not clean —
  never discard them.

` + "`deterministic_concerns`" + ` is the ONE thing that comes back in full, on the ` + "`actionable`" + `
entry that has it: a deterministic finding is a fact codefit already concluded, not a
question. Never spend a second call to re-read one.

## Fetch an endpoint's full concerns
Pick the endpoints worth pursuing from the summaries, then for each call
` + "`codefit-scan-endpoint`" + ` with:
- ` + "`root`" + `, ` + "`language`" + `: "{{.Language}}"
- ` + "`file`" + `: the endpoint's file path
It re-runs the same stateless analysis, so what you get back is exactly the signals and
` + "`reason_to_review`" + ` that ` + "`codefit-scan-all`" + ` left out. Then reason over that surface to
decide if the concern is real. Each surface item names WHAT to check (an id→resource
lookup, an authz decision, a returned object) and WHERE — codefit does not decide if
it's a vuln; you do.

## Check the response's ` + "`budget`" + ` block
` + "`codefit-scan-all`" + ` writes to a declared byte budget so it always RETURNS. Read
` + "`budget`" + ` before you conclude anything about the project:
- ` + "`withheld: 0`" + ` — you have every endpoint codefit classified.
- ` + "`withheld: N`" + ` — N endpoints are NOT in the lists. Each bucket's ` + "`count`" + ` is still
  the complete number and its ` + "`withheld`" + ` says how many are missing, so what you have
  is a PREFIX of ` + "`budget.ordering`" + `, not the whole audit. Say so to the human, and
  re-run narrowed with ` + "`changed_files`" + ` to reach the rest. Never report a withheld
  endpoint as clean — it was not shown, which is not the same as audited and fine.

## Audit the database schema
Call ` + "`codefit-scan-db`" + ` with ` + "`{root, language: \"{{.Language}}\"}`" + ` to audit the schema on its
own. ` + "`codefit-scan-all`" + ` runs it too, but ONLY when ` + "`database.schema_paths`" + ` is set in
` + "`.codefit.yaml`" + `. It reads a Prisma ` + "`schema.prisma`" + ` OR a directory of SQL-DDL/Flyway
migrations (PostgreSQL, MySQL, SQL Server), and classifies the schema as transactional or
a warehouse — on a warehouse it adds the star-schema/SCD, columnar-index and partitioning
checks.
{{- if .HasSchemaPaths}}
This project's config carries that key: ` + "`schema_paths`" + ` names ` + "`{{.SchemaPath}}`" + `, so the
dimension is ON. ` + "`codefit init`" + ` wrote it because it PROVED it — a config WITHOUT the key
means codefit could not prove one, never that it did not look.
{{- end}}
- A table with no primary key is deterministic. Everything else — foreign keys with no
  covering index, duplicate/redundant indexes, sensitive columns in the clear, risks in
  procedure/trigger bodies — is SURFACE: yours to reason about, exactly like the endpoint
  surface.
- ` + "`measured: false`" + ` means codefit could NOT read the schema (none configured, no parser for
  it, or every source unreadable). That is NOT clean — read the ` + "`note`" + `, fix the config, re-run.
- ` + "`surface`" + ` (both here and in ` + "`codefit-scan-all`" + `'s ` + "`db`" + ` bucket) is a light INDEX —
  ` + "`id`" + `, ` + "`category`" + `, ` + "`file`" + `, ` + "`line`" + `, ` + "`fingerprint`" + `, ` + "`structural_facts`" + ` — never the
  question text. It is always COMPLETE: ` + "`count`" + ` is the total classified and ` + "`withheld`" + ` is
  always 0 (there is no ranking axis across db surface's disjoint categories to withhold
  by; ` + "`withheld_note`" + ` says so). Read a NAMED item's full detail — the snippet and the
  actual ` + "`reason_to_review`" + ` question — by calling ` + "`codefit-scan-db`" + ` again with
  ` + "`{root, language, detail: [id, ...]}`" + `: many ids in one call, each returned byte-identical
  to the full item. An id that matches nothing comes back in ` + "`unrecognized`" + ` with a note —
  codefit is stateless and cannot tell whether the id never existed or the schema changed
  between calls, never an empty success. ` + "`codefit-scan-all`" + `'s ` + "`db`" + ` bucket carries no
  detail of its own; fetch it through ` + "`codefit-scan-db`" + `. Once you ask for detail, the
  response declares its own size the same way ` + "`codefit-coverage`" + ` does: ` + "`bytes`" + ` covers
  the index PLUS whatever detail you asked for, ` + "`index_bytes`" + ` is the index's own share, and
  ` + "`over_budget: true`" + ` still comes back complete, never truncated.

## Dependencies and declared limits
- ` + "`codefit-check-cves`" + ` ` + "`{root}`" + ` — known CVEs in dependencies via OSV.dev, read from exact
  lockfile versions. ` + "`codefit-scan-all`" + ` does NOT run it; call it separately.
- ` + "`codefit-coverage`" + ` ` + "`{language}`" + ` — what codefit audits deterministically, what it maps as
  surface, and what it does NOT cover. Read it before telling the human something is out
  of scope; do not assume the boundary. For ` + "`{{.Language}}`" + ` today: {{.SurfaceReach}}
  It answers with an INDEX, not with prose: every entry as ` + "`{id, claim, status, has_detail}`" + `,
  and it withholds NOTHING from that index (` + "`withheld`" + ` is always 0 and says so). To read one
  entry in full, call it again with ` + "`detail: [\"<id>\"]`" + ` — MANY ids in one call, e.g.
  ` + "`{language: \"typescript\", detail: [\"DB-050\", \"db.sqlddl-dialect-limits\"]}`" + `. Rule ids
  (` + "`DB-050`" + `, ` + "`DW-011`" + `, ` + "`SEC-001`" + `) are entry ids, so a rule you saw in a finding is askable
  by name. An id that matches nothing is NAMED back to you as unrecognized, never answered
  with an empty success. Ask for the ids you need rather than all of them: the response
  declares its own ` + "`bytes`" + ` and tells you when the detail you asked for crossed the budget —
  complete either way, never truncated.

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

## Closing the loop (persist what you reasoned)
After you reason about a surface item (from ` + "`codefit-scan-*`" + ` or ` + "`codefit-surface-*`" + `) and
reach a verdict, call ` + "`codefit-baseline-record-verdict`" + ` (` + "`{root, language, verdicts:[{surface_id, category, file, line, verdict, reasoning, confidence, severity?}]}`" + `)
so the answer accumulates across audits instead of dying in this conversation. It
re-validates each verdict against a FRESH re-analysis first: a verdict whose item no
longer exists there is REFUSED and named in the response, never silently dropped.
- **Recording a verdict never silences the item — only a human can, via
  ` + "`codefit-baseline-accept`" + `.** ` + "`vulnerable`" + ` may be recorded freely: it only adds alarm, the
  safe direction. ` + "`not_vulnerable`" + ` is a recommendation ON THE RECORD, never a
  dismissal: it creates no acknowledgement and moves the item's visibility in NEITHER
  direction — the baseline decides that exactly as it did before you recorded anything.
- Two agents disagreeing on the same item is not an error: BOTH verdicts are kept, and
  the item is flagged in conflict for a human to look at. Never treat a later verdict as
  overriding an earlier one.
- ` + "`reasoning`" + ` is prose ABOUT the item, committed to a shared file. NEVER quote the
  source line, a secret, or another project's schema identifier in it — cite the FORM
  of the issue, never the identifier.

## Custom authz helpers (teach codefit your project's auth)
Read ` + "`security.recognized_authz_helpers`" + ` / ` + "`recognized_authz_helpers_note`" + ` (in
` + "`codefit-scan-all`" + `'s response) directly for what codefit ALREADY recognizes for this
language — do not count ` + "`known_authz_detected: false`" + ` items by hand to find out.
codefit knows NextAuth-style helpers (` + "`getServerSession`" + `, ` + "`auth`" + `) but NOT your project's own
(e.g. ` + "`requirePermission`" + `, ` + "`getCurrentUser`" + `). When many authz items show ` + "`known_authz_detected:\nfalse`" + ` yet they DO call a project auth function, reason about whether that function is a
real authz helper, then PROPOSE registering it to the human ("N items call ` + "`X`" + `, which looks
like your authz helper — register it?"). ONLY when the human approves, call
` + "`codefit-baseline-register-authz-helper`" + ` (` + "`{root, language, helper_name, reason}`" + `); reverse with
` + "`codefit-baseline-unregister-authz-helper`" + `. NEVER register a helper on your own — registering
silences the authz gap on EVERY item that calls it (dozens at once), far more reach than
accepting one. Registering changes a FACT ("this helper is present"), not a verdict: it
clears the AUTHZ gap (permission){{if .HasIDOR}}, NOT the IDOR/ownership gap. An IDOR endpoint stays
actionable — the helper proves "is the caller permitted?", never "does the caller own
THIS resource?". Keep reviewing those.{{else}} only. Keep reviewing anything else this project's surface maps.{{end}}
{{else}}
codefit found no language provider it can audit in this project. ` + "`codefit init`" + ` looks
for {{.Markers}}
directly under the project root, and none of them resolved one here.

**Read this as a declared gap, not a clean result.** ` + "`codefit-scan-security`" + `,
` + "`codefit-scan-all`" + ` and the ` + "`codefit-surface-*`" + ` tools resolve NO provider for this project,
so no code here is scanned at all. Nobody looked — that is not the same as looked and
found nothing. NEVER report this project as audited on the strength of codefit having run.

Two things a human can do about it, which are theirs to decide, not yours:
- If codefit does register this project's language, ` + "`.codefit.yaml`" + `'s ` + "`project.language`" + `
  is the editing surface — set it and re-run.
- In a monorepo, run ` + "`codefit init`" + ` again per sub-project root. Detection is a plain check
  of the root directory; it does not recurse.

## What DOES still audit this project: the database schema
Call ` + "`codefit-scan-db`" + ` with ` + "`{root}`" + `. The schema parser resolves from the INPUT's shape —
a Prisma ` + "`schema.prisma`" + ` OR a directory of SQL-DDL/Flyway migrations (PostgreSQL, MySQL,
SQL Server) — never from the project's language, which is why this dimension reaches a
project no code provider does.

It reads exactly what ` + "`database.schema_paths`" + ` names in ` + "`.codefit.yaml`" + `.
{{- if .HasSchemaPaths}}
This project's config DOES carry that key: ` + "`schema_paths`" + ` names ` + "`{{.SchemaPath}}`" + `, so the
dimension is ON and ` + "`codefit-scan-db`" + ` audits that schema as written.

` + "`codefit init`" + ` wrote it because it PROVED it — it read that directory exactly as an audit
would and reconstructed a schema from it. It writes nothing it cannot prove, so a config
WITHOUT the key means codefit could not prove one, never that it did not look.
{{- else}}
Generated configs
for a project like this carry NO such key, so as written nothing is audited. Adding it is
what turns the dimension on:

    database:
      type: "postgresql"   # postgresql | mysql | sqlserver
      schema_paths:
        - "db/migrations"
{{- end}}

Leaving ` + "`type:`" + ` out is allowed and has a consequence: codefit parses the DDL as
PostgreSQL without saying it chose, so a MySQL or SQL Server schema is silently
mis-parsed and every finding after it reasons over a schema that does not exist.
sqlite is the one value codefit refuses outright rather than guessing at.

- A table with no primary key is deterministic. Everything else — foreign keys with no
  covering index, duplicate/redundant indexes, sensitive columns in the clear, risks in
  procedure/trigger bodies — is SURFACE: yours to reason about, not codefit's to decide.
- ` + "`measured: false`" + ` means codefit could NOT read the schema (none configured, no parser for
  it, or every source unreadable). That is NOT clean — read the ` + "`note`" + `, fix the config, re-run.
- ` + "`surface`" + ` is a light INDEX — ` + "`id`" + `, ` + "`category`" + `, ` + "`file`" + `, ` + "`line`" + `,
  ` + "`fingerprint`" + `, ` + "`structural_facts`" + ` — never the question text, and always COMPLETE:
  ` + "`count`" + ` is the total classified and ` + "`withheld`" + ` is always 0 (` + "`withheld_note`" + ` says
  why: no ranking axis across db surface's disjoint categories). Read a NAMED item's full
  detail by calling again with ` + "`{root, detail: [id, ...]}`" + `: many ids in one call, each
  byte-identical to the full item; an unmatched id is NAMED in ` + "`unrecognized`" + ` with a note,
  never an empty success. Once you ask for detail, the response declares its own size:
  ` + "`bytes`" + ` covers the index PLUS the detail you asked for, ` + "`index_bytes`" + ` is the index's
  own share, and ` + "`over_budget: true`" + ` still comes back complete.

## Dependencies
` + "`codefit-check-cves`" + ` ` + "`{root}`" + ` — known CVEs in dependencies via OSV.dev, read from exact
lockfile versions. It resolves no language provider either, so it works here too.

## When you report
Name which dimensions were measured and which were not. The honest summary for this
project is "codefit audited the schema; it scanned no code, because it resolves no
provider for this language" — never a bare "codefit found no issues".
{{end}}`))

// RenderSkill renders codefit's SKILL.md for the detected project, baking in the
// language so the example commands are exact. The DERIVED slot (surface
// category phrases) is read from the registry's real Capability() for this
// language — never a hardcoded per-language string (D5); an unregistered
// language declares no categories, so the clause is omitted whole.
//
// There is NO language fallback. This function used to bake
// language: "typescript" whenever Language was empty, which fabricated a
// language in the FIRST artifact an agent reads — its examples are copy-paste
// instructions, so the agent would be told to scan a Java repository as
// TypeScript. When no language was detected the skill renders its undetected
// body instead: what codefit looked for, what therefore does not run, and the
// one dimension that still does.
//
// The frontmatter description is deliberately NOT gated. Progressive disclosure
// loads the skill from the description alone, and it already names the database
// and schema triggers; narrowing it for an undetected project would mean a
// schema task never loads the skill at all — the agent would not see a smaller
// skill, it would see none.
// firstSchemaPath is the path the skill NAMES, slash-spelled exactly as the
// config beside it spells the same path. An agent that reads a different
// spelling here than the config carries would be told to audit a path that does
// not exist.
func firstSchemaPath(info ProjectInfo) string {
	if len(info.SchemaPaths) == 0 {
		return ""
	}
	return filepath.ToSlash(info.SchemaPaths[0])
}

func RenderSkill(info ProjectInfo) ([]byte, error) {
	var declared []surface.Category
	if e, ok := registry.ByName(info.Language); ok {
		declared = e.New(nil).Capability().Surface
	}
	data := struct {
		Language      string
		Detected      bool
		Markers       string
		SurfaceClause string
		HasIDOR       bool
		// SurfaceReach is R4/"Also in scope"
		// (docs/specs/declared-partial-language-exposure.md): the N-of-M
		// surface reach claim the skill states before the agent audits
		// anything, derived from surface.DeriveCoverage against the same
		// locked vocabulary the response-level not-covered statement uses —
		// never a hardcoded per-language sentence.
		SurfaceReach string
		// HasSchemaPaths and SchemaPath describe the config written BESIDE this
		// skill by the same run. They exist because the skill's claim that "a
		// project like this carries NO such key" became false the moment codefit
		// learned to detect SQL migration directories: the first artifact an
		// agent reads would tell it to expect no schema key in a config that has
		// one. They gate BODY prose only — never the frontmatter description.
		HasSchemaPaths bool
		SchemaPath     string
	}{
		Language:       info.Language,
		Detected:       info.Detected(),
		Markers:        markerList(),
		SurfaceClause:  surfaceClause(declared),
		HasIDOR:        hasCategory(declared, surface.CategoryIDOR),
		SurfaceReach:   surface.DeriveCoverage(declared).Note,
		HasSchemaPaths: len(info.SchemaPaths) > 0,
		SchemaPath:     firstSchemaPath(info),
	}
	var buf bytes.Buffer
	if err := skillTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering skill: %w", err)
	}
	return buf.Bytes(), nil
}
