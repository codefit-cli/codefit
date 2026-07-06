package sqlddl

import "strings"

// stmt is one top-level SQL statement with the 1-based line it starts on.
type stmt struct {
	text string
	line int
}

// split tokenizes SQL into top-level statements, according to the given
// dialect's lexical rules (comments, quoting, dollar-quoting). A statement
// ends at a ';' that is NOT inside a string, a quoted identifier, a
// line/block comment, or (when dialect.DollarQuoting) a dollar-quoted block
// ($$...$$ / $tag$...$tag$) — the crux that keeps a PL/pgSQL DO/function
// body's internal semicolons from cutting the statement.
//
// split is the SOLE owner of quote/comment knowledge (design §2): every
// dialect-quoted identifier is RE-EMITTED as canonical ANSI "..." as it is
// tokenized, so reduce.go's regexes and normalizeName never need to know the
// source dialect's quoting style.
func split(src []byte, dialect *Dialect) []stmt {
	s := string(src)
	n := len(s)
	var out []stmt
	var buf strings.Builder
	line := 1
	startLine := 0 // 0 = statement not started yet
	i := 0

	flush := func() {
		if t := strings.TrimSpace(buf.String()); t != "" {
			out = append(out, stmt{text: t, line: startLine})
		}
		buf.Reset()
		startLine = 0
	}
	mark := func() {
		if startLine == 0 {
			startLine = line
		}
	}

	for i < n {
		c := s[i]
		switch {
		case matchLineComment(s, i, dialect.LineComments) > 0:
			// line comment: skip to newline (not added to the statement)
			for i < n && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && s[i+1] == '*':
			// block comment (universal, not a dialect field)
			i += 2
			for i < n && (i+1 >= n || s[i] != '*' || s[i+1] != '/') {
				if s[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
		case c == '\'':
			// string literal (universal, '' escape) — never canonicalized.
			mark()
			i, line = scanStringLiteral(&buf, s, i, line, '\'')
		case dialect.DoubleQuoteIsString && c == '"':
			// MySQL default (ANSI_QUOTES off): " opens a STRING, not an identifier.
			mark()
			i, line = scanStringLiteral(&buf, s, i, line, '"')
		case identQuoteFor(c, dialect.IdentQuotes) != nil:
			mark()
			var ident string
			ident, i, line = scanIdentQuoted(s, i, line, *identQuoteFor(c, dialect.IdentQuotes))
			buf.WriteString(ident)
		case dialect.DollarQuoting && c == '$':
			if tag, ok := dollarTag(s, i); ok {
				mark()
				buf.WriteString(tag)
				i += len(tag)
				for i < n {
					if strings.HasPrefix(s[i:], tag) {
						buf.WriteString(tag)
						i += len(tag)
						break
					}
					if s[i] == '\n' {
						line++
					}
					buf.WriteByte(s[i])
					i++
				}
			} else {
				mark()
				buf.WriteByte(c)
				i++
			}
		case c == ';':
			flush()
			i++
		case c == '\n':
			line++
			if startLine != 0 {
				buf.WriteByte(c)
			}
			i++
		case c == ' ' || c == '\t' || c == '\r':
			if startLine != 0 {
				buf.WriteByte(c)
			}
			i++
		default:
			mark()
			buf.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

// matchLineComment returns the length of the dialect line-comment prefix that
// s[i:] starts with, or 0 if none match.
func matchLineComment(s string, i int, prefixes []string) int {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s[i:], p) {
			return len(p)
		}
	}
	return 0
}

// identQuoteFor returns the QuotePair whose Open == c, or nil if none match.
func identQuoteFor(c byte, pairs []QuotePair) *QuotePair {
	for i := range pairs {
		if pairs[i].Open == c {
			return &pairs[i]
		}
	}
	return nil
}

// scanStringLiteral scans a string literal delimited by quote (with doubling
// escape), writing it VERBATIM — including delimiters and escapes — into buf.
// Strings are never canonicalized, only identifiers are. Returns the advanced
// index and line.
func scanStringLiteral(buf *strings.Builder, s string, i, line int, quote byte) (int, int) {
	n := len(s)
	buf.WriteByte(quote)
	i++
	for i < n {
		if s[i] == quote {
			if i+1 < n && s[i+1] == quote {
				buf.WriteByte(quote)
				buf.WriteByte(quote)
				i += 2
				continue
			}
			buf.WriteByte(quote)
			i++
			break
		}
		if s[i] == '\n' {
			line++
		}
		buf.WriteByte(s[i])
		i++
	}
	return i, line
}

// scanIdentQuoted scans a dialect-quoted identifier starting at s[i] (where
// s[i] == qp.Open) and RE-EMITS it canonicalized to ANSI "..." regardless of
// the source delimiter — the seam that keeps reduce.go's regexes and
// normalizeName dialect-free (design §2). For the PostgreSQL descriptor
// (qp.Open == qp.Close == '"') this is the identity transform: byte-identical
// output, guaranteeing the PG no-regression gate. Returns the canonical
// identifier text (including its own "..." delimiters), the advanced index
// and line.
func scanIdentQuoted(s string, i, line int, qp QuotePair) (string, int, int) {
	n := len(s)
	var out strings.Builder
	out.WriteByte('"')
	i++
	closed := false
	for i < n {
		if s[i] == qp.Close {
			if qp.Doubling && i+1 < n && s[i+1] == qp.Close {
				writeIdentByte(&out, qp.Close)
				i += 2
				continue
			}
			i++ // closing delimiter consumed, identifier done
			closed = true
			break
		}
		if s[i] == '\n' {
			line++
		}
		writeIdentByte(&out, s[i])
		i++
	}
	// Only emit the canonical closing quote when a real closing delimiter was
	// matched in the source. On EOF without a close, mirror the pre-refactor
	// tokenizer: never invent a delimiter that was not there.
	if closed {
		out.WriteByte('"')
	}
	return out.String(), i, line
}

// writeIdentByte writes one content byte into a canonical ANSI-quoted
// identifier, doubling it if it is the canonical delimiter '"' itself so the
// re-emitted identifier stays validly escaped.
func writeIdentByte(out *strings.Builder, b byte) {
	out.WriteByte(b)
	if b == '"' {
		out.WriteByte('"')
	}
}

// dollarTag returns the dollar-quote tag beginning at s[i] (which must be '$'),
// e.g. "$$" or "$func$". The tag body is [A-Za-z0-9_]* and must be closed by a
// second '$'. Returns ok=false when it is a lone '$' (not a dollar quote).
func dollarTag(s string, i int) (string, bool) {
	if i >= len(s) || s[i] != '$' {
		return "", false
	}
	j := i + 1
	for j < len(s) && (isAlnum(s[j]) || s[j] == '_') {
		j++
	}
	if j < len(s) && s[j] == '$' {
		return s[i : j+1], true
	}
	return "", false
}

func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
