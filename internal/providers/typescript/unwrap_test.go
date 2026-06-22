package typescript_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
)

// TestUnwrapParenthesized verifies that a parenthesized pattern matches the
// inner expression — `(expr)` is semantically `expr`, so unwrap must peel the
// parenthesized_expression wrapper just like program/expression_statement.
//
// This is what lets a rule isolate a sub-node (here an object literal) as a
// pattern: `({__html: $A + $B})` parses as program > expression_statement >
// parenthesized_expression > object, and we want to match the object anywhere
// it appears — e.g. deep inside a JSX element with any number of attributes —
// independent of the wrappers around it. (XSS rule, slice 7.)
func TestUnwrapParenthesized(t *testing.T) {
	rules := compile(t, ruleengine.Rule{
		ID: "T-PAREN", Severity: "high", Dimension: "security",
		Message: "object literal matched through a parenthesized pattern",
		Pattern: "({__html: $A + $B})",
	})

	// The object appears as a plain assignment value — matching must reach it
	// despite the pattern being written parenthesized.
	if got := ruleengine.Match(rules, parseSrc(t, `const z = {__html: a + b}`), "f.ts"); len(got) != 1 {
		t.Fatalf("parenthesized pattern should match the inner object, got %d findings", len(got))
	}

	// A different object shape (no __html / not a concat) must not match.
	if got := ruleengine.Match(rules, parseSrc(t, `const z = {title: a}`), "f.ts"); len(got) != 0 {
		t.Errorf("non-matching object must stay silent, got %d", len(got))
	}
}
