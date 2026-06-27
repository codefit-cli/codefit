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
// skill only when the task is about auditing endpoints.
var skillTemplate = template.Must(template.New("skill").Parse(`---
name: ` + SkillName + `
description: Audit AI-generated code for security flaws agents miss — IDOR, broken authorization, data over-fetching. Use when reviewing API endpoints or route handlers, or before merging AI-written backend code.
---

# codefit — audit AI-generated code

codefit runs deterministic security analysis and maps the auditable surface of the
code, then hands it back for you to reason over. It does NOT call an LLM — you do the
reasoning. Drive it through its MCP tools.

## When to run
- Reviewing or merging AI-generated endpoints, route handlers, controllers, or queries.
- Before a commit touching authorization, resource access, or which fields are returned.

## Run a full scan
Call ` + "`codefit-scan-all`" + ` with:
- ` + "`root`" + `: absolute path to the repo root
- ` + "`language`" + `: "{{.Language}}"

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
- After a refactor, items the fix removed appear in ` + "`baseline.gone_candidates`" + `. Confirm with
  ` + "`codefit-scan-all`" + `, then call ` + "`codefit-baseline-prune`" + ` to drop them.
Narrate every baseline operation to the human (what you accepted or pruned, and why).
codefit never edits code — only its baseline.
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
