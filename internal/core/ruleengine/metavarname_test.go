package ruleengine_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
)

// SPEC (issue #152) — `metavariable-name`, a constraint that matches an
// identifier by NAME COMPONENT instead of by raw substring.
//
// WHAT IT SOLVES. SEC-001 constrained $NAME with an unanchored regex, so any
// identifier merely CONTAINING a credential word matched: `const tokenizer =
// "whitespace"` was affirmed as a hardcoded secret at confidence 1.0 — the
// loudest and stickiest thing codefit can emit, on a word that happens to
// contain "token". Go had already fixed exactly this (ADR 0075) by matching
// components through internal/core/namematch; TypeScript never got the fix
// because a rule could only express a regex.
//
// WHY A NEW OPERATOR AND NOT A BETTER REGEX. Measured, not assumed: Go's regexp
// is RE2, which has no lookbehind and no lookahead. Anchoring "token" to a
// camelCase component boundary needs to assert what precedes it (`accessToken`
// yes, `subtokenizer` no) and RE2 cannot. Enumerating the case variants instead
// multiplies every alternative by every boundary and is unreadable long before
// it is correct.
//
// WHAT IT RECEIVES. A metavariable name and a VOCABULARY name:
//
//	metavariable-name:
//	  $NAME: credential
//
// Only vocabularies this package can name are accepted, and an unknown one is
// a COMPILE error — the same strict, located contract the rest of Compile
// keeps. A rule that silently ignored an unknown vocabulary would match
// everything and tell nobody.
//
// WHY namematch IS REACHABLE FROM HERE. It lives in internal/core and names no
// provider: it is the shared vocabulary the cross-provider case table binds. The
// rule engine is TypeScript's detection mechanism (ADR 0083), but the words it
// looks for are the same words Go looks for, and that is the whole point — this
// operator is what stops the two from drifting again.
//
// COMPOSITION. A metavariable may carry a name constraint and a regex at once;
// both must hold. And like metavariable-regex, the constraint is consulted AT
// BIND TIME so it steers the object-subset search rather than filtering its
// first answer.
func compileCred(t *testing.T, extra map[string]string) []ruleengine.CompiledRule {
	t.Helper()
	r := ruleengine.Rule{
		ID: "T-NAME", Message: "m", Severity: "high", Dimension: "security",
		PatternEither:    []string{"const $NAME = $VALUE", "({...$REST, $NAME: $VALUE})"},
		MetavariableName: map[string]string{"$NAME": "credential"},
	}
	if extra != nil {
		r.MetavariableRegex = extra
	}
	c, err := ruleengine.Compile([]ruleengine.Rule{r}, tsParse)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return c
}

func TestMetavariableNameMatchesByComponent(t *testing.T) {
	compiled := compileCred(t, nil)
	cases := []struct {
		name string
		src  string
		want int
		why  string
	}{
		{"the reported false positive", `const tokenizer = "whitespace";`, 0,
			"contains \"token\" and is not a credential — this is issue #152 verbatim"},
		{"another substring victim", `const secretariat = "un";`, 0,
			"contains \"secret\""},
		{"a suffixed non-credential", `const passwordless = "on";`, 0,
			"contains \"password\" and means the opposite"},

		{"camelCase component", `const accessToken = "abc";`, 1, "token is a component"},
		{"SCREAMING_SNAKE", `const API_KEY = "abc";`, 1,
			"the exact case ADR 0075 measured: lower(API_KEY) does not contain \"apikey\", so a substring arm alone loses it"},
		{"the bare word", `const credential = "abc";`, 1,
			"the one true positive the substring regex carried and the component set did not, until #152"},
		{"plural", `const secrets = "abc";`, 1, "the plural fold covers it"},

		{"inside a config object", `const cfg = { baseUrl: "u", apiKey: "abc", n: 1 };`, 1,
			"the constraint has to steer the subset search, not filter its first binding"},
		{"a tokenizer inside a config object", `const cfg = { baseUrl: "u", tokenizer: "ws", n: 1 };`, 0,
			"and it must not rescue a non-credential by trying another member"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := tsParse(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(ruleengine.Match(compiled, n, "f.ts")); got != tc.want {
				t.Errorf("findings = %d, want %d — %s", got, tc.want, tc.why)
			}
		})
	}
}

// A name constraint and a regex on the same metavariable must BOTH hold. SEC-001
// needs this: the name says "credential" and a separate regex on $VALUE says
// "a quoted literal". If one silently won, the rule would affirm on half its
// evidence.
func TestMetavariableNameComposesWithRegex(t *testing.T) {
	compiled := compileCred(t, map[string]string{"$VALUE": `^".+"$`})
	for _, tc := range []struct {
		src  string
		want int
		why  string
	}{
		{`const apiKey = "abc";`, 1, "name and value both hold"},
		{`const apiKey = readKey();`, 0, "the name holds and the value does not"},
		{`const tokenizer = "abc";`, 0, "the value holds and the name does not"},
	} {
		n, err := tsParse(tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(ruleengine.Match(compiled, n, "f.ts")); got != tc.want {
			t.Errorf("%s -> %d findings, want %d — %s", tc.src, got, tc.want, tc.why)
		}
	}
}

// An unknown vocabulary is a COMPILE error, located. A rule engine that accepted
// it and matched nothing would be the exact defect the compile gate exists to
// prevent: a rule that loads cleanly and is silent forever.
func TestMetavariableNameRejectsUnknownVocabulary(t *testing.T) {
	_, err := ruleengine.Compile([]ruleengine.Rule{{
		ID: "T-BAD", Message: "m", Severity: "high", Dimension: "security",
		Pattern:          "const $NAME = $VALUE",
		MetavariableName: map[string]string{"$NAME": "no-such-vocabulary"},
	}}, tsParse)
	if err == nil {
		t.Fatal("Compile accepted an unknown vocabulary; the rule would match nothing and nobody would be told")
	}
	for _, want := range []string{"T-BAD", "no-such-vocabulary"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q — it must locate the failure", err.Error(), want)
		}
	}
}
