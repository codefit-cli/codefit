package scaffold_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/scaffold"
	"gopkg.in/yaml.v3"
)

// floorTokens are the trigger words that must survive in EVERY registered
// language's rendered skill description, regardless of how narrow that
// language's declared surface categories are — the DB trigger domain
// (language-independent by ADR 0018, so true for any registered language)
// and the code trigger domain plus the audit framing (D5). Narrowing the
// derived category slot must never narrow these.
var floorTokens = []string{
	"database", "prisma", "sql migrations", // DB trigger domain
	"api endpoints", "route handlers", "reviewing", "merging", // code trigger domain
	"audit", "ai-generated code", "security", // framing
}

// TestSkillDescription_FloorHoldsForEveryRegisteredLanguage is D5's floor
// check: for every registered language, rendered via the REAL RenderSkill
// (never a hand-built struct), the frontmatter description must still carry
// every floor token — a description narrowed too far means progressive
// disclosure loads NO skill at all, a failure nobody observes.
func TestSkillDescription_FloorHoldsForEveryRegisteredLanguage(t *testing.T) {
	for _, e := range registry.All() {
		t.Run(e.Canonical, func(t *testing.T) {
			out, err := scaffold.RenderSkill(scaffold.ProjectInfo{Name: "x", Language: e.Canonical})
			if err != nil {
				t.Fatalf("RenderSkill(%s): %v", e.Canonical, err)
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
			var front map[string]any
			if err := yaml.Unmarshal([]byte(rest[:end]), &front); err != nil {
				t.Fatalf("frontmatter is not valid YAML: %v", err)
			}
			desc, _ := front["description"].(string)
			low := strings.ToLower(desc)
			for _, tok := range floorTokens {
				if !strings.Contains(low, tok) {
					t.Errorf("%s: description is missing floor token %q, got:\n%s", e.Canonical, tok, desc)
				}
			}
		})
	}
}
