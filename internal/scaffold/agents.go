package scaffold

import "path/filepath"

// SkillName is codefit's skill identifier — the directory name and the
// frontmatter `name`, per the Anthropic Agent Skills spec.
const SkillName = "codefit"

// SkillFileName is the file every agent looks for inside a skill directory.
const SkillFileName = "SKILL.md"

// AgentTarget maps a coding agent to how codefit detects its presence in a
// project and where that agent discovers skills.
type AgentTarget struct {
	Name     string   // human-facing name, used in the init report
	Markers  []string // project-relative files OR dirs whose presence signals the agent
	SkillDir string   // project-relative dir that holds the skill's SKILL.md
}

// AgentTargets is the SINGLE source of truth for agent → skill-path mapping.
// To support a new agent or follow a path change, edit this table only.
//
// Markers are files OR dirs: real projects signal an agent with a root config
// FILE (CLAUDE.md, opencode.json) as often as a dir (.claude, .opencode), so we
// accept both. AGENTS.md is deliberately NOT a marker: it is a cross-agent
// convention, so it would falsely match.
//
// NOTE the Codex asymmetry: it is DETECTED by its own marker (.codex, where Codex
// keeps its project config) but the skill is WRITTEN to .agents/skills (the
// standardized path Codex actually reads skills from, per OpenAI's Codex docs).
// Detect-by-X / write-to-Y is deliberate, not a bug — do not "fix" it by making
// them match, or Codex will stop finding the skill.
var AgentTargets = []AgentTarget{
	{Name: "Claude Code", Markers: []string{".claude", "CLAUDE.md"}, SkillDir: skillDir(".claude")},
	{Name: "OpenCode", Markers: []string{".opencode", "opencode.json", "opencode.jsonc"}, SkillDir: skillDir(".opencode")},
	{Name: "Codex", Markers: []string{".codex"}, SkillDir: skillDir(".agents")},
}

// fallbackTarget is used when no known agent is detected: the skill goes to the
// standardized .agents/skills path (read by OpenCode and Codex) so it is not
// lost, and init tells the user to install it manually.
var fallbackTarget = AgentTarget{
	Name:     "standard location",
	SkillDir: skillDir(".agents"),
}

// skillDir builds "<base>/skills/codefit" with OS-correct separators.
func skillDir(base string) string {
	return filepath.Join(base, "skills", SkillName)
}

// DetectAgents returns the known agents present in root, in AgentTargets order.
// An agent is present when any of its markers (a file or a dir) exists.
func DetectAgents(root string) []AgentTarget {
	var found []AgentTarget
	for _, t := range AgentTargets {
		for _, m := range t.Markers {
			if exists(root, m) {
				found = append(found, t)
				break
			}
		}
	}
	return found
}

// PlacementTargets returns where init should write the skill: the detected
// agents, or a single standard-location fallback when none are present.
func PlacementTargets(root string) (targets []AgentTarget, usedFallback bool) {
	if found := DetectAgents(root); len(found) > 0 {
		return found, false
	}
	return []AgentTarget{fallbackTarget}, true
}
