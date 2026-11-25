package ruleengine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/codefit-cli/codefit/internal/core/syntax"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
)

// [valueveto] metavariable-value-veto — drop a match when the VALUE bound to a
// metavariable proves, by its own shape, that it is something else.
//
//	metavariable-value-veto:
//	  $VALUE: not-a-credential
//
// WHAT IT SOLVES (issue #148, ADR 0086, ADR 0087). SEC-001 affirmed any quoted
// literal under a credential-named binding at confidence 1.0, so `const
// secretFile = "lib/config.ts"` was reported as a hardcoded secret. The Go
// provider already asks internal/core/valueshape whether the literal proves it
// is a URL, a URN, an environment-variable name or a file path. A rule could not
// ask: a regex cannot parse a URL or consult the credential vocabulary, and a
// second implementation would be a second place to be wrong. This operator makes
// the rule call the SAME function.
//
// WHAT IT RECEIVES. The source text of the whole code node bound to the
// metavariable — for a string literal, its quotes included. That is what
// [Matches] binds for any node; compile guarantees the node can be a whole
// string literal and not a piece of one (see the last bullet below).
//
// HOW IT DECIDES. The text is decoded as ONE JavaScript string literal by
// [decodeJSStringLiteral]. Only when decoding succeeds AND the predicate returns
// true is the binding rejected. Every other case keeps it: a value that does not
// decode (a concatenation, a template, a legacy octal escape, a lone surrogate,
// a malformed escape) proves nothing. Failure always falls toward "keeps
// firing", because a veto that silences a real credential is worse than a line
// of noise.
//
// DECODING IS MANDATORY, NOT COSMETIC. `"…?api_key\x3dabc"` denotes a URL that
// carries api_key=abc; trimming its quotes would hand the predicate
// `api_key\x3dabc`, a parameter with no value, and veto a credential.
//
// WHEN IT RUNS. At bind time, through [constraint.ok], so it steers the
// object-subset search ([objectsubset]): in `{secretFile: "lib/config.ts",
// apiKey: "s3cr3t"}` the first pair is rejected while it is being bound and the
// search goes on to apiKey. A post-match filter would bind the first pair, drop
// the whole object, and report nothing — a false all-clear.
//
// WHAT FAILS AT COMPILE, located and naming the rule:
//   - a predicate this package does not register (the set is closed, like
//     [metavarVocabularies]);
//   - a key that is not a metavariable (`VALUE`, `$value`);
//   - a metavariable that some base alternative never binds. [evalRule] treats a
//     constrained metavariable that was never bound as a rejection, so such a
//     key would make that alternative — or the whole rule — silent forever. The
//     object ellipsis marker (`$REST`) never binds and is rejected the same way;
//   - a metavariable that stands, in some base alternative, where a whole string
//     literal cannot: inside a string, a template or a regex (it would bind the
//     contents, without quotes, which never decode), or in a shorthand property,
//     a member name or a declaration name (which never hold a string). Proved by
//     example in [literalCannotStandThere], with the provider's own parser.
//
// What compile does NOT check: another constraint on the same metavariable that
// admits no value the predicate would veto (a regex demanding one exact literal,
// say). The veto is then inert because the rule says so, not because of where
// the metavariable stands.
//
// WHAT IT DOES NOT SEE is whatever never reaches the metavariable: the rule's
// own patterns decide that, and a value outside them is neither vetoed nor
// reported. See the rule's own declared limits.
type valueVeto func(boundText string) bool

// valueVetoes is the closed set of value predicates a rule may name. Closed on
// purpose, for the reason [metavarVocabularies] is: an open registry would let a
// rule name anything and discover at runtime that it vetoes nothing.
var valueVetoes = map[string]valueVeto{
	"not-a-credential": vetoNotACredential,
}

// vetoNotACredential decodes the bound text and asks valueshape.NotACredential
// — the same function the Go provider calls — whether the value proves it is
// not a credential.
func vetoNotACredential(boundText string) bool {
	value, ok := decodeJSStringLiteral(boundText)
	if !ok {
		return false
	}
	_, vetoed := valueshape.NotACredential(value)
	return vetoed
}

