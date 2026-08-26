package ruleengine

import (
	"regexp"
	"strings"

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
// types must be equal, the node's own literal tokens must agree (its [skeleton],
// which is what makes an operator visible), and named children must match
// pairwise; leaves compare by text.
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

	// The node's own tokens must agree too, not just its children. Without this
	// the operator is invisible and `$A + $B` matches `a % b` — see [skeleton].
	if !sameSkeleton(pattern, code) {
		return false
	}

	for i := 0; i < pattern.NamedChildCount(); i++ {
		if !matchNode(pattern.NamedChild(i), code.NamedChild(i), binds) {
			return false
		}
	}
	return true
}

// sameSkeleton reports whether two nodes are built from the same literal tokens.
//
// It fails OPEN: if either skeleton cannot be read, the test is skipped and the
// nodes are treated as compatible. A matcher that starts REJECTING on input it
// does not understand turns rules silent, and a silent security rule is a silent
// vulnerability — strictly worse than an imprecise one.
func sameSkeleton(pattern, code syntax.Node) bool {
	patternSkel, ok := skeleton(pattern)
	if !ok {
		return true
	}
	codeSkel, ok := skeleton(code)
	if !ok {
		return true
	}
	return patternSkel == codeSkel
}

// skeleton returns a node's literal tokens: its own source with every named
// child's source cut out, then normalized. `a + b` has the skeleton "+", and
// `db.query` has ".".
//
// WHY THIS EXISTS. [syntax.Node] deliberately exposes only NAMED children — it
// is implemented per provider and ADR 0003 keeps it minimal and
// parser-agnostic — but an operator token is not a named child. A
// `binary_expression` for `+`, `-`, `*` and `%` therefore has the same type AND
// the same two named children, so comparing type and children pairwise could not
// tell them apart: the pattern `$A + $B` matched `a % b`, and SEC-010 affirmed a
// SQL-injection finding at confidence 1.0 on a modulo. The same blindness made
// `typeof x`, `void x` and `delete x` mutually interchangeable, and `const`
// interchangeable with `let`.
//
// The operator is recoverable from what the interface ALREADY exposes, so no
// provider has to grow a method: Text() is the node's whole source, and each
// named child's [StartByte, EndByte) says which slice of it belongs to that
// child. Blank the children out and what remains is exactly the tokens the
// grammar itself contributed — the node's shape.
//
// ok is false when the node's byte range and Text() disagree, which no provider
// does today; see [sameSkeleton] for why that is not treated as a mismatch.
func skeleton(n syntax.Node) (string, bool) {
	text := n.Text()
	start, end := n.StartByte(), n.EndByte()
	if end-start != len(text) {
		return "", false
	}

	var b strings.Builder
	b.Grow(len(text))
	cursor := 0
	for i := 0; i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if child == nil {
			return "", false
		}
		childStart, childEnd := child.StartByte()-start, child.EndByte()-start
		// Children must be in order and within the parent. Anything else means
		// the byte ranges cannot be trusted to locate the tokens.
		if childStart < cursor || childEnd < childStart || childEnd > len(text) {
			return "", false
		}
		b.Write(text[cursor:childStart])
		cursor = childEnd
	}
	b.Write(text[cursor:])

	return normalizeSkeleton(b.String()), true
}

// normalizeSkeleton drops what is formatting rather than shape.
//
// Whitespace goes because a rule is written with canonical spacing while real
// code is spaced however its formatter likes: keeping it would silence SEC-010
// on `db.query("..."+id)`.
//
// The other two are optional punctuation, both semantically empty in JS/TS and
// both measured here, not imagined:
//
//   - A separator immediately before a closing delimiter. Prettier's default
//     trailingComma:"all" writes one into every multiline argument list, so
//     `arguments` reads "(,)" where a pattern reads "()". Keeping it would turn
//     every call-shaped rule silent on Prettier-formatted code.
//   - A trailing statement terminator. Automatic semicolon insertion makes it
//     optional, and rule patterns are written without one while real code has
//     one — `lexical_declaration` spans it, so `const x = "s";` reads "const;"
//     against a pattern's "const". This one is not theoretical either: it broke
//     SEC-001 and SEC-058 the moment the skeleton test was switched on. Only a
//     TRAILING one is dropped, so the structural semicolons in `for(;;)` survive.
//
// Trading one false positive for a class of false negatives would be a bad
// trade in a security auditor, so both are normalized away rather than treated
// as shape.
//
// These are JS/TS grammar facts living in the core, which is the same call the
// matcher already makes for metavariable syntax and the engine makes for its
// trivial-wrapper node names.
func normalizeSkeleton(s string) string {
	s = strings.Join(strings.Fields(s), "")

	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c == ')' || c == ']' || c == '}') && len(out) > 0 && out[len(out)-1] == ',' {
			out[len(out)-1] = c
			continue
		}
		out = append(out, c)
	}
	if n := len(out); n > 0 && out[n-1] == ';' {
		out = out[:n-1]
	}
	return string(out)
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
