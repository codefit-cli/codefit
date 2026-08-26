package typescript_test

// The structural matcher lives in core/ruleengine, but its tests live here
// because this is where a real parser produces core/syntax.Node values (the Go
// provider does not yet emit syntax.Node). ruleengine never imports a parser;
// these tests feed it real trees.

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/internal/core/syntax"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

func parseSrc(t *testing.T, src string) syntax.Node {
	t.Helper()
	root, err := typescript.New().Parse(providers.SourceFile{Path: "p.ts", Content: []byte(src)})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// call returns the first call_expression in src.
func call(t *testing.T, src string) syntax.Node {
	n := find(parseSrc(t, src), "call_expression")
	if n == nil {
		t.Fatalf("no call_expression in %q", src)
	}
	return n
}

func TestMatchSimplePatternBindsMetavar(t *testing.T) {
	pat := call(t, "foo($X)")
	binds, ok := ruleengine.Matches(pat, call(t, "foo(bar)"))
	if !ok {
		t.Fatal("foo($X) should match foo(bar)")
	}
	if binds["$X"] != "bar" {
		t.Errorf("$X = %q, want bar", binds["$X"])
	}
}

func TestMatchTypeMismatch(t *testing.T) {
	pat := call(t, "foo($X)")
	if _, ok := ruleengine.Matches(pat, call(t, "baz(x)")); ok {
		t.Error("foo($X) must not match a call to baz")
	}
}

func TestMatchLiteralIsExact(t *testing.T) {
	pat := call(t, "md5($X)")
	if _, ok := ruleengine.Matches(pat, call(t, "md5(data)")); !ok {
		t.Error("md5($X) should match md5(data)")
	}
	if _, ok := ruleengine.Matches(pat, call(t, "sha256(data)")); ok {
		t.Error("md5($X) must not match sha256(data)")
	}
}

// TestMatchRepeatedMetavarConsistency is the classic structural-matcher bug:
// the same metavariable used twice must bind to the SAME code, not just bind the
// first occurrence.
func TestMatchRepeatedMetavarConsistency(t *testing.T) {
	pat := call(t, "$X.foo($X)")

	// Both occurrences are the same identifier -> match, $X bound once.
	binds, ok := ruleengine.Matches(pat, call(t, "user.foo(user)"))
	if !ok {
		t.Fatal("$X.foo($X) should match user.foo(user)")
	}
	if binds["$X"] != "user" {
		t.Errorf("$X = %q, want user", binds["$X"])
	}

	// Occurrences differ -> must NOT match (consistency enforced).
	if _, ok := ruleengine.Matches(pat, call(t, "user.foo(other)")); ok {
		t.Error("$X.foo($X) must NOT match user.foo(other) — metavar inconsistency")
	}
}

// ---- unnamed tokens are part of the shape ---------------------------------
//
// THE DEFECT THESE LOCK: matchNode compared node type, then named-child COUNT,
// then children pairwise — and an operator is NOT a named child. Every
// `binary_expression` therefore looked identical through syntax.Node, so the
// pattern `$A + $B` matched `a % b`, and SEC-010 affirmed a SQL-injection
// finding at confidence 1.0 on a modulo. The operator is the whole meaning of
// the construct; a matcher blind to it is not matching a shape, only an arity.
//
// The fix compares the node's SKELETON — its literal tokens with every named
// child blanked out — which recovers the operator from Text() and the children's
// byte ranges without widening syntax.Node (ADR 0003 keeps it minimal).

// binExpr returns the first binary_expression in src.
func binExpr(t *testing.T, src string) syntax.Node {
	t.Helper()
	n := find(parseSrc(t, src), "binary_expression")
	if n == nil {
		t.Fatalf("no binary_expression in %q", src)
	}
	return n
}

func TestMatchOperatorIsPartOfTheShape(t *testing.T) {
	pat := binExpr(t, "$A + $B")

	if _, ok := ruleengine.Matches(pat, binExpr(t, "a + b")); !ok {
		t.Error("$A + $B must match a + b — the declared case")
	}

	// Same node type, same two named children, DIFFERENT operator. Each of these
	// matched before the skeleton check existed.
	for _, src := range []string{"a - b", "a * b", "a % b", "a / b", "a > b", "a && b", "a ?? b"} {
		t.Run(src, func(t *testing.T) {
			if _, ok := ruleengine.Matches(pat, binExpr(t, src)); ok {
				t.Errorf("$A + $B must NOT match %q — concatenation is the only shape it declares", src)
			}
		})
	}
}

// The blindness is not specific to binary operators: `typeof`, `void` and
// `delete` are all unary_expression with one named child, so they were mutually
// indistinguishable for exactly the same reason. This pins the general fix
// rather than a binary_expression special case.
func TestMatchKeywordOperatorIsPartOfTheShape(t *testing.T) {
	unary := func(src string) syntax.Node {
		n := find(parseSrc(t, src), "unary_expression")
		if n == nil {
			t.Fatalf("no unary_expression in %q", src)
		}
		return n
	}
	pat := unary("typeof $X")

	if _, ok := ruleengine.Matches(pat, unary("typeof x")); !ok {
		t.Error("typeof $X must match typeof x")
	}
	for _, src := range []string{"void x", "delete x"} {
		if _, ok := ruleengine.Matches(pat, unary(src)); ok {
			t.Errorf("typeof $X must NOT match %q", src)
		}
	}
}

// The skeleton must compare TOKENS, not formatting: a rule is written with
// canonical spacing and real code is spaced however its formatter likes. If the
// skeleton kept whitespace, `db.query("a"+id)` would go silent — a false
// NEGATIVE in a security rule, which is worse than the false positive being
// fixed.
func TestMatchIgnoresWhitespaceBetweenTokens(t *testing.T) {
	pat := binExpr(t, "$A + $B")
	for _, src := range []string{"a+b", "a  +  b", "a +\n  b"} {
		if _, ok := ruleengine.Matches(pat, binExpr(t, src)); !ok {
			t.Errorf("$A + $B must still match %q — spacing is not shape", src)
		}
	}
}

// Same reason, one step further: Prettier's default trailingComma:"all" writes a
// trailing comma into every multiline argument list, so `arguments` reads "(,)"
// where the pattern reads "()". A trailing separator before a closing delimiter
// is grammatically optional and semantically empty, and treating it as shape
// would silence every call-shaped rule on Prettier-formatted code.
func TestMatchToleratesATrailingSeparator(t *testing.T) {
	pat := call(t, "foo($X)")
	for _, src := range []string{"foo(x,)", "foo(\n  x,\n)"} {
		if _, ok := ruleengine.Matches(pat, call(t, src)); !ok {
			t.Errorf("foo($X) must still match %q — a trailing comma is not shape", src)
		}
	}
	// It is only the TRAILING one that is empty: a real second argument is shape.
	if _, ok := ruleengine.Matches(pat, call(t, "foo(x, y)")); ok {
		t.Error("foo($X) must NOT match foo(x, y)")
	}
}

// A rule pattern is written without a statement terminator; real code has one,
// and lexical_declaration SPANS it, so its skeleton reads "const;" against the
// pattern's "const". Automatic semicolon insertion makes the terminator
// optional, so it is formatting, not shape. This is the exact regression the
// skeleton check caused on first switch-on: SEC-001 and SEC-058 went silent on
// their own declared shape.
func TestMatchToleratesAStatementTerminator(t *testing.T) {
	decl := func(src string) syntax.Node {
		n := find(parseSrc(t, src), "lexical_declaration")
		if n == nil {
			t.Fatalf("no lexical_declaration in %q", src)
		}
		return n
	}
	pat := decl("const $NAME = $VALUE")
	for _, src := range []string{`const apiKey = "sk"`, `const apiKey = "sk";`} {
		if _, ok := ruleengine.Matches(pat, decl(src)); !ok {
			t.Errorf("const $NAME = $VALUE must match %q — a terminator is not shape", src)
		}
	}
	// The declaration KEYWORD, though, is shape: it is the token that says which
	// binding form this is, and it used to be invisible for the same reason the
	// operator was.
	if _, ok := ruleengine.Matches(pat, decl(`let apiKey = "sk";`)); ok {
		t.Error("const $NAME = $VALUE must NOT match a let declaration")
	}
}
