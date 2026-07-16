package dbrules

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db020 — a VIEW whose top-level SELECT column list exposes a column/alias
// whose name matches a sensitive token. It reuses matchSensitiveToken /
// hasEncryptionHint (names.go) VERBATIM — the exact same vocabulary db053
// already uses — never a second, divergent vocabulary.
//
// Name-heuristic over recovered DDL text → NEVER an affirmation, always
// SURFACE (ADR 0015/0017, same epistemology as DB-053): a column named
// "password" in a view says nothing about whether the underlying value is
// masked, hashed, or genuinely exposed to whoever queries the view — that
// judgment is the agent's.
//
// Body-completeness contract (ADR 0004, architecture/tsql-body-truncation-
// limit): a View's Body is ALWAYS Complete on every dialect — reduce.go's
// viewBody derives this unconditionally, because a CREATE VIEW statement
// cannot legally contain a top-level ';' in any of the three supported
// dialects, so there is nothing to truncate. This rule still checks
// Complete defensively (never trust a struct's own contract silently) and
// that guard is test-locked (TestDB020_AbstainOnIncompleteBody) so a future
// change to viewBody's derivation cannot silently turn an honest partial
// capture into a false affirmation.
type db020 struct{}

func (db020) ID() string { return "DB-020" }

func (db020) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, v := range s.Views {
		if !v.Body.Complete {
			continue // defensive abstain — see type doc; views are always Complete today
		}
		cols, ok := extractSelectColumns(v.Body.Text)
		if !ok {
			continue // no top-level SELECT/FROM found, or "SELECT *" — declared miss
		}
		for _, c := range cols {
			tok, ok := matchSensitiveToken(c.name)
			if !ok {
				continue
			}
			out = append(out, findings.SurfaceItem{
				Category: string(surface.CategoryDBViewSensitiveColumn),
				File:     v.Pos.File,
				Line:     v.Pos.Line + strings.Count(v.Body.Text[:c.offset], "\n"),
				StructuralSignals: []string{
					"view: " + v.Name,
					"column: " + c.name,
					"matched_token: " + tok,
				},
				StructuralFacts: map[string]bool{
					"encryption_hint_in_name": hasEncryptionHint(c.name),
				},
				ReasonToReview: "This view's SELECT list exposes a column whose name suggests sensitive data (" + c.name +
					"). Is this intentional (e.g. an admin-only view), or should this column be excluded/masked?",
			})
		}
	}
	return nil, out
}

// --- the SELECT-column-list extractor (DB-020's net-new parsing surface) ---
//
// architecture/unit-c-tsql-risk-reeval (obs #1056) flagged that DB-020 needs
// a NET-NEW SELECT-column-list parser: reduce.go's existing primitives
// (splitTopLevelParts, balancedParen) split a CREATE TABLE paren body, not a
// "SELECT ... FROM" projection, and neither stops at FROM nor strips " AS
// alias". This file is that parser, kept DELIBERATELY BOUNDED (ADR 0018's
// declared-subset discipline — anchored, bounded scanning over statement
// text, never a general SQL-expression parser):
//
//   - It finds the top-level SELECT ... FROM span (paren-depth 0, outside
//     string/quoted-identifier literals) and splits the projection between
//     them at top-level commas.
//   - Per comma-separated item, the exposed name is: the alias after a
//     top-level " AS alias" when present; else the trailing component of a
//     bare "table.column" / "schema.table.column" reference; else a bare
//     identifier with no dot at all.
//   - Anything else in an item — a function call, CASE, subquery, arithmetic
//     expression, string/numeric literal — WITHOUT an alias has no
//     extractable name. That ONE item is a declared miss (silently skipped,
//     never guessed at). This is the exact line ADR 0004 draws between a
//     bounded scan and a general SQL-expression parser (resolving what
//     columns a subquery/CTE/function-call projection actually yields is
//     OUT OF SCOPE — the ADR-0004 stop condition this package's callers must
//     honor); DB-020 stays on the bounded side of that line.
//   - "SELECT *" cannot be enumerated at all (the column list is not literal
//     in the DDL text) — a declared miss for the WHOLE view, not a silent
//     false negative dressed up as coverage.
//
// Leading-comma style (real AdventureWorks vEmployee: "SELECT\n e.[Col1]\n
// ,p.[Col2]\n ...") needs NO special case: splitting on the comma BYTE
// itself is agnostic to which side of the line break it sits on.

