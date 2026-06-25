package scaffold_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/scaffold"
)

// copyTree copies the fixture project into a writable temp dir so Generate can
// write artifacts without polluting testdata.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	return dst
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestGenerateCreatesConfigAndSkill(t *testing.T) {
	root := copyTree(t, sampleNext)
	mkdirs(t, root, ".claude", ".opencode")

	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.ConfigAction != scaffold.ConfigCreated {
		t.Errorf("config action = %q, want created", res.ConfigAction)
	}
	if !fileExists(filepath.Join(root, ".codefit.yaml")) {
		t.Errorf(".codefit.yaml was not written")
	}
	if res.UsedFallback {
		t.Errorf("usedFallback = true, want false (agents present)")
	}
	if len(res.Skills) != 2 {
		t.Fatalf("skills written = %d, want 2 (Claude Code + OpenCode)", len(res.Skills))
	}
	for _, sw := range res.Skills {
		if !fileExists(filepath.Join(root, sw.Path)) {
			t.Errorf("skill for %s not on disk at %s", sw.Agent, sw.Path)
		}
	}
}

func TestGenerateDoesNotOverwriteWithoutPermission(t *testing.T) {
	root := copyTree(t, sampleNext)
	cfgPath := filepath.Join(root, ".codefit.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"existing\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := scaffold.Generate(scaffold.Options{Root: root, OverwriteConfig: false})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.ConfigAction != scaffold.ConfigSkipped {
		t.Errorf("config action = %q, want skipped", res.ConfigAction)
	}
	data, _ := os.ReadFile(cfgPath)
	if string(data) != "version: \"existing\"\n" {
		t.Errorf("existing config was modified: %q", data)
	}
}

func TestGenerateOverwritesWithPermission(t *testing.T) {
	root := copyTree(t, sampleNext)
	cfgPath := filepath.Join(root, ".codefit.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"existing\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := scaffold.Generate(scaffold.Options{Root: root, OverwriteConfig: true})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.ConfigAction != scaffold.ConfigOverwritten {
		t.Errorf("config action = %q, want overwritten", res.ConfigAction)
	}
	data, _ := os.ReadFile(cfgPath)
	if string(data) == "version: \"existing\"\n" {
		t.Errorf("config was not overwritten")
	}
}

func TestGenerateFallbackWhenNoAgents(t *testing.T) {
	root := copyTree(t, sampleNext) // no agent marker dirs

	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !res.UsedFallback {
		t.Errorf("usedFallback = false, want true")
	}
	if len(res.Skills) != 1 {
		t.Fatalf("skills = %d, want 1 fallback", len(res.Skills))
	}
	want := filepath.FromSlash(".agents/skills/codefit/SKILL.md")
	if res.Skills[0].Path != want {
		t.Errorf("fallback skill path = %q, want %q", res.Skills[0].Path, want)
	}
	if !fileExists(filepath.Join(root, want)) {
		t.Errorf("fallback skill not on disk")
	}
}
