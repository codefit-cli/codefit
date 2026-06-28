package scaffold_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/scaffold"
	"gopkg.in/yaml.v3"
)

// renderSkill renders the skill for a typescript project and splits it into its
// YAML frontmatter and markdown body.
func renderSkill(t *testing.T, info scaffold.ProjectInfo) (front map[string]any, body string) {
	t.Helper()
	out, err := scaffold.RenderSkill(info)
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "---\n") {
		t.Fatalf("skill must start with YAML frontmatter, got:\n%s", s)
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		t.Fatalf("skill frontmatter is not closed with ---, got:\n%s", s)
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &front); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}
	return front, rest[end+len("\n---\n"):]
}

func tsInfo() scaffold.ProjectInfo {
	return scaffold.ProjectInfo{Name: "bitacora", Language: "typescript", Framework: "next", ORM: "prisma"}
}

// The frontmatter must satisfy the Anthropic Agent Skills spec: name + description
// required, name == the skill/dir identifier.
func TestSkillFrontmatterValid(t *testing.T) {
	front, _ := renderSkill(t, tsInfo())

	name, _ := front["name"].(string)
	if name != scaffold.SkillName {
		t.Errorf("frontmatter name = %q, want %q", name, scaffold.SkillName)
	}
	desc, _ := front["description"].(string)
	if desc == "" {
		t.Errorf("frontmatter description is required and must be non-empty")
	}
	if len(desc) > 1024 {
		t.Errorf("description length = %d, must be <= 1024 (spec limit)", len(desc))
	}
	// The trigger words must lead the description for progressive disclosure.
	low := strings.ToLower(desc)
	for _, kw := range []string{"audit", "security", "idor"} {
		if !strings.Contains(low, kw) {
			t.Errorf("description should contain trigger word %q, got %q", kw, desc)
		}
	}
}

func TestSkillBodyHasLoopEssentials(t *testing.T) {
	_, body := renderSkill(t, tsInfo())

	for _, must := range []string{
		"codefit-scan-all",
		"codefit-scan-endpoint",
		"actionable",
		"resolved_clean",
		"frontier_pending",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("skill body is missing %q", must)
		}
	}
	// The golden rule learned in dogfooding: frontier is not clean.
	if !strings.Contains(strings.ToLower(body), "not concluded") {
		t.Errorf("skill body must carry the golden rule that 'not concluded' is not 'clean'")
	}
}

// The detected language must be baked into the example commands so they are
// copy-paste exact for this project.
func TestSkillBakesLanguage(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	if !strings.Contains(body, `"typescript"`) {
		t.Errorf("skill body must bake the detected language into the commands, got:\n%s", body)
	}
}

// The skill must teach the baseline loop, including the human-only safeguard for
// accept and the rule that deterministic findings are not auto-silenced.
func TestSkillTeachesBaselineLoop(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	for _, must := range []string{
		"codefit-baseline-list",
		"codefit-baseline-accept",
		"codefit-baseline-prune",
		".codefit-baseline",
		"gone_candidates",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("skill must mention %q", must)
		}
	}
	low := strings.ToLower(body)
	if !strings.Contains(low, "never accept an item on your own") {
		t.Errorf("skill must carry the human-only safeguard for accept")
	}
	if !strings.Contains(low, "not auto-silenced") {
		t.Errorf("skill must say deterministic findings are not auto-silenced")
	}
}

// The skill must be IMPERATIVE about using codefit rather than auditing by hand —
// density of signal, so even a small model triggers the tool instead of reading
// files manually.
func TestSkillIsImperative(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	low := strings.ToLower(body)
	if !strings.Contains(low, "must call") || !strings.Contains(low, "codefit-scan-all") {
		t.Errorf("skill must imperatively tell the agent to call codefit-scan-all first, got:\n%s", body)
	}
	if !strings.Contains(low, "do not audit by reading files") && !strings.Contains(low, "not audit by reading files") {
		t.Errorf("skill must tell the agent NOT to audit by reading files manually, got:\n%s", body)
	}
}

// The skill must teach the custom authz-helper registration, with the human-only
// safeguard and the fact-not-verdict nuance (clears authz, not IDOR/ownership).
func TestSkillTeachesCustomAuthzHelpers(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	low := strings.ToLower(body)
	if !strings.Contains(body, "codefit-baseline-register-authz-helper") {
		t.Errorf("skill must mention the register-authz-helper tool")
	}
	if !strings.Contains(low, "never register a helper") {
		t.Errorf("skill must carry the human-only safeguard for registering helpers")
	}
	if !strings.Contains(low, "ownership") {
		t.Errorf("skill must say registering clears authz but not the IDOR/ownership gap, got:\n%s", body)
	}
}

// RenderSkill is exported and may be called with no detected language; it must
// still produce valid, exact commands (defaulting the baked language).
func TestSkillEmptyLanguageDefaults(t *testing.T) {
	_, body := renderSkill(t, scaffold.ProjectInfo{Name: "x"})
	if !strings.Contains(body, `"typescript"`) {
		t.Errorf("empty language must default the baked command language, got:\n%s", body)
	}
}

// HONESTY: resolved_clean must NOT claim codefit "verified it clean" (overstates
// the guarantee). It says the expected check is present locally but its
// sufficiency was not verified — same nuance as the verification facts.
func TestSkillResolvedCleanDoesNotOverpromise(t *testing.T) {
	_, body := renderSkill(t, tsInfo())
	low := strings.ToLower(body)
	if strings.Contains(low, "verified it clean") || strings.Contains(low, "verified clean") {
		t.Errorf("resolved_clean must not over-promise 'verified clean':\n%s", body)
	}
	if !strings.Contains(low, "present") || !strings.Contains(low, "sufficient") {
		t.Errorf("resolved_clean must say the check is present but not verified sufficient, got:\n%s", body)
	}
}
