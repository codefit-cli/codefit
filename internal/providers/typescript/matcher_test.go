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
