package ruleengine

import (
	"fmt"
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
// WHAT IT RECEIVES. The source text bound to the metavariable, quotes included —
// what [Matches] binds for any node.
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
//     object ellipsis marker (`$REST`) never binds and is rejected the same way.
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
func compileValueVetoes(r Rule, bases []syntax.Node, cs constraints) error {
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
		}
		c := cs[mv]
		c.veto = veto
		cs[mv] = c
	}
	return nil
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
		if r == ' ' || r == ' ' {
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
	case v >= 0xDC00 && v <= 0xDFFF:
		return 0, false // a low surrogate with no high one before it
	case v >= 0xD800 && v <= 0xDBFF:
		if next+6 > len(text) || text[next] != '\\' || text[next+1] != 'u' {
			return 0, false // a lone high surrogate
		}
		low, ok := parseHex(text, next+2, 4)
		if !ok || low < 0xDC00 || low > 0xDFFF {
			return 0, false
		}
		b.WriteRune(utf16.DecodeRune(rune(v), rune(low)))
		return next + 6, true
	}
	b.WriteRune(rune(v))
	return next, true
}

// parseHex reads exactly n hex digits starting at text[i]. n may be large for
// `\u{…}`, so the value is capped as it grows and reported as too large.
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

func isSurrogate(v uint32) bool { return v >= 0xD800 && v <= 0xDFFF }

func isDecimalDigit(c byte) bool { return c >= '0' && c <= '9' }
