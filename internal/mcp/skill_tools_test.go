package mcp_test

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/mcp"
	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// deliberatelyNotInSkill records, per registered tool the skill does NOT name, the
// reason it stays out. The skill is deliberately thin (density over prose), so
// omitting a tool is a legitimate choice — but it must be a DECLARED one. An
// undeclared omission is how the skill silently fell two phases behind the server:
// the DB dimension shipped across v0.2.0–v0.2.5 while the skill still described
// only endpoint security, so an agent reading it never learned scan-db existed.
var deliberatelyNotInSkill = map[string]string{
	"codefit-scan-security":     "subsumed by codefit-scan-all, which the skill makes the mandatory entry point",
	"codefit-surface-idor":      "low-level {files:[{path,content}]} entry point; the skill drives scan-all/scan-endpoint",
	"codefit-surface-authz":     "low-level {files:[{path,content}]} entry point; the skill drives scan-all/scan-endpoint",
	"codefit-surface-nplus1":    "low-level {files:[{path,content}]} entry point; the skill drives scan-all/scan-endpoint",
	"codefit-surface-overfetch": "low-level {files:[{path,content}]} entry point; the skill drives scan-all/scan-endpoint",
	"codefit-confirm-surface":   "low-level, root-less, non-persisting entry point (stateless, no project identity) — the skill drives codefit-baseline-record-verdict, which persists across audits, instead",
}

// registeredTools returns the tool names the MCP server actually exposes, read
// from a live session rather than from the constant block — the constants declare
// more tools than NewServer registers (Phase 3 names exist already).
func registeredTools(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	st, ct := mcpsdk.NewInMemoryTransports()
	srv := mcp.NewServer()
	go func() { _ = srv.Run(ctx, st) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "skill-drift-check"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	lt, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(lt.Tools))
	for _, tool := range lt.Tools {
		names = append(names, tool.Name)
	}
	if len(names) == 0 {
		t.Fatal("no tools registered — the probe itself is broken")
	}
	return names
}

// toolDescription returns the registered description for name, read from a
// live session (mirrors registeredTools' approach) so this test tracks what
// the server actually SERVES an agent, not a copy of the string.
func toolDescription(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	st, ct := mcpsdk.NewInMemoryTransports()
	srv := mcp.NewServer()
	go func() { _ = srv.Run(ctx, st) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "doc-lock-check"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	lt, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range lt.Tools {
		if tool.Name == name {
			return tool.Description
		}
	}
	t.Fatalf("tool %q is not registered — the probe itself is broken", name)
	return ""
}

// TestScanAllDescriptionDocumentsSecurityNotMeasured is the D3b agent-facing
// doc lock: codefit-scan-all's description AND the generated skill must each
// tell the agent that by_dimension.security can be null, parallel to the
// existing by_dimension.db sentence — otherwise an agent reading either
// source has no way to know a null security score is a documented state
// (no provider for the language), not a codefit wiring bug.
func TestScanAllDescriptionDocumentsSecurityNotMeasured(t *testing.T) {
	desc := toolDescription(t, string(mcp.ToolScanAll))
	if !strings.Contains(desc, "by_dimension.security") {
		t.Errorf("codefit-scan-all's description must mention by_dimension.security, got:\n%s", desc)
	}

	skill, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: "typescript"})
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}
	body := string(skill)
	if !strings.Contains(body, "by_dimension.security") {
		t.Errorf("the generated skill must mention by_dimension.security, got:\n%s", body)
	}
}

// recognizedAuthzHelpersDescPhrase is the caption sentence's own load-bearing
// substring — locked separately from the bare field name so this test proves
// the description states the "codefit's knowledge, not the project's actual
// guarding" caveat, not merely that the JSON field name appears somewhere in
// the prose.
const recognizedAuthzHelpersDescPhrase = "codefit's KNOWLEDGE, not the project's actual guarding"

// TestScanAllAndScanSecurityDescriptionsDeclareRecognizedAuthzHelpers is the
// spec's "Tool descriptions declare the fields exist" requirement, made
// checkable: an agent reading codefit-scan-all's or codefit-scan-security's
// registered description, BEFORE it ever sees a response, must already be
// told that a low or zero recognized_authz_helpers count is codefit's own
// knowledge gap, not a statement that the project is unguarded — the same
// distinction issue #148 exists because SEC-001 crossed once, at the response
// level, without a description warning it off first.
func TestScanAllAndScanSecurityDescriptionsDeclareRecognizedAuthzHelpers(t *testing.T) {
	for _, name := range []string{string(mcp.ToolScanAll), string(mcp.ToolScanSecurity)} {
		desc := toolDescription(t, name)
		if !strings.Contains(desc, "recognized_authz_helpers") {
			t.Errorf("%s's description must mention recognized_authz_helpers, got:\n%s", name, desc)
		}
		if !strings.Contains(desc, recognizedAuthzHelpersDescPhrase) {
			t.Errorf("%s's description must state that a low/zero count reflects codefit's knowledge, "+
				"not the project's actual guarding (%q), got:\n%s", name, recognizedAuthzHelpersDescPhrase, desc)
		}
	}

	skill, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: "typescript"})
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}
	body := string(skill)
	if !strings.Contains(body, "recognized_authz_helpers") {
		t.Errorf("the generated skill must mention recognized_authz_helpers, got:\n%s", body)
	}
}

