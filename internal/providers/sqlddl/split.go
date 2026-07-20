package sqlddl

import (
	"regexp"
	"strings"
)

// stmt is one top-level SQL statement with the 1-based line it starts on.
//
// term and quotedBlockSeen are the two tokenizer FACTS reduce.go's Body
// derivation (Phase 2.2, Unit A) reads to decide db.Body.Complete — split()
// already knows both for free while it scans; nothing about them is
// discovered by re-reading the statement text afterward.
type stmt struct {
	text string
	line int

	// term is which mechanism ended this statement: the terminator kind, per
	// the binding decision (architecture/tsql-body-truncation-limit).
	term termKind

	// quotedBlockSeen is true when a dollar-quoted ($$...$$ / $tag$...$tag$)
	// block was consumed while scanning this statement — PostgreSQL's
	// DollarQuoting swallows every internal ';' inside it, so a statement
	// that contains one is trustworthy as COMPLETE even when its own
	// terminator is an ordinary ';' (the outer one, after the $$ block
	// closed). A plain single-quoted string literal does NOT set this: it
	// only protects ITS OWN content from being misread as a terminator, it
	// does not prove the statement's tail is intact (see routineBody in
	// reduce.go for why counting bare string literals here would be unsafe).
	quotedBlockSeen bool
}

// termKind is the mechanism that flushed a stmt — the tokenizer-state fact
// db.Body.Complete is derived from (reduce.go), never from re-scanning the
// body text for BEGIN/END (that unsound guard was REMOVED from this package,
// see apply()'s doc comment and ADR 0022).
type termKind string

