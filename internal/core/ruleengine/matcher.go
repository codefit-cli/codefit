package ruleengine

import (
	"regexp"

	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// metavarRe matches a Semgrep metavariable: $ followed by an uppercase name.
// In TS/JS a metavariable parses as an ordinary identifier whose text is "$X",
// so the matcher recognizes metavariables by the bound node's text.
var metavarRe = regexp.MustCompile(`^\$[A-Z][A-Z0-9_]*$`)

// Matches reports whether the (already-parsed) pattern node matches the code
// node structurally, returning the metavariable bindings (name -> matched
// source text). It is the core of the matcher and is exported so each operator
// can be tested against real trees.
//
// Matching is purely structural — no ellipsis (PRD §17 subset): a metavariable
// matches any node (and binds, consistently across repeats); otherwise node
// types must be equal and named children must match pairwise; leaves compare by
// text.
func Matches(pattern, code syntax.Node) (map[string]string, bool) {
	binds := map[string]string{}
	if !matchNode(pattern, code, binds) {
		return nil, false
	}
	return binds, true
}

func matchNode(pattern, code syntax.Node, binds map[string]string) bool {
	if pattern == nil || code == nil {
		return pattern == nil && code == nil
	}

	// Metavariable: binds to any node, but consistently — a repeated metavariable
	// must bind to code with the same text.
	if name, ok := metavarName(pattern); ok {
		text := string(code.Text())
		if prev, seen := binds[name]; seen {
			return prev == text
		}
		binds[name] = text
		return true
	}

	if pattern.Type() != code.Type() {
		return false
	}

	// Leaf (identifier, literal, operator token): compare text exactly.
	if pattern.NamedChildCount() == 0 {
		return string(pattern.Text()) == string(code.Text())
	}

	if pattern.NamedChildCount() != code.NamedChildCount() {
		return false
	}
	for i := 0; i < pattern.NamedChildCount(); i++ {
		if !matchNode(pattern.NamedChild(i), code.NamedChild(i), binds) {
			return false
		}
	}
	return true
}

// metavarName returns the metavariable name if the node is one (an identifier
// whose text is "$NAME").
func metavarName(n syntax.Node) (string, bool) {
	if n.NamedChildCount() != 0 {
		return "", false
	}
	text := string(n.Text())
	if metavarRe.MatchString(text) {
		return text, true
	}
	return "", false
}
