package ruleengine

import (
	"regexp"

	"github.com/codefit-cli/codefit/internal/core/namematch"
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
// Matching is purely structural: a metavariable matches any node (and binds,
// consistently across repeats); otherwise node types must be equal, the node's
// own literal tokens must agree (its [skeleton], which is what makes an operator
// visible), and named children must match pairwise; leaves compare by text.
//
// Semgrep's general ellipsis is deliberately NOT supported. Objects are the one
// scoped exception — see [objectsubset] — and it is opt-in per pattern, so a
// pattern that does not ask for it matches exactly as described above.
func Matches(pattern, code syntax.Node) (map[string]string, bool) {
	binds := map[string]string{}
	if !matchNode(pattern, code, binds, nil) {
		return nil, false
	}
	return binds, true
}

// constraints are the rule's metavariable-regex, consulted AT BIND TIME rather
// than after the fact. With exact-arity matching a pattern had one possible
// assignment, so filtering the single answer afterwards was equivalent. Subset
// matching has many: for `{...$R, $NAME: $VALUE}` over a config object the first
// assignment binds $NAME to whatever property comes first, and a post-hoc filter
// then rejects the whole match — never reaching the credential three properties
// later. Rejecting a violating binding here instead makes the constraint STEER
// the backtracking search. Bindings never change once committed inside an
// attempt, so rejecting early is safe as well as complete.
//
// nil means unconstrained, which is what `pattern-inside` and `pattern-not` pass:
// they answer a yes/no question about the node and bind nothing the rule reads.
type constraints map[string]constraint

// [metavarname] A constraint on one metavariable: a regex, a name vocabulary, or
// both. When both are present BOTH must hold — a rule that affirmed on half its
// evidence would be worse than one that did not compile.
//
// The vocabulary arm exists because a regex cannot express it. SEC-001 used an
// unanchored regex for $NAME, so `const tokenizer = "whitespace"` was affirmed
// as a hardcoded credential at confidence 1.0 (issue #152) — the loudest and
// stickiest output codefit has, on a word that merely contains "token". Go had
// already fixed this by matching name COMPONENTS through namematch (ADR 0075);
// a rule could not, because a rule could only say "regex".
//
// AND IT IS NOT A REGEX THAT WAS MISSING. Go's regexp is RE2: no lookbehind, no
// lookahead. Anchoring "token" to a component boundary requires asserting what
// precedes it — accessToken yes, subtokenizer no — and RE2 cannot. Enumerating
// the case variants instead multiplies every alternative by every boundary and
// is unreadable long before it is correct.
//
// namematch is reachable from the core because it names no provider: it is the
// shared vocabulary the cross-provider case table binds. The rule engine is
// TypeScript's detection mechanism (ADR 0083), but the WORDS are the same words
// Go looks for, and keeping them in one place is what stops the two drifting
// apart again.
type constraint struct {
	re  *regexp.Regexp
	set map[string]bool // a namematch vocabulary; nil when unconstrained by name
}

func (c constraint) ok(text string) bool {
	if c.re != nil && !c.re.MatchString(text) {
		return false
	}
	if c.set != nil {
		if _, hit := namematch.MatchSet(text, c.set); !hit {
			return false
		}
	}
	return true
}

func matchNode(pattern, code syntax.Node, binds map[string]string, cs constraints) bool {
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
		if c, constrained := cs[name]; constrained && !c.ok(text) {
			return false
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

	// The scoped ellipsis: an object pattern carrying a spread-of-metavariable
	// matches a SUBSET, so arity equality and the skeleton comparison below are
	// both wrong for it — a wider object has more commas. See [objectsubset].
	if pattern.Type() == "object" && hasObjectEllipsis(pattern) {
		return matchObjectSubset(pattern, code, binds, cs)
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
		if !matchNode(pattern.NamedChild(i), code.NamedChild(i), binds, cs) {
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

// [objectsubset] OBJECT SUBSET MATCHING — the deliberately scoped ellipsis.
//
// The matcher compares named-child COUNT, so an object pattern reached only an
// object with the same number of members: `({$K: $V})` saw `{apiKey: "x"}` and
// nothing else. A census over two real TypeScript projects measured every object
// holding a credential-named string property — arities 5, 5, 5 and 3. Four out
// of four are multi-property, so the exact-arity pattern reached NONE of them.
// That is why this is an engine change rather than another rule alternative.
//
// THE SPELLING IS `{...$REST, $K: $V}`, NOT SEMGREP'S `{..., $K: $V}`, and that
// was measured rather than preferred. Since the compile gate a pattern whose
// tree contains an ERROR node is rejected, and the TypeScript parser cannot
// parse a bare ellipsis inside an object literal:
//
//	{..., $NAME: $VALUE}        HasError=true  -> rejected at compile
//	{$NAME: $VALUE, ...}        HasError=true  -> rejected at compile
//	{...$REST, $NAME: $VALUE}   parses clean   -> object[spread_element, pair]
//
// A spread of a metavariable is ordinary TypeScript and survives the gate. The
// divergence from Semgrep's surface syntax is declared in rules/README.md.
//
// SEMANTICS. Every non-ellipsis member of the pattern must match SOME member of
// the code object, in any order — object members carry no meaningful order, so
// this is a subset search and not a prefix walk. Everything else in the code
// object, including its own real spreads, is ignored. The ellipsis is
// zero-or-more, so ONE pattern covers arity 1 and arity N alike and a rule never
// needs a separate single-pair alternative.
//
// THE MARKER DOES NOT BIND. `$REST` is punctuation, not a capture, so a
// metavariable-regex naming it constrains nothing. Declared rather than silently
// ignored.
//
// IT IS OPT-IN PER PATTERN. A pattern without a spread keeps exact-arity
// semantics untouched, which is what makes this safe to ship: no existing rule
// changes behavior. Pinned by TestObjectWithoutSpreadKeepsExactArity — if that
// test ever goes red, every object rule in the tree has silently widened.
//
// The search BACKTRACKS. A greedy assignment can fail where another succeeds,
// because one pattern member may match several code members while another
// matches only one: committing the first candidate permanently reports a false
// miss. Patterns hold a handful of members, so the exhaustive search costs
// nothing. Pinned by the "greedy assignment would MISS this one" census row.
func hasObjectEllipsis(n syntax.Node) bool {
	for i := 0; i < n.NamedChildCount(); i++ {
		if isObjectEllipsis(n.NamedChild(i)) {
			return true
		}
	}
	return false
}

// isObjectEllipsis reports whether n is the ellipsis marker: a spread whose only
// child is a metavariable. A spread of anything else (`...defaults`) is an
// ordinary member and must still be matched literally.
func isObjectEllipsis(n syntax.Node) bool {
	if n == nil || n.Type() != "spread_element" || n.NamedChildCount() != 1 {
		return false
	}
	_, ok := metavarName(n.NamedChild(0))
	return ok
}

func matchObjectSubset(pattern, code syntax.Node, binds map[string]string, cs constraints) bool {
	var required []syntax.Node
	for i := 0; i < pattern.NamedChildCount(); i++ {
		if c := pattern.NamedChild(i); !isObjectEllipsis(c) {
			required = append(required, c)
		}
	}
	members := make([]syntax.Node, 0, code.NamedChildCount())
	for i := 0; i < code.NamedChildCount(); i++ {
		members = append(members, code.NamedChild(i))
	}

	taken := make([]bool, len(members))
	var assign func(k int) bool
	assign = func(k int) bool {
		if k == len(required) {
			return true
		}
		for j := range members {
			if taken[j] {
				continue
			}
			saved := snapshotBinds(binds)
			if !matchNode(required[k], members[j], binds, cs) {
				restoreBinds(binds, saved)
				continue
			}
			taken[j] = true
			if assign(k + 1) {
				return true
			}
			taken[j] = false
			restoreBinds(binds, saved)
		}
		return false
	}
	return assign(0)
}

func snapshotBinds(binds map[string]string) map[string]string {
	out := make(map[string]string, len(binds))
	for k, v := range binds {
		out[k] = v
	}
	return out
}

func restoreBinds(binds, saved map[string]string) {
	for k := range binds {
		delete(binds, k)
	}
	for k, v := range saved {
		binds[k] = v
	}
}