const (
	// termSemicolon: the statement ended at an ordinary ';' — the ONLY kind
	// that can mean "this may just be the first of several internal
	// statements", because it is the same character a routine/trigger body
	// uses for ITS OWN internal statement separators too.
	termSemicolon termKind = "semicolon"
	// termCustomDelimiter: the statement ended at the dialect's ACTIVE
	// terminator after a MySQL "DELIMITER //" directive changed it away from
	// ';' — by construction this means every internal ';' inside the body was
	// NOT a terminator, so the whole body was captured.
	termCustomDelimiter termKind = "customDelimiter"
	// termGoBreak: the statement ended at a T-SQL/sqlcmd "GO" batch
	// separator, which is not part of the SQL grammar in any dialect — the
	// text up to it is whatever the tokenizer accumulated, never CUT by GO
	// itself.
	termGoBreak termKind = "goBreak"
	// termEOF: the statement ended because the input ran out (no trailing
	// terminator at all) — like termGoBreak, EOF itself never cuts content.
	termEOF termKind = "eof"
)

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
	term := ";"         // active statement terminator; changed by a DELIMITER directive
	quotedSeen := false // a dollar-quoted block was consumed since the last flush (Phase 2.2, Unit A)

	// headDecided/routineHead (ADR 0027): per-statement state deciding whether
	// an internal ';' inside a dialect with RoutineBodyEndsAtBatchSeparator is
	// a body separator (suppress the flush) or the statement's real terminator
	// (flush as usual). headDecided is set at most once per statement, the
	// first time the generic ';' terminator case is reached — at that point
	// buf holds exactly the CREATE FUNCTION/PROCEDURE/TRIGGER head (or not),
	// and isRoutineHead's verdict is cached in routineHead for every
	// subsequent internal ';' of the same statement.
	headDecided := false
	routineHead := false

	flush := func(kind termKind) {
		if t := strings.TrimSpace(buf.String()); t != "" {
			out = append(out, stmt{text: t, line: startLine, term: kind, quotedBlockSeen: quotedSeen})
		}
		buf.Reset()
		startLine = 0
		quotedSeen = false
		headDecided = false
		routineHead = false
	}
	mark := func() {
		if startLine == 0 {
			startLine = line
		}
	}

	for i < n {
		// Unit I phantom-table guard (design §8): two client-tool batch
		// markers — MySQL's "DELIMITER <tok>" directive and T-SQL/sqlcmd's
		// standalone "GO" batch separator — are recognized here as UNIVERSAL,
		// dialect-free literals (never gated on dialect.Name): neither is
		// part of the SQL:1992 grammar in ANY of the three dialects, so
		// matching them unconditionally is safe and keeps every existing
		// fixture byte-identical (neither marker appears in real DDL).
		//
		// A DELIMITER directive changes the ACTIVE terminator (term) that the
		// generic terminator case below matches against, instead of the
		// hardcoded ';' — this is what lets a MySQL "DELIMITER //" ... "//"
		// ... "DELIMITER ;" trigger/procedure body stay ONE statement (its
		// internal ';'s are no longer terminators), so the body's head
		// (CREATE TRIGGER/PROCEDURE) is captured by reduce.go's anchored
		// regex while no body-internal fragment can ever leak out as its own
		// top-level statement (the phantom-table risk the design flagged).
		if adv, newTerm, ok := matchDelimiterDirective(s, i); ok {
			// A DELIMITER directive line normally follows a statement that
			// already flushed on its OWN terminator (the common case: buf is
			// empty here, so this flush produces no stmt at all). Labeled
			// termGoBreak because it is the same kind of soft, non-';' break —
			// never a real semicolon — for the rare malformed input where buf
			// is non-empty at this point.
			flush(termGoBreak)
			term = newTerm
			if strings.Contains(s[i:i+adv], "\n") {
				line++
			}
			i += adv
			continue
		}
		// A standalone "GO" line is a soft statement break (like ';') so that
		// a real CREATE TABLE on either side of a GO-batched routine body is
		// still tokenized as its own statement, never glued to the batch's
		// tail. A T-SQL routine BODY is not modeled: its internal ';'-cut
		// fragments are not guarded, so a CREATE-TABLE-shaped fragment inside a
		// GO-batched body may surface as a spurious table — a documented known
		// limit (ADR 0022), not silently corrected. MySQL bodies wrapped in
		// DELIMITER //…// are unaffected (the whole body is one statement here).
		if adv := matchGoBatchSeparator(s, i); adv > 0 {
			flush(termGoBreak)
			if strings.Contains(s[i:i+adv], "\n") {
				line++
			}
			i += adv
			continue
		}

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
				quotedSeen = true
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
		case strings.HasPrefix(s[i:], term):
			// ADR 0027: when this dialect ends a routine body at the GO
			// batch separator (T-SQL) rather than at an internal ';', an
			// ordinary ';' terminator inside a recognized CREATE
			// FUNCTION/PROCEDURE/TRIGGER head must NOT flush — it is a
			// body-internal statement separator, not the end of THIS
			// top-level statement. The verdict is decided once per
			// statement (headDecided) from whatever buf holds at the
			// FIRST internal ';' — that is exactly the routine head, since
			// nothing has flushed yet — and cached for every subsequent
			// ';' in the same body. The whole body then flushes later, at
			// the existing GO branch (termGoBreak) or at EOF (termEOF).
			if term == ";" && dialect.RoutineBodyEndsAtBatchSeparator {
				if !headDecided {
					routineHead = isRoutineHead(buf.String())
					headDecided = true
				}
				if routineHead {
					buf.WriteString(term)
					i += len(term)
					continue
				}
			}
			kind := termCustomDelimiter
			if term == ";" {
				kind = termSemicolon
			}
			flush(kind)
			i += len(term)
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
	flush(termEOF)
	return out
}

