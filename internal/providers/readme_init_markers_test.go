package providers_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// readmeInitMarkerAnchor is the exact prose that introduces README.md's list of
// the marker files `codefit init` looks for. The lock below reads the
// parenthesised group that follows it.
//
// The anchor is prose, not a line number: the line moves with any edit above it,
// and a probe that fails on unrelated edits is a probe that gets deleted. If the
// sentence is reworded so the anchor disappears, the lock FAILS rather than
// silently checking nothing — a marker list README no longer states in this
// shape needs this lock moved with it, not skipped.
const readmeInitMarkerAnchor = "no marker file ("

// backtickedToken matches one `code-spanned` token of README prose.
var backtickedToken = regexp.MustCompile("`([^`]+)`")

// extractParenGroupAfter returns the text between the first "(" of anchor and
// the next ")", across line breaks — README wraps this list over two lines.
func extractParenGroupAfter(readme, anchor string) (string, error) {
	start := strings.Index(readme, anchor)
	if start == -1 {
		return "", fmt.Errorf("README.md has no text containing %q", anchor)
	}
	rest := readme[start+len(anchor):]
	end := strings.Index(rest, ")")
	if end == -1 {
		return "", fmt.Errorf("README.md's %q group is never closed by \")\"", anchor)
	}
	return rest[:end], nil
}

// backtickedTokens returns every `code-spanned` token in s, in order.
func backtickedTokens(s string) []string {
	var out []string
	for _, m := range backtickedToken.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// TestReadmeInitMarkers_MatchTheRegistry locks README.md's hand-written list of
// the marker files `codefit init` looks for against
// registry.InitDetectMarkerFiles() — the SAME query every generated artifact
// (the init report, the .codefit.yaml comment, the skill) already derives its
// marker names from.
//
// Spec R4 requires every marker file named in ANY user-facing text to be
// derived. README.md is user-facing text, and it is the one place a marker name
// cannot be interpolated: markdown has no template. So the derivation is
// enforced from the outside, exactly as
// readme_surface_count_test.go already enforces README's restatements of
// TypeScript's surface reach against typescript.New().Capability().
//
// Without this lock, `codefit init`'s own artifacts would follow a registry
// change while README kept naming the old set — the drift shape this whole
// change exists to remove, reproduced in the file a developer reads FIRST, and
// in the same release whose CHANGELOG claims the list can no longer drift. It
// was measured, not assumed: adding go.work to the registry table left every
// test in internal/providers, internal/scaffold and internal/cli green.
//
// BOTH directions are checked, because a one-directional lock is how the
// reverse drift survives:
//   - a marker ADDED to the registry and not to README (README under-states what
//     codefit detects, so a developer whose project would be detected is told it
//     is not);
//   - a marker README names that the registry does NOT have (README over-states,
//     which is the deleted refusal message's exact defect: it named manifests
//     that resolve nothing).
//
// Deliberately NOT in scope: README's other mentions of `go.mod` (the CVE
// section's "reads exact versions from lockfiles / `go.mod`" and the
// codefit-check-cves tool row). Those name go.mod as a LOCKFILE the CVE check
// parses, not as a detection marker; widening this lock to every occurrence in
// the file would fail on a true statement about a different capability.
func TestReadmeInitMarkers_MatchTheRegistry(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}

	group, err := extractParenGroupAfter(string(raw), readmeInitMarkerAnchor)
	if err != nil {
		t.Fatal(err)
	}
	named := backtickedTokens(group)

	// Positive probe, both halves: a lock that extracted an empty list, or ran
	// against an empty registry, would compare nothing to nothing and pass
	// forever — indistinguishable from agreement.
	if len(named) == 0 {
		t.Fatalf("README.md's marker group after %q names no `code-spanned` file: %q — "+
			"the extraction is broken, so agreement proves nothing", readmeInitMarkerAnchor, group)
	}
	registered := registry.InitDetectMarkerFiles()
	if len(registered) == 0 {
		t.Fatal("registry.InitDetectMarkerFiles() is empty; the comparison below would be vacuous")
	}
	t.Logf("positive probe: README names %v; the registry declares %v", named, registered)

	inRegistry := make(map[string]bool, len(registered))
	for _, m := range registered {
		inRegistry[m] = true
	}
	inReadme := make(map[string]bool, len(named))
	for _, m := range named {
		inReadme[m] = true
	}

	for _, m := range registered {
		if !inReadme[m] {
			t.Errorf("the registry detects marker %q, and README.md's init sentence does not name it "+
				"(it names %v).\nREADME under-states what `codefit init` detects: a developer whose "+
				"project WOULD be detected reads that it is not. Add it to the sentence after %q.",
				m, sortedCopy(named), readmeInitMarkerAnchor)
		}
	}
	for _, m := range named {
		if !inRegistry[m] {
			t.Errorf("README.md's init sentence names marker %q, which no InitDetect-eligible registry "+
				"entry declares (the registry declares %v).\nREADME over-states what `codefit init` "+
				"detects — the deleted refusal message's exact defect: it named manifests that resolve "+
				"nothing.", m, sortedCopy(registered))
		}
	}
}

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}