// TestSkillNamesEveryRegisteredTool is the drift lock between the two agent-facing
// sources: the tools the server registers and the skill that teaches them. Every
// registered tool must either be named in the skill or carry a declared reason for
// staying out.
func TestSkillNamesEveryRegisteredTool(t *testing.T) {
	skill, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: "typescript"})
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}
	body := string(skill)

	for _, name := range registeredTools(t) {
		named := strings.Contains(body, name)
		reason, declared := deliberatelyNotInSkill[name]

		switch {
		case named && declared:
			t.Errorf("%s is named in the skill but still listed as deliberately omitted — drop the stale allowlist entry", name)
		case !named && !declared:
			t.Errorf("%s is registered by the MCP server but the skill never names it, and no reason is declared:\n"+
				"    teach it in internal/scaffold/skill.go, or declare why it stays out in deliberatelyNotInSkill", name)
		case !named && strings.TrimSpace(reason) == "":
			t.Errorf("%s is omitted from the skill with an empty reason — declare why", name)
		}
	}
}

// TestSkillNamesEveryRegisteredTool_ForEveryLanguage widens
// TestSkillNamesEveryRegisteredTool's drift lock to every REGISTERED
// language, not just the TypeScript fixture — the derived category slot
// (D5) narrows per language, but the tool-naming floor must not narrow with
// it. TestSkillNamesEveryRegisteredTool and deliberatelyNotInSkill
// themselves stay untouched.
func TestSkillNamesEveryRegisteredTool_ForEveryLanguage(t *testing.T) {
	registered := registeredTools(t)
	for _, e := range registry.All() {
		t.Run(e.Canonical, func(t *testing.T) {
			skill, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: e.Canonical})
			if err != nil {
				t.Fatalf("RenderSkill(%s): %v", e.Canonical, err)
			}
			body := string(skill)
			for _, name := range registered {
				named := strings.Contains(body, name)
				reason, declared := deliberatelyNotInSkill[name]
				switch {
				case named && declared:
					t.Errorf("%s: %s is named in the skill but still listed as deliberately omitted", e.Canonical, name)
				case !named && !declared:
					t.Errorf("%s: %s is registered by the MCP server but the skill never names it, and no reason is declared", e.Canonical, name)
				case !named && strings.TrimSpace(reason) == "":
					t.Errorf("%s: %s is omitted from the skill with an empty reason", e.Canonical, name)
				}
			}
		})
	}
}

// TestSkillOmissionAllowlistHasNoGhosts keeps the allowlist honest in the other
// direction: an entry for a tool the server no longer registers would silently
// excuse a future tool that reuses the name.
func TestSkillOmissionAllowlistHasNoGhosts(t *testing.T) {
	registered := make(map[string]bool)
	for _, name := range registeredTools(t) {
		registered[name] = true
	}
	for name := range deliberatelyNotInSkill {
		if !registered[name] {
			t.Errorf("deliberatelyNotInSkill names %q, which the server does not register — remove the stale entry", name)
		}
	}
}

// No agent-facing prose may promise that recording a verdict KEEPS an item
// visible. Recording creates no acknowledgement — that is true and it is the
// point — but the inference "therefore the item still appears on every scan"
// is FALSE: that is the baseline's DETERMINISTIC rule, and a reasoned item is
// SURFACE, which the baseline marks known and stops surfacing on its own
// (package baseline's safeguard, predating agent verdicts).
//
// The claim was written once and copied to four places — the package doc, the
// generated skill, this tool's description and its response note — because each
// author read the neighbouring bullet instead of the one that applied. Three of
// them are what an agent reads BEFORE it ever calls the tool, so a false
// promise here is believed, not checked. The response note is measured against
// a real scan-all cycle by
// TestRecordVerdict_NoteClaimsNoMoreVisibilityThanTheNextScanDelivers; prose
// cannot be measured, so it is locked here instead.
func TestAgentFacingProseDoesNotPromiseVerdictsKeepItemsVisible(t *testing.T) {
	forbidden := []string{
		"actionable on every scan",
		"appears on every scan",
		"keeps appearing on every scan",
		"remove the item from view",
	}

	skill, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{
		"generated skill": string(skill),
		"codefit-baseline-record-verdict description": toolDescription(t, "codefit-baseline-record-verdict"),
	}

	// Positive control: the probe must be able to find something, or a clean
	// result would be indistinguishable from a broken search (CLAUDE.md).
	for name, text := range sources {
		if !strings.Contains(text, "codefit-baseline-accept") {
			t.Fatalf("probe broken: %s does not even mention codefit-baseline-accept", name)
		}
	}

	for name, text := range sources {
		for _, claim := range forbidden {
			if strings.Contains(text, claim) {
				t.Errorf("%s promises %q. Recording a verdict does not control visibility — "+
					"say it creates no acknowledgement, not that the item stays on screen.", name, claim)
			}
		}
	}
}
