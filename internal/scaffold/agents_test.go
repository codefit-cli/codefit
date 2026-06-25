package scaffold_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/scaffold"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", d, err)
		}
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", rel, err)
	}
}

func names(targets []scaffold.AgentTarget) []string {
	out := make([]string, len(targets))
	for i, t := range targets {
		out[i] = t.Name
	}
	return out
}

func TestDetectAgentsByMarkerDir(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, ".claude", ".opencode")

	got := names(scaffold.DetectAgents(root))
	if len(got) != 2 {
		t.Fatalf("detected agents = %v, want Claude Code + OpenCode", got)
	}
}

// Real projects (e.g. Bitácora) mark agent presence with a config FILE at the
// root, not a dir: CLAUDE.md for Claude Code, opencode.json for OpenCode. The
// skill must still land in that agent's skills dir.
func TestDetectAgentsByMarkerFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "CLAUDE.md", "# project memory\n")
	writeFile(t, root, "opencode.json", "{}\n")

	got := names(scaffold.DetectAgents(root))
	if len(got) != 2 {
		t.Fatalf("detected agents = %v, want Claude Code + OpenCode from file markers", got)
	}
	targets := scaffold.DetectAgents(root)
	for _, tg := range targets {
		if tg.Name == "Claude Code" && tg.SkillDir != filepath.FromSlash(".claude/skills/codefit") {
			t.Errorf("Claude Code skill dir = %q", tg.SkillDir)
		}
		if tg.Name == "OpenCode" && tg.SkillDir != filepath.FromSlash(".opencode/skills/codefit") {
			t.Errorf("OpenCode skill dir = %q", tg.SkillDir)
		}
	}
}

// Codex is detected by its own marker (.codex) but the skill is written to
// .agents/skills — the asymmetry must hold (see agents.go for the why).
func TestCodexDetectWriteAsymmetry(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, ".codex")

	targets := scaffold.DetectAgents(root)
	if len(targets) != 1 || targets[0].Name != "Codex" {
		t.Fatalf("detected = %v, want exactly Codex", names(targets))
	}
	if want := filepath.FromSlash(".agents/skills/codefit"); targets[0].SkillDir != want {
		t.Errorf("Codex skill dir = %q, want %q (detect .codex, write .agents)", targets[0].SkillDir, want)
	}
}

func TestPlacementFallbackWhenNoAgents(t *testing.T) {
	root := t.TempDir() // no agent marker dirs

	targets, usedFallback := scaffold.PlacementTargets(root)
	if !usedFallback {
		t.Errorf("usedFallback = false, want true when no agents are present")
	}
	if len(targets) != 1 {
		t.Fatalf("fallback targets = %v, want exactly one standard location", names(targets))
	}
	if want := filepath.FromSlash(".agents/skills/codefit"); targets[0].SkillDir != want {
		t.Errorf("fallback skill dir = %q, want %q", targets[0].SkillDir, want)
	}
}

func TestPlacementUsesDetectedAgents(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, ".claude")

	targets, usedFallback := scaffold.PlacementTargets(root)
	if usedFallback {
		t.Errorf("usedFallback = true, want false when an agent is present")
	}
	if len(targets) != 1 || targets[0].Name != "Claude Code" {
		t.Fatalf("targets = %v, want Claude Code", names(targets))
	}
}