// selectColumn is one exposed name from a view's top-level SELECT
// projection, with its byte OFFSET within Body.Text — used to compute an
// accurate per-column anchor line via the same "count newlines up to offset"
// arithmetic reduce.go's own applyCreateTable/applyAlterTable already use
// for table columns and ALTER actions.
type selectColumn struct {
	name   string
	offset int
}

// extractSelectColumns returns every exposed name from body's top-level
// SELECT projection, or ok=false when no top-level SELECT/FROM pair exists
// at all, or the projection is exactly "*" (SELECT * — not enumerable).
func extractSelectColumns(body string) ([]selectColumn, bool) {
	selIdx, ok := findTopLevelKeyword(body, "SELECT", 0)
	if !ok {
		return nil, false
	}
	projStart := selIdx + len("SELECT")
	fromIdx, ok := findTopLevelKeyword(body, "FROM", projStart)
	if !ok {
		return nil, false
	}
	proj := body[projStart:fromIdx]

	// consumed tracks how many leading bytes of proj have been skipped
	// (whitespace, then an optional DISTINCT/ALL keyword, then whitespace
	// again) so every item's offset below stays anchored to body, never to a
	// re-sliced string.
	consumed := leadingSpaceLen(proj)
	rest := proj[consumed:]
	for _, kw := range []string{"DISTINCT", "ALL"} {
		if hasCaseInsensitiveWordPrefix(rest, kw) {
			consumed += len(kw)
			adv := leadingSpaceLen(proj[consumed:])
			consumed += adv
			rest = proj[consumed:]
			break
		}
	}
	if strings.TrimSpace(rest) == "*" {
		return nil, false // SELECT * — declared miss, column list not literal in the DDL
	}

	var out []selectColumn
	for _, p := range splitProjectionItems(rest) {
		leadWS := leadingSpaceLen(p.text)
		item := strings.TrimSpace(p.text)
		if item == "" {
			continue
		}
		absOffset := projStart + consumed + p.off + leadWS
		if name, ok := exposedName(item); ok {
			out = append(out, selectColumn{name: name, offset: absOffset})
		}
	}
	return out, true
}

// exposedName returns the exposed column/alias name for one trimmed
// projection item, or ok=false when the item is a bounded-scan miss (see the
// file doc comment for the exact shapes recognized).
func exposedName(item string) (string, bool) {
	if alias, ok := topLevelAsAlias(item); ok {
		return stripIdentQuotes(alias), true
	}
	if !isPlainColumnRef(item) {
		return "", false
	}
	parts := splitQualifiedName(item)
	return stripIdentQuotes(parts[len(parts)-1]), true
}

// topLevelAsAlias returns the alias after the first top-level (paren-depth
// 0, outside string/quoted-identifier literals) whole-word "AS" in item, or
// ok=false when item has no top-level AS — e.g. "CAST(x AS int)" has its AS
// at paren-depth 1, correctly ignored.
func topLevelAsAlias(item string) (string, bool) {
	idx, ok := findTopLevelKeyword(item, "AS", 0)
	if !ok {
		return "", false
	}
	alias := strings.TrimSpace(item[idx+len("AS"):])
	if alias == "" {
		return "", false
	}
	return alias, true
}

// identPart matches either a canonical ANSI-quoted identifier ("..." — every
// dialect's bracket/backtick-quoted identifier is re-emitted as this form by
// split() before reduce.go/dbrules ever see it) or a bare identifier.
const identPart = `("[^"]*"|[A-Za-z_][A-Za-z0-9_]*)`

// rePlainColumnRef matches ONLY a bare or dotted identifier chain — no
// parens, no operators, no literals, no whitespace — e.g. "password",
// "a.actor_id", "e.\"BusinessEntityID\"", "\"schema\".\"table\".\"col\"".
// Anything else (a function call, CASE, arithmetic) does not match, and is a
// declared miss in exposedName.
var rePlainColumnRef = regexp.MustCompile(`^` + identPart + `(\.` + identPart + `)*$`)

func isPlainColumnRef(item string) bool {
	return rePlainColumnRef.MatchString(item)
}

