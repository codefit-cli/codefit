package ruleengine_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// SPEC (issue #169) — OBJECT SUBSET MATCHING, the scoped ellipsis.
//
// WHAT IT SOLVES. A pattern object matched only an object with the SAME number
// of members, because matchNode compares NamedChildCount. So `({$NAME: $VALUE})`
// reached a credential written as `{apiKey: "sk-live"}` and nothing else. A
// census over two real TypeScript projects measured every object holding a
// credential-named string property:
//
//	project A   3 such objects, arity 5, 5, 5
//	project B   1 such object,  arity 3
//
// Four out of four are multi-property. The single-pair pattern reaches NONE of
// them, which is why this is an engine change and not a rule fix.
//
// WHAT IT RECEIVES. A pattern `object` node containing at least one
// `spread_element` whose only child is a metavariable — `{...$REST, $K: $V}`.
//
// WHY THAT SPELLING AND NOT SEMGREP'S `...`. Measured, not chosen: since the
// compile gate (PR #173) a pattern whose tree contains an ERROR node is
// REJECTED, and the TypeScript parser cannot parse a bare ellipsis inside an
// object.
//
//	{..., $NAME: $VALUE}        -> HasError=true, rejected at compile
//	{$NAME: $VALUE, ...}        -> HasError=true, rejected at compile
//	{...$REST, $NAME: $VALUE}   -> parses clean: object[spread_element, pair]
//
// A spread of a metavariable is ordinary TypeScript, so it survives the gate.
// This is a deliberate divergence from Semgrep's surface syntax, declared here
// and in rules/README.md.
//
// WHAT IT RETURNS. Every NON-spread member of the pattern must match SOME member
// of the code object; arity equality is dropped. The code's remaining members —
// including its own real spreads — are ignored.
//
// EDGE CASES THIS HANDLES.
//   - Zero others: `{...$R, $K: $V}` matches `{apiKey: "x"}`. The ellipsis is
//     zero-or-more, so ONE pattern covers arity 1 and arity N. Rules never need
//     a separate single-pair alternative.
//   - Position is irrelevant: object members have no meaningful order, so the
//     match is a subset search, not a prefix walk.
//   - The marker DOES NOT BIND. `$REST` is punctuation, not a capture, so a
//     metavariable-regex on it is meaningless. Declared, not silently ignored.
//   - A pattern WITHOUT a spread keeps EXACT-ARITY semantics, unchanged. The
//     extension is opt-in per pattern, which is what makes it safe to ship: no
//     existing rule changes behavior. Pinned by the last test below.
//   - No match must be reported when the pattern member is simply absent.
func objNode(t *testing.T, src string) syntax.Node {
	t.Helper()
	n, err := tsParse(src)
	if err != nil || n == nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	var found syntax.Node
	var walk func(syntax.Node)
	walk = func(x syntax.Node) {
		if x == nil || found != nil {
			return
		}
		if x.Type() == "object" {
			found = x
			return
		}
		for i := 0; i < x.NamedChildCount(); i++ {
			walk(x.NamedChild(i))
		}
	}
	walk(n)
	if found == nil {
		t.Fatalf("no object node in %q", src)
	}
	return found
}

