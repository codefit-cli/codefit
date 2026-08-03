package scaffold_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/scaffold"
)

// committedSkillDir is the ONE skill placement this repository commits. codefit
// dogfoods itself with Claude Code, so `codefit init` writes the skill here.
var committedSkillDir = filepath.Join(".claude", "skills", scaffold.SkillName)

// TestCommittedSkillMatchesRenderedSkill is the drift lock for the only generated
// artifact this repository commits: .claude/skills/codefit/SKILL.md.
//
// Committing a generated file is a promise that the copy in git is what the
// generator produces TODAY. Without a lock that promise decays silently: someone
// edits skillTemplate, ships it, and every agent reading the committed copy is
// taught the previous version of codefit — the exact failure the template's own
// doc comment records (the DB dimension shipped across v0.2.0–v0.2.5 while the
// skill still described only endpoint security).
//
// The test renders through the REAL pipeline — Detect() then RenderSkill() — so a
// change to language detection or to the template shows up here instead of being
// re-implemented (and re-broken) inside the assertion.
func TestCommittedSkillMatchesRenderedSkill(t *testing.T) {
	root := repoRoot(t)

	info, err := scaffold.Detect(root)
	if err != nil {
		t.Fatalf("Detect(%s): %v", root, err)
	}
	// Guard the probe itself: RenderSkill defaults an empty language to
	// "typescript", so a Detect that silently returned nothing would compare this
	// Go repository's skill against a TypeScript render and call the mismatch drift.
	if info.Language == "" {
		t.Fatalf("Detect(%s) returned no language — the probe is broken, not the skill", root)
	}

	want, err := scaffold.RenderSkill(info)
	if err != nil {
		t.Fatalf("RenderSkill: %v", err)
	}

	rel := filepath.Join(committedSkillDir, scaffold.SkillFileName)
	got, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		// Never t.Skip here: a skipped test protects nothing, and the whole point
		// is that the generated file must BE in the tree.
		t.Fatalf("committed skill %s could not be read: %v\n"+
			"    generate it with `go run ./cmd/codefit init` (or `codefit init`) and commit the result", rel, err)
	}

	// Line endings: this repo has no .gitattributes and core.autocrlf=true on
	// Windows, so a Windows checkout can materialize the committed .md with CRLF
	// while RenderSkill always emits LF. Comparing raw bytes would fail on every
	// Windows machine for a reason that has nothing to do with drift, so both
	// sides are normalized to LF. Adding a .gitattributes is a separate,
	// deliberately deferred Windows-portability work unit — not a fix to smuggle
	// in here.
	if !bytes.Equal(normalizeEOL(got), normalizeEOL(want)) {
		t.Errorf("committed skill %s does NOT match what RenderSkill produces today (detected language %q)\n"+
			"    %s\n"+
			"    regenerate it with `go run ./cmd/codefit init` (or `codefit init --non-interactive`) and commit the file\n"+
			"    do NOT hand-edit the committed copy: internal/scaffold/skill.go is the source",
			rel, info.Language, firstDifference(normalizeEOL(got), normalizeEOL(want)))
	}
}

// TestCommittedSkillLivesWhereInitWritesIt keeps the committed path honest. The
// assertion above reads one hardcoded path; if the agent→skill-dir mapping ever
// moves, that path would still hold a stale-but-matching file while `codefit init`
// wrote the real skill somewhere else.
func TestCommittedSkillLivesWhereInitWritesIt(t *testing.T) {
	root := repoRoot(t)

	targets, usedFallback := scaffold.PlacementTargets(root)
	if usedFallback {
		t.Fatalf("PlacementTargets(%s) fell back to the standard location — this repo has .claude/ and CLAUDE.md, so Claude Code must be detected", root)
	}
	for _, tgt := range targets {
		if tgt.SkillDir == committedSkillDir {
			return
		}
	}
	dirs := make([]string, 0, len(targets))
	for _, tgt := range targets {
		dirs = append(dirs, tgt.SkillDir)
	}
	t.Errorf("`codefit init` no longer writes the skill to %s (it writes to %s)\n"+
		"    move the committed skill to the new location and update committedSkillDir, or the committed copy stops being the one agents read",
		committedSkillDir, strings.Join(dirs, ", "))
}

// repoRoot walks up from the test's working directory (internal/scaffold) to the
// module root, found by its go.mod. Walking beats a fixed "../.." because it
// fails loudly instead of quietly auditing the wrong tree.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolving working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above the test's working directory — cannot locate the repository root")
		}
		dir = parent
	}
}

// normalizeEOL collapses CRLF to LF so the comparison measures content, not the
// checkout's line-ending policy.
func normalizeEOL(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// firstDifference describes where the committed file and the render diverge, so
// the failure points at a line instead of "the bytes differ".
func firstDifference(got, want []byte) string {
	gotLines := strings.Split(string(got), "\n")
	wantLines := strings.Split(string(want), "\n")
	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if gotLines[i] != wantLines[i] {
			return "first difference at line " + strconv.Itoa(i+1) +
				":\n      committed: " + gotLines[i] +
				"\n      rendered:  " + wantLines[i]
		}
	}
	return "the files share a common prefix but differ in length: committed has " +
		strconv.Itoa(len(gotLines)) + " lines, the render has " + strconv.Itoa(len(wantLines))
}
