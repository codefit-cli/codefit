package typescript_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
)

// TestPatternInside is the convergence checkpoint: pattern-inside is implemented
// purely from syntax.Node byte ranges (containment), so it must discriminate
// correctly over the REAL gotreesitter AST. The same base pattern (`secret`)
// matches twice; pattern-inside (`dangerous($X)`) must keep only the occurrence
// whose byte range is contained in a node matching the inside pattern.
//
// Note: pattern-inside's *expressiveness* is bounded by the no-ellipsis subset —
// e.g. "inside a class with any method" is not expressible because a bare `$M`
// parses as a field, not a method (verified). The byte-range *mechanism* below
// is exact regardless; rules that need richer context wait for ellipsis (YAGNI).
func TestPatternInside(t *testing.T) {
	rules := compile(t, ruleengine.Rule{
		ID: "T-SECRET-IN-DANGER", Severity: "high", Dimension: "security",
		Message:       "secret used inside a dangerous() call",
		Pattern:       "secret",
		PatternInside: "dangerous($X)",
	})

	src := "dangerous(secret)\nsafe(secret)"
	finds := ruleengine.Match(rules, parseSrc(t, src), "f.ts")
	if len(finds) != 1 {
		t.Fatalf("want 1 match (only the secret inside dangerous()), got %d: %+v", len(finds), finds)
	}
	if finds[0].Line != 1 {
		t.Errorf("kept finding should be on line 1 (inside dangerous), got line %d", finds[0].Line)
	}
}

// TestPatternInsideNoContext: with no enclosing match for the inside pattern,
// nothing is reported even though the base pattern matches.
func TestPatternInsideNoContext(t *testing.T) {
	rules := compile(t, ruleengine.Rule{
		ID: "T-SECRET-IN-DANGER", Severity: "high", Dimension: "security", Message: "x",
		Pattern:       "secret",
		PatternInside: "dangerous($X)",
	})
	if got := ruleengine.Match(rules, parseSrc(t, "safe(secret)"), "f.ts"); len(got) != 0 {
		t.Errorf("secret outside any dangerous() must not match, got %d", len(got))
	}
}
