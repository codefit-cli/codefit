package ruleengine_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/internal/core/syntax"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	"github.com/codefit-cli/codefit/rules"
)

// tsParse is the parse function Compile receives in production: the REAL
// TypeScript provider parsing a pattern string as code (typescript.parsePattern
// does exactly this). A hand-built syntax.Node whose HasError() returns true
// would prove nothing here — it would lock a tree the production path cannot
// produce. The whole defect lives in what tree-sitter ACTUALLY returns for a
// broken pattern, so the real parser is the fixture.
//
// The external test package is what lets core reach a provider: production code
// in internal/core must never import internal/providers/<lang>, and `go list
// -deps` (how the layering contract tests measure) does not see external test
// imports. Precedent: internal/core/dbrules/names_test.go.
func tsParse(src string) (syntax.Node, error) {
	return typescript.New().Parse(providers.SourceFile{Path: "pattern.ts", Content: []byte(src)})
}

// brokenPattern is a TypeScript expression with an unbalanced parenthesis. It is
// NOT a parse failure in the usual sense: tree-sitter is error-RECOVERING, so it
// returns a tree (nil error) that merely CONTAINS an ERROR node. That is the
// entire trap — see the precondition test below, which proves it rather than
// assuming it.
const brokenPattern = "eval($X"

// goodPattern is the same rule written correctly, used as the control: the gate
// must reject the broken pattern WITHOUT rejecting a pattern that parses.
const goodPattern = "eval($X)"

// TestCompile_ParseIsErrorRecovering_SoNilErrorIsNotProof pins the precondition
// the whole gate rests on. If tree-sitter ever starts returning a real error for
// this input, the gate below is guarding a case that can no longer occur, and
// this test says so directly instead of staying green over nothing.
func TestCompile_ParseIsErrorRecovering_SoNilErrorIsNotProof(t *testing.T) {
	root, err := tsParse(brokenPattern)
	if err != nil {
		t.Fatalf("parse(%q) returned err=%v; this test's premise is that tree-sitter RECOVERS and "+
			"returns a nil error, which is why a nil error is not proof the pattern parsed", brokenPattern, err)
	}
	if root == nil {
		t.Fatalf("parse(%q) returned a nil node with a nil error", brokenPattern)
	}
	if !root.HasError() {
		t.Fatalf("parse(%q).HasError() = false, want true: HasError() is the ONLY signal that this "+
			"pattern is broken, and it must be present for the compile gate to have anything to read", brokenPattern)
	}

	okRoot, err := tsParse(goodPattern)
	if err != nil {
		t.Fatalf("parse(%q): %v", goodPattern, err)
	}
	if okRoot.HasError() {
		t.Fatalf("parse(%q).HasError() = true, want false: the control pattern must be clean, "+
			"otherwise the gate cannot tell broken from good", goodPattern)
	}
}

