package providers_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// readmeCategoryMarkers gives, per surface.ProviderCategories entry, the
// substring(s) README.md's prose uses to name it. More than one alternative
// exists per category because README restates TypeScript's surface reach in
// two different spots with different phrasing (a table cell using short
// dimension names, a bullet using longer prose) — the lock below checks BOTH
// restatements, so both need to be found by SOME accepted spelling. This map
// is exhaustive over surface.ProviderCategories on purpose: a category added
// to the vocabulary with no marker here fails this test loudly instead of
// being silently skipped.
var readmeCategoryMarkers = map[surface.Category][]string{
	surface.CategoryIDOR:      {"IDOR"},
	surface.CategoryAuthz:     {"authz", "authoriz"},
	surface.CategoryOverfetch: {"over-fetching"},
	surface.CategoryNPlus1:    {"N+1"},
}

// repoRoot walks up from the test's working directory to the module root,
// found by its go.mod — the same idiom internal/scaffold's
// skill_committed_test.go already uses to locate committed files from a test.
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

// extractLineContaining returns the first line of readme containing anchor,
// erroring loudly if no such line exists — a missing anchor means README's
// structure moved and this lock needs to move with it, not silently pass.
// extractLineContainingUnder is extractLineContaining scoped to one section, and
// the scoping is the point: the unscoped form returns the FIRST match anywhere in
// the file, so a summary row added above the fold silently shadows the row this
// control means to read. That happened — a reach table near the top matched
// "| **TypeScript" and the control reddened over a row that was never its subject,
// while the real Supported-languages row still named every category. A control an
// unrelated edit can point at the wrong line is not measuring what it claims.
func extractLineContainingUnder(readme, heading, anchor string) (string, error) {
	_, after, found := strings.Cut(readme, heading)
	if !found {
		return "", fmt.Errorf("README.md has no heading %q — this control's anchor moved and it is now reading nothing", heading)
	}
	line, err := extractLineContaining(after, anchor)
	if err != nil {
		return "", fmt.Errorf("under %q: %w", heading, err)
	}
	return line, nil
}

func extractLineContaining(readme, anchor string) (string, error) {
	for _, line := range strings.Split(readme, "\n") {
		if strings.Contains(line, anchor) {
			return line, nil
		}
	}
	return "", fmt.Errorf("README.md has no line containing %q", anchor)
}

// extractBulletBlock returns the anchor line plus every following line up to
// (not including) the next bullet or a blank line — the shape of README's
// "Works today" bullet list, where one bullet's prose wraps across lines.
func extractBulletBlock(readme, anchor string) (string, error) {
	lines := strings.Split(readme, "\n")
	start := -1
	for i, line := range lines {
		if strings.Contains(line, anchor) {
			start = i
			break
		}
	}
	if start == -1 {
		return "", fmt.Errorf("README.md has no bullet containing %q", anchor)
	}
	end := start
	for end+1 < len(lines) {
		next := strings.TrimSpace(lines[end+1])
		if next == "" || strings.HasPrefix(next, "- ") {
			break
		}
		end++
	}
	return strings.Join(lines[start:end+1], " "), nil
}

// TestReadmeSurfaceCategoryCount_MatchesTypeScriptCapability locks README.md's
// THREE restatements of TypeScript's surface-mapping reach against the real
// declaration (typescript.New().Capability().Surface) instead of a literal
// count: for every category TypeScript actually declares, its marker must
// appear in the "Works today" bullet, the "Supported languages" table row,
// AND the "What codefit covers today" bullet (line ~214, "Surface mapping —
// the agent reasons."). This is the class of drift that let README say
// "three categories" for seven weeks after N+1 shipped as TypeScript's fourth
// (v0.2.2) — a category added to Capability().Surface with no matching
// README marker now fails this test for a real reason, not because hardcoded
// numbers moved together.
//
// The third site was found by sdd-verify (obs #1467, SUGGESTION): it was a
// known gap, not a live defect (the site was already accurate), but the lock
// existed to catch exactly this class of drift and was not exhaustive over
// every restatement in the file. Extended here rather than left undeclared.
func TestReadmeSurfaceCategoryCount_MatchesTypeScriptCapability(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs", "guide", "languages.md"))
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	readme := string(raw)

	bullet, err := extractBulletBlock(readme, "**Surface mapping** —")
	if err != nil {
		t.Fatal(err)
	}
	row, err := extractLineContainingUnder(readme, "## Supported languages", "| **TypeScript")
	if err != nil {
		t.Fatal(err)
	}
	coversBullet, err := extractBulletBlock(readme, "**Surface mapping — the agent reasons.**")
	if err != nil {
		t.Fatal(err)
	}

	cap := typescript.New().Capability()
	for _, cat := range surface.ProviderCategories {
		if !containsCategory(cap.Surface, cat) {
			continue
		}
		markers, ok := readmeCategoryMarkers[cat]
		if !ok {
			t.Fatalf("readmeCategoryMarkers has no entry for %q — add one before this lock can check it", cat)
		}
		if !containsAny(bullet, markers) {
			t.Errorf("README's \"Surface mapping\" bullet does not name category %q (any of %v): %s", cat, markers, bullet)
		}
		if !containsAny(row, markers) {
			t.Errorf("README's Supported-languages TypeScript row does not name category %q (any of %v): %s", cat, markers, row)
		}
		if !containsAny(coversBullet, markers) {
			t.Errorf("README's \"What codefit covers today\" Surface-mapping bullet does not name category %q (any of %v): %s", cat, markers, coversBullet)
		}
	}
}

func containsCategory(list []surface.Category, cat surface.Category) bool {
	for _, c := range list {
		if c == cat {
			return true
		}
	}
	return false
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