func TestObjectSubsetMatching(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		code    string
		want    bool
		why     string
	}{
		{
			name:    "the shape the census says is the common one",
			pattern: `({...$REST, apiKey: $V})`,
			code:    `const c = { baseUrl: "u", apiKey: "sk-live", retries: 3 };`,
			want:    true,
			why:     "a credential in a multi-property config object — 4 of 4 measured objects look like this",
		},
		{
			name:    "member first",
			pattern: `({...$REST, apiKey: $V})`,
			code:    `const c = { apiKey: "sk-live", retries: 3 };`,
			want:    true,
			why:     "object members have no meaningful order; the match is a subset search",
		},
		{
			name:    "member last",
			pattern: `({...$REST, apiKey: $V})`,
			code:    `const c = { retries: 3, apiKey: "sk-live" };`,
			want:    true,
			why:     "same, from the other end",
		},
		{
			name:    "zero others — the ellipsis is zero-or-more",
			pattern: `({...$REST, apiKey: $V})`,
			code:    `const c = { apiKey: "sk-live" };`,
			want:    true,
			why:     "one pattern must cover arity 1 and arity N, so rules need no second alternative",
		},
		{
			name:    "the code has its OWN spread",
			pattern: `({...$REST, apiKey: $V})`,
			code:    `const c = { ...defaults, apiKey: "sk-live" };`,
			want:    true,
			why:     "the code's real spread is just another member to ignore",
		},
		{
			name:    "absent member does NOT match",
			pattern: `({...$REST, apiKey: $V})`,
			code:    `const c = { baseUrl: "u", retries: 3 };`,
			want:    false,
			why:     "subset matching must never turn into matching anything",
		},
		{
			name:    "two required members, both present",
			pattern: `({...$REST, apiKey: $V, region: $R2})`,
			code:    `const c = { region: "us", other: 1, apiKey: "k" };`,
			want:    true,
			why:     "every non-spread member must find a home, in any order",
		},
		{
			name:    "greedy assignment would MISS this one",
			pattern: `({...$REST, $K: $V, apiKey: $W})`,
			code:    `const c = { apiKey: "k", other: 1 };`,
			want:    true,
			why: "the FIRST pattern member matches ANY pair, the second only the apiKey pair. " +
				"Taking the first candidate and never reconsidering reports a false miss, so this " +
				"row is the only thing in the table that proves the search backtracks",
		},
		{
			name:    "two required members, one missing",
			pattern: `({...$REST, apiKey: $V, region: $R2})`,
			code:    `const c = { apiKey: "k", other: 1 };`,
			want:    false,
			why:     "one unmatched member sinks the whole pattern",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := ruleengine.Matches(objNode(t, tc.pattern), objNode(t, tc.code))
			if got != tc.want {
				t.Errorf("match = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// The metavariable inside a subset match must BIND, or metavariable-regex (which
// is how SEC-001 decides a value is a string literal) cannot filter it.
func TestObjectSubsetBindsTheValue(t *testing.T) {
	binds, ok := ruleengine.Matches(
		objNode(t, `({...$REST, apiKey: $V})`),
		objNode(t, `const c = { baseUrl: "u", apiKey: "sk-live", retries: 3 };`),
	)
	if !ok {
		t.Fatal("did not match")
	}
	if binds["$V"] != `"sk-live"` {
		t.Errorf("$V = %q, want %q — without the binding metavariable-regex cannot filter", binds["$V"], `"sk-live"`)
	}
}

// THE SAFETY PROPERTY. A pattern with NO spread keeps exact-arity semantics, so
// shipping this changes the behavior of exactly zero existing rules. If this
// test ever goes red, the extension stopped being opt-in and every object rule
// in the tree silently widened.
func TestObjectWithoutSpreadKeepsExactArity(t *testing.T) {
	if _, ok := ruleengine.Matches(
		objNode(t, `({__html: $Q})`),
		objNode(t, `f({__html: h, className: "x"})`),
	); ok {
		t.Error("a spread-less pattern matched a wider object: the extension leaked and is no longer opt-in")
	}
	if _, ok := ruleengine.Matches(
		objNode(t, `({__html: $Q})`),
		objNode(t, `f({__html: h})`),
	); !ok {
		t.Error("a spread-less pattern stopped matching its exact-arity object")
	}
}

// THE CONSTRAINT MUST STEER THE SEARCH, not merely filter its first answer.
//
// A subset match has MANY valid assignments where an exact-arity match had
// exactly one. The matcher used to return the first assignment it found and let
// the rule apply metavariable-regex afterwards — correct while only one
// assignment existed, and wrong the moment the ellipsis shipped: for
// `{...$R, $NAME: $VALUE}` against a config object, the first assignment binds
// $NAME to whatever property happens to come first, the regex rejects it, and
// the credential three properties later is never even considered.
//
// That is under-reporting produced BY the fix for under-reporting, which is why
// it gets its own test at the engine level instead of only in the shape census.
func TestObjectSubsetSearchRespectsMetavariableRegex(t *testing.T) {
	compiled, err := ruleengine.Compile([]ruleengine.Rule{{
		ID: "T-CRED", Message: "m", Severity: "high", Dimension: "security",
		PatternEither:     []string{"({...$REST, $NAME: $VALUE})"},
		MetavariableRegex: map[string]string{"$NAME": "(?i)(apikey|secret|token)", "$VALUE": `^".+"$`},
	}}, tsParse)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	cases := []struct {
		name string
		src  string
		want int
		why  string
	}{
		{
			name: "credential is NOT the first property",
			src:  `const cfg = { baseUrl: "u", apiKey: "sk-live-abc", retries: 3 };`,
			want: 1,
			why:  "the search must keep trying assignments until one satisfies the regex",
		},
		{
			name: "credential is the only property",
			src:  `const cfg = { apiKey: "sk-live-abc" };`,
			want: 1,
			why:  "the ellipsis is zero-or-more",
		},
		{
			name: "credential name but the value is not a literal",
			src:  `const cfg = { baseUrl: "u", apiKey: readKey() };`,
			want: 0,
			why:  "steering the search must not weaken what counts as a secret",
		},
		{
			name: "no credential-named property at all",
			src:  `const cfg = { baseUrl: "u", retries: 3, label: "x" };`,
			want: 0,
			why:  "and it must not degrade into matching any object",
		},
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