func knownValueVetoes() []string {
	out := make([]string, 0, len(valueVetoes))
	for k := range valueVetoes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// compileValueVetoes validates r.MetavariableValueVeto against the compiled base
// alternatives and installs each veto on its metavariable's constraint. Keys are
// visited in sorted order so the first error reported is deterministic.
func compileValueVetoes(r Rule, bases []syntax.Node, cs constraints, parse func(string) (syntax.Node, error)) error {
	keys := make([]string, 0, len(r.MetavariableValueVeto))
	for k := range r.MetavariableValueVeto {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, mv := range keys {
		name := r.MetavariableValueVeto[mv]
		if !metavarRe.MatchString(mv) {
			return fmt.Errorf("rule %s: metavariable-value-veto key %q is not a metavariable "+
				"(want $ followed by an upper-case name, like $VALUE)", r.ID, mv)
		}
		veto, known := valueVetoes[name]
		if !known {
			return fmt.Errorf("rule %s: metavariable-value-veto %s: unknown predicate %q (known: %s) — "+
				"a rule that named an unknown predicate would compile cleanly and veto nothing",
				r.ID, mv, name, strings.Join(knownValueVetoes(), ", "))
		}
		for i, base := range bases {
			bound := map[string]bool{}
			boundMetavariables(base, bound)
			if !bound[mv] {
				return fmt.Errorf("rule %s: metavariable-value-veto %s: base pattern %q never binds it — "+
					"a constrained metavariable that is never bound rejects every match, so that "+
					"alternative would be silent forever", r.ID, mv, baseSource(r, i))
			}
			if reason := literalCannotStandThere(baseSource(r, i), base, mv, parse); reason != "" {
				return fmt.Errorf("rule %s: metavariable-value-veto %s: in base pattern `%s` it does not stand "+
					"where a string literal can stand (%s) — the veto would compile and never fire. A "+
					"metavariable inside a string, a template or a regex binds the contents without their "+
					"quotes, which never decode; a declaration name, a member name or a shorthand property "+
					"never holds a string at all", r.ID, mv, baseSource(r, i), reason)
			}
		}
		c := cs[mv]
		c.veto = veto
		cs[mv] = c
	}
	return nil
}

// vetoProbe is the string literal literalCannotStandThere writes in place of a
// vetoed metavariable. Any literal would do; this one cannot occur by accident.
const vetoProbe = `"codefitValueVetoProbe"`

// literalCannotStandThere proves that the veto on mv can fire in pattern, or
// says why not. It rewrites every occurrence of mv in the pattern's source as a
// string literal and requires three things of the result: it still parses
// without error; the compiled pattern still matches it; and mv is then bound to
// that literal NODE — its whole source, quotes included — and not to a piece of
// another node that happens to carry the same characters, at ANY occurrence.
//
// It is a proof by example, not a list of node types, so it is the parser that
// decides where a string literal may stand. Measured against the TypeScript
// parser: inside a string, a template or a regex the rewrite either stops
// parsing or binds a fragment; in a shorthand property, a member name or a
// declaration name it stops parsing; as an argument, a spread, an object key, a
// template substitution, an object property value or a literal type it binds
// the literal.
//
// An empty result means the veto can fire there.
func literalCannotStandThere(src string, pattern syntax.Node, mv string, parse func(string) (syntax.Node, error)) string {
	occurrence := regexp.MustCompile(`(^|[^A-Za-z0-9_$])` + regexp.QuoteMeta(mv) + `([^A-Za-z0-9_$]|$)`)
	rewritten := src
	for {
		// Loop because adjacent occurrences share the separator between them.
		next := occurrence.ReplaceAllString(rewritten, "${1}"+vetoProbe+"${2}")
		if next == rewritten {
			break
		}
		rewritten = next
	}
	// compilePattern is the compile gate every pattern passes: it refuses a
	// tree that parsed with an error, because the parser recovers instead of
	// failing. No pattern measured so far reaches a wrong verdict without that
	// refusal — the checks below reject each broken rewrite too — so here it is
	// a second gate, not the only one.
	code, err := compilePattern(rewritten, parse)
	if err != nil {
		return fmt.Sprintf("written as a string literal there, the pattern `%s` does not parse", rewritten)
	}
	probe, err := compilePattern(vetoProbe, parse)
	if err != nil {
		return "the parser does not read a plain string literal, so nothing can be proved"
	}
	literalType := probe.Type()

	binds := map[string]string{}
	if !matchNode(pattern, code, binds, nil) {
		return fmt.Sprintf("the pattern does not match `%s`", rewritten)
	}
	// Also a second gate: with every occurrence rewritten, no measured pattern
	// binds mv to anything but the probe once it matched. It stays because the
	// property it states — mv receives the literal — is the one the veto needs.
	if binds[mv] != vetoProbe {
		return fmt.Sprintf("in `%s` it binds %q instead of the literal", rewritten, binds[mv])
	}
	// Every node that reads as the probe must be the literal or wrap it (a
	// literal TYPE wraps a string node with the same text). A node that reads as
	// the probe without containing the literal is a fragment of something else —
	// a string's contents, a template piece, a regex body — and a metavariable
	// bound there receives text without its own quotes. One such occurrence is
	// enough to reject, because the first occurrence is the one that binds.
	literals, fragments := 0, 0
	walk(code, func(n syntax.Node) {
		if string(n.Text()) != vetoProbe {
			return
		}
		switch {
		case n.Type() == literalType:
			literals++
		case !containsLiteral(n, literalType):
			fragments++
		}
	})
	if fragments > 0 || literals == 0 {
		return fmt.Sprintf("in `%s` the literal's characters are part of another node, not a string literal", rewritten)
	}
	return ""
}

// containsLiteral reports whether a descendant of n is a literalType node whose
// text is the probe.
func containsLiteral(n syntax.Node, literalType string) bool {
	for i := 0; i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		if (c.Type() == literalType && string(c.Text()) == vetoProbe) || containsLiteral(c, literalType) {
			return true
		}
	}
	return false
}

// baseSource returns the source text of the i-th base alternative, for errors.
func baseSource(r Rule, i int) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.PatternEither[i]
}

