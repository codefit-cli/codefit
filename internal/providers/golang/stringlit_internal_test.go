package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestStringLitFallsBackToSourceTextWhenALiteralDoesNotUnquote pins the one
// branch of stringLit the real parser never reaches, which is why this test
// builds the node by hand: the parser refuses the literal below, so no Go
// source can carry it to stringLit. The branch exists so that, if a literal the
// parser accepts ever fails strconv.Unquote, SEC-001 answers in the direction
// that does not hide a credential: the source text is returned, it proves no
// shape, and the finding fires.
func TestStringLitFallsBackToSourceTextWhenALiteralDoesNotUnquote(t *testing.T) {
	const undecodable = `"\q"`

	// Verified by content: the parser must refuse this literal, or the
	// hand-built node below stands for something the parser does produce and
	// a real-source test belongs here instead.
	src := "package p\n\nconst signingKey = " + undecodable + "\n"
	if _, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.SkipObjectResolution); err == nil {
		t.Fatalf("the parser accepts %s; drive this case through real source instead of a hand-built node", undecodable)
	}

	lit := &ast.BasicLit{Kind: token.STRING, Value: undecodable}
	got, ok := stringLit(lit)
	if !ok || got != undecodable {
		t.Errorf("stringLit(%s) = (%q, %v), want (%q, true): the source text, so the value is non-empty", undecodable, got, ok, undecodable)
	}
	if !hardcodedSecret("signingKey", lit) {
		t.Errorf("hardcodedSecret(signingKey, %s) = false, want true: an unreadable value proves nothing, so SEC-001 fires", undecodable)
	}
}