// matchLineComment returns the length of the dialect line-comment prefix that
// s[i:] starts with, or 0 if none match. A prefix with RequireBoundaryAfter
// only matches when the character immediately following it is a boundary
// (whitespace or a control char) or there is no following character
// (end-of-line/EOF) — the rule MySQL's "--" needs (unlike PostgreSQL's
// unconditional "--") to avoid misreading "1--1" as a comment opener. This
// single dialect-free path is shared by every dialect: no per-dialect branch
// lives in the tokenizer.
func matchLineComment(s string, i int, comments []LineComment) int {
	for _, lc := range comments {
		if lc.Prefix == "" || !strings.HasPrefix(s[i:], lc.Prefix) {
			continue
		}
		if !lc.RequireBoundaryAfter || isCommentBoundary(s, i+len(lc.Prefix)) {
			return len(lc.Prefix)
		}
	}
	return 0
}

// isCommentBoundary reports whether position j in s is a valid boundary for
// a line-comment prefix that requires one: whitespace/control char, or
// end-of-string (no character at all).
func isCommentBoundary(s string, j int) bool {
	if j >= len(s) {
		return true
	}
	c := s[j]
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c < 0x20 || c == 0x7f
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

// reDelimiterDirective matches a MySQL client "DELIMITER <tok>" directive
// line (e.g. "DELIMITER //", "DELIMITER ;"), case-insensitive. It is a
// client-tool convention, never part of the SQL:1992 grammar, so matching it
// unconditionally (not gated on dialect.Name) is safe in principle — but the
// argument itself MUST be constrained to punctuation-only tokens (Unit I
// rework, C1): a bare "DELIMITER[ \t]+(\S+)" also fires on an ordinary column
// definition that happens to start a line with the word "delimiter" (e.g.
// "delimiter VARCHAR(1),"), silently corrupting the tokenizer on VALID input.
// A real client-tool delimiter token (//, $$, |, ;, ...) is never a bare
// word/identifier, so requiring the argument to be entirely non-word,
// non-whitespace characters (`[^\w\s]+`) keeps the directive recognizable
// while making a word-leading argument (a type/identifier) categorically
// impossible to match — dialect-free, no fixture in this package uses a
// punctuation-only delimiter token as a real column/type name.
var reDelimiterDirective = regexp.MustCompile(`(?i)^[ \t]*DELIMITER[ \t]+([^\w\s]+)[ \t]*(\r?\n|$)`)

// reGoBatchSeparator matches a T-SQL/sqlcmd "GO" batch-separator line: the
// ENTIRE trimmed line must be "GO" (case-insensitive) — this word-boundary
// requirement (immediately followed by only whitespace then end-of-line/EOF)
// is what keeps it from ever matching part of a longer identifier such as
// "GOTO" or a column literally named "go".
var reGoBatchSeparator = regexp.MustCompile(`(?i)^[ \t]*GO[ \t]*(\r?\n|$)`)

// isAtLineStart reports whether s[i] begins a new line (i==0 or the previous
// byte is '\n'). Both DELIMITER-directive and GO-batch-separator recognition
// are gated on this: they only ever fire between statement content and the
// start of a line, never with a string, comment, or quoted identifier
// straddling the boundary — every such token is fully consumed in a single
// loop iteration elsewhere in split(), so i is never left mid-token when this
// check runs.
func isAtLineStart(s string, i int) bool {
	return i == 0 || s[i-1] == '\n'
}

// matchDelimiterDirective returns the byte length to advance and the new
// terminator token when s[i:] is a DELIMITER directive line at the start of
// a line, or ok=false otherwise.
func matchDelimiterDirective(s string, i int) (advance int, newTerm string, ok bool) {
	if !isAtLineStart(s, i) {
		return 0, "", false
	}
	m := reDelimiterDirective.FindStringSubmatch(s[i:])
	if m == nil {
		return 0, "", false
	}
	return len(m[0]), m[1], true
}

// matchGoBatchSeparator returns the byte length to advance when s[i:] is a
// standalone "GO" batch-separator line at the start of a line, or 0 otherwise.
func matchGoBatchSeparator(s string, i int) int {
	if !isAtLineStart(s, i) {
		return 0
	}
	return len(reGoBatchSeparator.FindString(s[i:]))
}
