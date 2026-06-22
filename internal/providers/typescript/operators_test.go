package typescript_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
)

func countMatches(t *testing.T, rules []ruleengine.CompiledRule, src string) int {
	t.Helper()
	return len(ruleengine.Match(rules, parseSrc(t, src), "f.ts"))
}

func TestPatternEither(t *testing.T) {
	rules := compile(t, ruleengine.Rule{
		ID: "T-WEAKHASH", Severity: "medium", Dimension: "security", Message: "weak hash",
		PatternEither: []string{"md5($X)", "sha1($X)"},
	})
	if n := countMatches(t, rules, "md5(data)"); n != 1 {
		t.Errorf("md5: want 1, got %d", n)
	}
	if n := countMatches(t, rules, "sha1(data)"); n != 1 {
		t.Errorf("sha1: want 1, got %d", n)
	}
	if n := countMatches(t, rules, "sha256(data)"); n != 0 {
		t.Errorf("sha256 should not match either branch, got %d", n)
	}
}

func TestPatternNot(t *testing.T) {
	rules := compile(t, ruleengine.Rule{
		ID: "T-EXEC", Severity: "high", Dimension: "security", Message: "dynamic call",
		Pattern:    "exec($X)",
		PatternNot: `exec("ls")`, // a constant-arg exec is excluded
	})
	if n := countMatches(t, rules, "exec(userInput)"); n != 1 {
		t.Errorf("exec(userInput): want 1, got %d", n)
	}
	if n := countMatches(t, rules, `exec("ls")`); n != 0 {
		t.Errorf(`exec("ls") should be excluded by pattern-not, got %d`, n)
	}
}