// boundMetavariables records every metavariable a match of pattern n binds. It
// walks the pattern the way [matchNode] does: a metavariable leaf binds, and
// the object ellipsis marker inside an object pattern does not ([objectsubset]).
func boundMetavariables(n syntax.Node, out map[string]bool) {
	if n == nil {
		return
	}
	if name, ok := metavarName(n); ok {
		out[name] = true
		return
	}
	marker := n.Type() == "object" && hasObjectEllipsis(n)
	for i := 0; i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		if marker && isObjectEllipsis(child) {
			continue
		}
		boundMetavariables(child, out)
	}
}

// decodeJSStringLiteral returns the string a JavaScript/TypeScript string
// literal denotes, and false when text is not exactly ONE such literal or uses
// an escape this decoder refuses.
//
// Accepted: text starts with ' or ", and the first unescaped matching quote is
// its last byte. Escapes: \n \r \t \b \f \v; \0 not followed by a digit; \xHH;
// \uHHHH, with a high surrogate only when a \uHHHH low surrogate follows it;
// \u{H…} up to U+10FFFF and outside the surrogate range; a line continuation
// (backslash before LF, CR, CRLF, U+2028 or U+2029), which denotes nothing; and
// an identity escape for any other character (\/ \' \" \\ \q).
//
// Refused, so the veto proves nothing: legacy octal escapes (\00, \1…\7) and the
// non-octal decimal escapes \8 \9, which strict mode forbids; a lone surrogate,
// which has no UTF-8 form; a malformed \x or \u; a raw LF or CR inside the
// literal; and anything that is not one literal — a template, a concatenation
// such as `"a" + "b"`, an unterminated or mismatched quote.
func decodeJSStringLiteral(text string) (string, bool) {
	if len(text) < 2 {
		return "", false
	}
	quote := text[0]
	if quote != '"' && quote != '\'' {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(text))
	i := 1
	for i < len(text) {
		c := text[i]
		switch {
		case c == quote:
			// The first unescaped matching quote must close the literal.
			if i != len(text)-1 {
				return "", false
			}
			return b.String(), true
		case c == '\n' || c == '\r':
			return "", false
		case c != '\\':
			b.WriteByte(c)
			i++
			continue
		}
		next, ok := decodeEscape(text, i+1, &b)
		if !ok {
			return "", false
		}
		i = next
	}
	return "", false // no closing quote
}