// splitQualifiedName splits an already-validated (isPlainColumnRef) dotted
// identifier chain into its parts, respecting ANSI double-quoted segments
// (a quoted identifier may itself legally contain a '.').
func splitQualifiedName(item string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(item); i++ {
		c := item[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			cur.WriteByte(c)
		case c == '.' && !inQuote:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	parts = append(parts, cur.String())
	return parts
}

// stripIdentQuotes removes a canonical ANSI "..." quoting, un-doubling any
// embedded "" escape, or returns s unchanged when it is not quoted.
func stripIdentQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return s
}

// commaItem is a comma-separated fragment with its byte offset in the
// source it was split from.
type commaItem struct {
	text string
	off  int
}

// splitProjectionItems splits s (a SELECT projection's top-level column
// list) by commas at paren-depth 0, respecting BOTH single-quoted string
// literals and ANSI-canonical double-quoted identifiers.
//
// This is DB-020's OWN primitive — deliberately NOT a reuse of reduce.go's
// splitTopLevelParts/balancedParen, which track single-quoted strings ONLY
// (architecture/unit-c-tsql-risk-reeval, obs #1056, finding R3). Every
// dialect's bracket/backtick-quoted identifier is re-emitted as canonical
// "..." by split() before reduce.go/dbrules ever see it, so a projection
// containing one (e.g. a T-SQL "[zip code]" alias, canonicalized to
// "zip code") WOULD mis-split with those primitives if the quoted text
// contained a ',' or '(' — this function tracks both quote forms explicitly
// so that never happens here. Locked by
// TestSplitProjectionItems_QuotedIdentifierWithCommaIsNotMisSplit.
func splitProjectionItems(s string) []commaItem {
	var out []commaItem
	depth, start := 0, 0
	inStr, inIdent := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			if c == '\'' {
				inStr = false
			}
		case inIdent:
			if c == '"' {
				inIdent = false
			}
		case c == '\'':
			inStr = true
		case c == '"':
			inIdent = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, commaItem{s[start:i], start})
			start = i + 1
		}
	}
	out = append(out, commaItem{s[start:], start})
	return out
}

// findTopLevelKeyword returns the index in s where keyword (case-
// insensitive, whole word) first occurs at paren-depth 0, outside single-
// quoted string literals and ANSI-canonical double-quoted identifiers,
// searching from byte offset from. ok=false when no such occurrence exists.
//
// This is DB-020's own bounded keyword scanner, not a general SQL
// tokenizer: it tracks only enough state (paren depth, '\” strings, '"'
// identifiers) to find SELECT/FROM/AS reliably — the same declared-subset
// discipline reduce.go's own regexes already apply to CREATE TABLE/INDEX
// (ADR 0018).
func findTopLevelKeyword(s, keyword string, from int) (int, bool) {
	depth := 0
	inStr, inIdent := false, false
	kw := strings.ToUpper(keyword)
	for i := from; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			if c == '\'' {
				inStr = false
			}
			continue
		case inIdent:
			if c == '"' {
				inIdent = false
			}
			continue
		case c == '\'':
			inStr = true
			continue
		case c == '"':
			inIdent = true
			continue
		case c == '(':
			depth++
			continue
		case c == ')':
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if isIdentByte(prevByte(s, i)) {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(s[i:]), kw) {
			end := i + len(kw)
			if end == len(s) || !isIdentByte(s[end]) {
				return i, true
			}
		}
	}
	return 0, false
}

func prevByte(s string, i int) byte {
	if i == 0 {
		return ' '
	}
	return s[i-1]
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func leadingSpaceLen(s string) int {
	return len(s) - len(strings.TrimLeftFunc(s, unicode.IsSpace))
}

// hasCaseInsensitiveWordPrefix reports whether s starts with kw (case
// insensitive) followed by a word boundary (whitespace or end of string) —
// so e.g. "DISTINCTCOUNT" is never mis-read as the keyword "DISTINCT".
func hasCaseInsensitiveWordPrefix(s, kw string) bool {
	if len(s) < len(kw) || !strings.EqualFold(s[:len(kw)], kw) {
		return false
	}
	return len(s) == len(kw) || isSpaceByte(s[len(kw)])
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
