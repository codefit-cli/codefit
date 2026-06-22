package typescript_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/internal/core/syntax"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// parseFn is the parser the ruleengine uses to compile rule patterns: the TS
// provider parsing a pattern string as code.
func parseFn(src string) (syntax.Node, error) {
	return typescript.New().Parse(providers.SourceFile{Path: "pat.ts", Content: []byte(src)})
}

func compile(t *testing.T, rules ...ruleengine.Rule) []ruleengine.CompiledRule {
	t.Helper()
	c, err := ruleengine.Compile(rules, parseFn)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMetavariableRegex(t *testing.T) {
	rules := compile(t, ruleengine.Rule{
		ID: "T-SECRET", Severity: "high", Dimension: "security",
		Message:           "secret-named variable assigned a literal",
		Pattern:           "const $X = $V",
		MetavariableRegex: map[string]string{"$X": "(?i)password|secret|token|apikey"},
	})

	// Matches: variable name hits the regex.
	if got := ruleengine.Match(rules, parseSrc(t, `const password = "abc"`), "f.ts"); len(got) != 1 {
		t.Errorf("want 1 finding for `password`, got %d", len(got))
	}
	// Does not match: name does not hit the regex (false-positive guard).
	if got := ruleengine.Match(rules, parseSrc(t, `const userName = "abc"`), "f.ts"); len(got) != 0 {
		t.Errorf("want 0 findings for `userName`, got %d", len(got))
	}
}