// TestCompile_RejectsUnparsablePattern is the defect itself. Before the fix,
// Compile ACCEPTED a rule whose pattern does not parse and returned no error;
// the rule then matched nothing for the entire life of the process. loader.go's
// own doc comment promises the opposite: "a broken rule can never enter the
// engine silently — a silent rule is a silent vulnerability."
//
// Every pattern-bearing operator is covered, because every one of them goes
// through the same parse and every one of them is equally silent when it does
// not parse.
func TestCompile_RejectsUnparsablePattern(t *testing.T) {
	cases := []struct {
		name string
		rule ruleengine.Rule
		// wantInErr is asserted verbatim: the error must LOCATE the failure —
		// name the rule id and quote the offending pattern text — or whoever
		// reads the CI log cannot tell which of N rules broke, nor which of a
		// rule's several patterns.
		wantInErr []string
	}{
		{
			name: "pattern",
			rule: ruleengine.Rule{
				ID: "T-BROKEN-PATTERN", Message: "m", Severity: "high", Dimension: "security",
				Pattern: brokenPattern,
			},
			wantInErr: []string{"T-BROKEN-PATTERN", brokenPattern},
		},
		{
			name: "pattern-either",
			rule: ruleengine.Rule{
				ID: "T-BROKEN-EITHER", Message: "m", Severity: "high", Dimension: "security",
				PatternEither: []string{goodPattern, brokenPattern},
			},
			wantInErr: []string{"T-BROKEN-EITHER", "pattern-either", brokenPattern},
		},
		{
			name: "pattern-not",
			rule: ruleengine.Rule{
				ID: "T-BROKEN-NOT", Message: "m", Severity: "high", Dimension: "security",
				Pattern: goodPattern, PatternNot: brokenPattern,
			},
			wantInErr: []string{"T-BROKEN-NOT", "pattern-not", brokenPattern},
		},
		{
			name: "pattern-inside",
			rule: ruleengine.Rule{
				ID: "T-BROKEN-INSIDE", Message: "m", Severity: "high", Dimension: "security",
				Pattern: goodPattern, PatternInside: brokenPattern,
			},
			wantInErr: []string{"T-BROKEN-INSIDE", "pattern-inside", brokenPattern},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := ruleengine.Compile([]ruleengine.Rule{tc.rule}, tsParse)
			if err == nil {
				t.Fatalf("Compile accepted a rule whose %s does not parse (%d compiled rules, no error). "+
					"The rule would match NOTHING and nobody would ever be told", tc.name, len(compiled))
			}
			if compiled != nil {
				t.Errorf("Compile returned %d rules alongside the error; a failed compile must return no rules", len(compiled))
			}
			for _, want := range tc.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q — the error must locate the failure", err.Error(), want)
				}
			}
		})
	}
}

// TestCompile_AcceptsPatternThatParses is the false-positive guard. A gate that
// rejects everything is not a gate, it is an outage: it would silence every rule
// codefit ships. This is the control run for the test above.
func TestCompile_AcceptsPatternThatParses(t *testing.T) {
	compiled, err := ruleengine.Compile([]ruleengine.Rule{{
		ID: "T-GOOD", Message: "m", Severity: "high", Dimension: "security",
		Pattern: goodPattern,
	}}, tsParse)
	if err != nil {
		t.Fatalf("Compile rejected a pattern that parses cleanly: %v", err)
	}
	if len(compiled) != 1 {
		t.Fatalf("want 1 compiled rule, got %d", len(compiled))
	}

	// ...and it still MATCHES. Compiling is not firing: a gate that quietly
	// dropped the unwrapped node would still satisfy the check above.
	src, err := typescript.New().Parse(providers.SourceFile{Path: "f.ts", Content: []byte("eval(userInput)")})
	if err != nil {
		t.Fatal(err)
	}
	if got := ruleengine.Match(compiled, src, "f.ts"); len(got) != 1 {
		t.Fatalf("want 1 finding from the compiled rule, got %d", len(got))
	}
}

// TestCompile_EveryShippedRuleStillCompiles is the blast-radius check for the
// gate: it runs the REAL embedded rule corpus through the REAL provider parser,
// which is exactly what production does at first use. If the gate rejected a
// rule that ships today, that rule is malformed and has been silently matching
// nothing — a finding to report, never something to silence.
//
// Measured when the gate landed: 9 rules, 15 patterns, 0 rejected.
func TestCompile_EveryShippedRuleStillCompiles(t *testing.T) {
	raw, err := ruleengine.LoadFS(rules.FS, "typescript/security")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("loaded 0 shipped rules: this test would pass by vacuity")
	}
	// Rule by rule, so a failure names the offender instead of the corpus.
	for _, r := range raw {
		if _, err := ruleengine.Compile([]ruleengine.Rule{r}, tsParse); err != nil {
			t.Errorf("shipped rule %s no longer compiles: %v", r.ID, err)
		}
	}
	if _, err := ruleengine.Compile(raw, tsParse); err != nil {
		t.Fatalf("the shipped corpus no longer compiles as a whole: %v", err)
	}
}
