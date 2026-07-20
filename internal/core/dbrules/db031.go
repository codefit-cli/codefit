package dbrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db031 — a stored procedure/function whose captured body contains NO
// exception-handling construct for its dialect. It is SURFACE, never an
// affirmation (ADR 0015/0017, the same epistemology as DB-020): the rule
// states one structural FACT — "no exception-handling construct was found in
// this routine body" — an ABSENCE. Whether that absence is a defect is the
// AGENT's judgment, and so is the ADEQUACY of any handler that IS present: an
// empty CATCH, or a handler that swallows every error, reads as "present"
// here (the construct exists) yet may still be a real bug. DB-031 detects the
// STRUCTURE, it does not grade it.
//
// Functions AND procedures are both covered: reduce.go builds a single
// db.Procedure for both "CREATE FUNCTION" and "CREATE PROCEDURE", so one pass
// over s.Procedures sees every routine body. TRIGGERS are intentionally out of
// scope for DB-031 (their cross-table/external-effect risks are DB-040/DB-041's
// domain); DB-031 reasons only about procedure/function bodies.
//
// Per-dialect vocabulary (the tokens do not collide across dialects, so the
// scanner recognizes the UNION, but each is matched PRECISELY as a pair of
// adjacent keyword tokens — only whitespace/comments may sit between them):
//
//   - T-SQL:      BEGIN TRY      (the TRY block that pairs with BEGIN CATCH)
//   - MySQL:      HANDLER FOR    (the DECLARE ... HANDLER FOR ... declaration)
//   - PL/pgSQL:   EXCEPTION WHEN (the handler clause inside a BEGIN ... END)
//
// CRITICAL EXCLUSION: PostgreSQL "RAISE EXCEPTION" is a THROW, not a handler.
// The scanner matches EXCEPTION only when the very next keyword token is WHEN;
// "RAISE EXCEPTION 'msg'" has a string literal after EXCEPTION, and a bare
// "EXCEPTION;" has punctuation after it, so neither matches. The real vendored
// Pagila rewards_report RAISEs EXCEPTION twice and has NO handler — DB-031 MUST
// fire on it, and does. A scanner matching bare "EXCEPTION" would silently
// false-negative there; that is this rule's signature failure mode, test-locked
// (TestDB031_Postgres_RewardsReport_RaiseButNoHandler_Fires).
//
// Body-completeness gate (ADR 0004/0025): a routine whose Body.Complete is
// false is NEVER evaluated. Affirming an ABSENCE over text the parser could not
// prove whole would be a lie — the missing handler might sit past the cut. The
// gate is defensive and test-locked (TestDB031_AbstainOnIncompleteBody) even
// though T-SQL bodies are now de-truncated (ADR 0027) so real routine bodies
// arrive Complete.
//
// The scanner is a deliberately BOUNDED, string/comment-aware token walker
// (mirroring DB-020's declared-subset discipline, ADR 0018), NOT a SQL-
// expression parser: it tracks only enough state to skip single-quoted string
// literals and -- / /* */ comments so a handler-shaped token inside a string or
// comment never false-matches, and to recognize whole-word keyword tokens.
type db031 struct{}

func (db031) ID() string { return "DB-031" }

func (db031) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, p := range s.Procedures {
		if !p.Body.Complete {
			continue // ADR 0004/0025 — never affirm an absence over unproven text
		}
		if hasExceptionHandler(p.Body.Text) {
			continue // a handler construct IS present — adequacy is the agent's call
		}
		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDBRoutineNoExceptionHandling),
			File:     p.Pos.File,
			Line:     p.Pos.Line,
			StructuralSignals: []string{
				"routine: " + p.Name,
			},
			StructuralFacts: map[string]bool{
				"exception_handler_found": false,
			},
			ReasonToReview: "No exception-handling construct (T-SQL BEGIN TRY, MySQL DECLARE ... HANDLER, " +
				"PL/pgSQL EXCEPTION WHEN) was found in this routine's body. Should it handle errors " +
				"explicitly, or is unhandled propagation intentional here? (This is the ABSENCE of the " +
				"construct — the adequacy of any handler that WERE present is a separate judgment.)",
		})
	}
	return nil, out
}

// handlerPair is one dialect's exception-handler construct expressed as two
// adjacent keyword tokens: `first` immediately followed (only whitespace or
// comments between) by `second`.
type handlerPair struct{ first, second string }

// exceptionHandlerPairs is the union vocabulary across the three supported
// dialects. They do not collide (BEGIN TRY is T-SQL only, HANDLER FOR MySQL
// only, EXCEPTION WHEN PL/pgSQL only), so scanning the union is precise.
var exceptionHandlerPairs = []handlerPair{
	{"BEGIN", "TRY"},      // T-SQL BEGIN TRY ... BEGIN CATCH
	{"HANDLER", "FOR"},    // MySQL DECLARE ... HANDLER FOR ...
	{"EXCEPTION", "WHEN"}, // PL/pgSQL EXCEPTION WHEN ... (NOT bare/RAISE EXCEPTION)
}

// hasExceptionHandler reports whether body contains any dialect's exception-
// handling construct, matched as an adjacent keyword-token pair outside string
// literals and comments. "Adjacent" means the second keyword is the very next
// keyword token after the first, with only whitespace/comments between — so
// "RAISE EXCEPTION 'msg'" (string after EXCEPTION) and "EXCEPTION;" (punctuation
// after EXCEPTION) do NOT match, only a genuine "EXCEPTION WHEN".
func hasExceptionHandler(body string) bool {
	// pending is the uppercased first-keyword of a candidate pair we have just
	// read and are now looking to immediately follow; "" means none pending.
	pending := ""
	i, n := 0, len(body)
	for i < n {
		c := body[i]

		// -- line comment: skip to end of line.
		if c == '-' && i+1 < n && body[i+1] == '-' {
			pending = ""
			i += 2
			for i < n && body[i] != '\n' {
				i++
			}
			continue
		}
		// /* */ block comment: skip to close.
		if c == '/' && i+1 < n && body[i+1] == '*' {
			pending = ""
			i += 2
			for i+1 < n && (body[i] != '*' || body[i+1] != '/') {
				i++
			}
			i += 2
			continue
		}
		// single-quoted string literal: skip to close (handles '' escape by
		// re-entering on the next iteration).
		if c == '\'' {
			pending = ""
			i++
			for i < n && body[i] != '\'' {
				i++
			}
			i++ // consume closing quote (or run past EOF harmlessly)
			continue
		}
		// whitespace: allowed between the two keywords of a pair — keep pending.
		if isSpaceByte(c) {
			i++
			continue
		}
		// an identifier word (a whole-word token).
		if isIdentStart(c) {
			start := i
			for i < n && isIdentByte(body[i]) {
				i++
			}
			word := strings.ToUpper(body[start:i])
			if pending != "" {
				for _, hp := range exceptionHandlerPairs {
					if hp.first == pending && hp.second == word {
						return true
					}
				}
			}
			// This word did not complete a pair; it may itself OPEN one.
			pending = firstKeyword(word)
			continue
		}
		// any other byte (punctuation, digit, '$', operator): breaks adjacency.
		pending = ""
		i++
	}
	return false
}

// firstKeyword returns word when it is the first token of some handler pair,
// else "" — so only a real opener sets a pending expectation.
func firstKeyword(word string) string {
	for _, hp := range exceptionHandlerPairs {
		if hp.first == word {
			return word
		}
	}
	return ""
}

// isIdentStart reports whether b can begin an identifier word (a letter or
// underscore — a digit cannot start a keyword token).
func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