// decodeEscape decodes the escape whose first character is text[i] (the byte
// after the backslash), writes what it denotes to b, and returns the index just
// past it.
func decodeEscape(text string, i int, b *strings.Builder) (int, bool) {
	if i >= len(text) {
		return 0, false
	}
	switch c := text[i]; c {
	case 'n':
		b.WriteByte('\n')
	case 'r':
		b.WriteByte('\r')
	case 't':
		b.WriteByte('\t')
	case 'b':
		b.WriteByte('\b')
	case 'f':
		b.WriteByte('\f')
	case 'v':
		b.WriteByte('\v')
	case '0':
		if i+1 < len(text) && isDecimalDigit(text[i+1]) {
			return 0, false // legacy octal (\00, \01, …)
		}
		b.WriteByte(0)
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return 0, false // legacy octal, or the non-octal decimal escapes \8 \9
	case 'x':
		v, ok := parseHex(text, i+1, 2)
		if !ok {
			return 0, false
		}
		b.WriteRune(rune(v))
		return i + 3, true
	case 'u':
		return decodeUnicodeEscape(text, i+1, b)
	case '\r':
		if i+1 < len(text) && text[i+1] == '\n' {
			return i + 2, true // CRLF line continuation
		}
		return i + 1, true
	case '\n':
		return i + 1, true
	default:
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && size <= 1 {
			return 0, false
		}
		if r == lineSeparator || r == paragraphSeparator {
			return i + size, true // line continuation
		}
		b.WriteRune(r) // identity escape
		return i + size, true
	}
	return i + 1, true
}

// decodeUnicodeEscape decodes what follows `\u`: either `{H…}` or four hex
// digits, joining a high surrogate with the `\uHHHH` low surrogate that must
// follow it.
func decodeUnicodeEscape(text string, i int, b *strings.Builder) (int, bool) {
	if i < len(text) && text[i] == '{' {
		end := strings.IndexByte(text[i:], '}')
		if end < 2 { // no '}', or `\u{}`
			return 0, false
		}
		v, ok := parseHex(text, i+1, end-1)
		if !ok || v > utf8.MaxRune || isSurrogate(v) {
			return 0, false
		}
		b.WriteRune(rune(v))
		return i + end + 1, true
	}
	v, ok := parseHex(text, i, 4)
	if !ok {
		return 0, false
	}
	next := i + 4
	switch {
	case isLowSurrogate(v):
		return 0, false // a low surrogate with no high one before it
	case isHighSurrogate(v):
		if next+6 > len(text) || text[next] != '\\' || text[next+1] != 'u' {
			return 0, false // a lone high surrogate
		}
		low, ok := parseHex(text, next+2, 4)
		if !ok || !isLowSurrogate(low) {
			return 0, false
		}
		b.WriteRune(utf16.DecodeRune(rune(v), rune(low)))
		return next + 6, true
	}
	b.WriteRune(rune(v))
	return next, true
}

// parseHex reads exactly n hex digits starting at text[i], and returns false
// only when fewer than n bytes remain or one of them is not a hex digit. n may
// be large for `\u{…}`, so the value stops growing once it exceeds
// utf8.MaxRune: the result is then SOME value above utf8.MaxRune, not the one
// the digits spell, and the remaining digits are still validated. parseHex does
// not reject that value; the caller owns the range check ([decodeUnicodeEscape]
// compares the result with utf8.MaxRune).
func parseHex(text string, i, n int) (uint32, bool) {
	if n <= 0 || i+n > len(text) {
		return 0, false
	}
	var v uint32
	for _, c := range []byte(text[i : i+n]) {
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		if v > utf8.MaxRune {
			continue // already out of range; keep validating the digits
		}
		v = v<<4 | uint32(d)
	}
	return v, true
}

// The UTF-16 surrogate ranges, written once. A high surrogate must be followed
// by a low one for a \uHHHH pair to denote a code point; either half alone has
// no UTF-8 form.
func isHighSurrogate(v uint32) bool { return v >= 0xD800 && v <= 0xDBFF }

func isLowSurrogate(v uint32) bool { return v >= 0xDC00 && v <= 0xDFFF }

func isSurrogate(v uint32) bool { return isHighSurrogate(v) || isLowSurrogate(v) }

// The two line terminators beyond LF and CR. After a backslash, each is a line
// continuation and denotes nothing. Named, because written raw they render as
// blanks.
const (
	lineSeparator      = '\u2028' // U+2028 LINE SEPARATOR
	paragraphSeparator = '\u2029' // U+2029 PARAGRAPH SEPARATOR
)

func isDecimalDigit(c byte) bool { return c >= '0' && c <= '9' }
