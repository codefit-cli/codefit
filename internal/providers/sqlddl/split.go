package sqlddl

import "strings"

// stmt is one top-level SQL statement with the 1-based line it starts on.
type stmt struct {
	text string
	line int
}

// split tokenizes SQL into top-level statements. A statement ends at a ';' that
// is NOT inside a string, a quoted identifier, a line/block comment, or a
// dollar-quoted block ($$...$$ / $tag$...$tag$). The dollar-quote handling is the
// crux: PL/pgSQL DO/function bodies contain internal semicolons that must not cut.
func split(src []byte) []stmt {
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
		case c == '-' && i+1 < n && s[i+1] == '-':
			// line comment: skip to newline (not added to the statement)
			for i < n && s[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && s[i+1] == '*':
			// block comment
			i += 2
			for i < n && (i+1 >= n || s[i] != '*' || s[i+1] != '/') {
				if s[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
		case c == '\'':
			mark()
			buf.WriteByte(c)
			i++
			for i < n {
				if s[i] == '\'' {
					if i+1 < n && s[i+1] == '\'' { // '' escape
						buf.WriteString("''")
						i += 2
						continue
					}
					buf.WriteByte('\'')
					i++
					break
				}
				if s[i] == '\n' {
					line++
				}
				buf.WriteByte(s[i])
				i++
			}
		case c == '"':
			mark()
			buf.WriteByte(c)
			i++
			for i < n {
				if s[i] == '"' {
					if i+1 < n && s[i+1] == '"' {
						buf.WriteString(`""`)
						i += 2
						continue
					}
					buf.WriteByte('"')
					i++
					break
				}
				if s[i] == '\n' {
					line++
				}
				buf.WriteByte(s[i])
				i++
			}
		case c == '$':
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
