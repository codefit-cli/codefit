package dbrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db030 — a stored procedure OR function whose body CONSTRUCTS and runs SQL at
// runtime from a string. It is SURFACE, never an affirmation (same epistemology
// as DB-020/DB-031): the rule states the FACT "this routine builds and runs
// dynamic SQL (marker: X)" and the AGENT judges whether it is injectable —
// codefit maps the surface, it deliberately does not do taint analysis (whether
// the string mixes in untrusted input is the reasoning the agent's own model
// performs over the mapped surface). Applies to procedures AND functions (both
// surface as db.Procedure), like DB-031.
//
// THE TRAP: a STATIC EXEC/CALL of a named internal procedure (EXEC dbo.uspFoo,
// CALL recompute_totals) is NOT dynamic SQL and does not fire. Only a call that
// runs a BUILT string does. This is the same EXEC family as DB-041's trap but
// excluded for a different reason: there it was "not external", here it is "not
// dynamic". An EXEC dbo.uspLogError fires NEITHER rule.
//
// Per-dialect markers (matched as bounded tokens, string/comment-aware):
//
//   - T-SQL:      sp_executesql; EXEC(<expr>) / EXECUTE(<expr>) — EXEC of an
//     expression in parentheses, NOT of a literal proc name.
//   - PL/pgSQL:   EXECUTE '<string>' / EXECUTE format(...) — the dynamic-command
//     statement; and quote_literal / quote_ident, which build a
//     dynamic query string.
//   - MySQL:      PREPARE ... FROM — a prepared statement built from a string.
//
// Gated on Body.Complete (ADR 0004/0025). A bare `EXECUTE <variable>` with no
// visible construction marker in the same body is a declared miss (the string
// may have been built out of view) — a known limit, not a silent one.
type db030 struct{}

func (db030) ID() string { return "DB-030" }

func (db030) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, p := range s.Procedures {
		if !p.Body.Complete {
			continue // ADR 0004/0025 — never affirm over unproven text
		}
		markers := dynamicSQLMarkers(p.Body.Text)
		if len(markers) == 0 {
			continue
		}
		signals := []string{"routine: " + p.Name}
		for _, m := range markers {
			signals = append(signals, "dynamic_sql: "+m)
		}
		out = append(out, findings.SurfaceItem{
			Category:          string(surface.CategoryDBDynamicSQLInRoutine),
			File:              p.Pos.File,
			Line:              p.Pos.Line,
			StructuralSignals: signals,
			StructuralFacts: map[string]bool{
				"dynamic_sql_built": true,
			},
			ReasonToReview: "This routine builds and runs SQL from a string at runtime. If any part " +
				"of that string comes from untrusted input without parameterization, it is a SQL " +
				"injection. Is the dynamic SQL parameterized / does it mix in caller input? (DB-030 " +
				"states that dynamic SQL is constructed here; whether it is injectable is a judgment " +
				"codefit does not make. A static EXEC of a named internal procedure is not counted.)",
		})
	}
	return nil, out
}

// dynamicSQLMarkers scans a routine body and returns the deduplicated dynamic-SQL
// construction markers it finds (in first-seen order), string- and comment-aware.
func dynamicSQLMarkers(body string) []string {
	var found []string
	seen := map[string]bool{}
	add := func(m string) {
		if !seen[m] {
			seen[m] = true
			found = append(found, m)
		}
	}
	prepareSeen := false

	i, n := 0, len(body)
	for i < n {
		c := body[i]

		if c == '-' && i+1 < n && body[i+1] == '-' {
			i += 2
			for i < n && body[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < n && body[i+1] == '*' {
			i += 2
			for i+1 < n && (body[i] != '*' || body[i+1] != '/') {
				i++
			}
			i += 2
			continue
		}
		if c == '\'' {
			i++
			for i < n && body[i] != '\'' {
				i++
			}
			i++
			continue
		}
		if isIdentStart(c) {
			start := i
			for i < n && isIdentByte(body[i]) {
				i++
			}
			word := strings.ToLower(body[start:i])

			switch {
			case word == "sp_executesql":
				add("sp_executesql")
			case word == "quote_literal" || word == "quote_ident":
				add("quote_builder")
			case word == "prepare":
				prepareSeen = true
			case word == "from" && prepareSeen:
				add("prepare_from")
				prepareSeen = false
			case word == "exec" || word == "execute":
				// Dynamic only if what follows is an expression/string/format —
				// NOT a literal procedure name (that is a static call, the trap).
				j := skipWsAndComments(body, i)
				switch {
				case j < n && body[j] == '(':
					add("exec_expr")
				case j < n && body[j] == '\'':
					add("execute_string")
				default:
					if w, _ := readWord(body, j); strings.ToLower(w) == "format" {
						add("execute_format")
					}
				}
			}
			continue
		}
		if c == ';' {
			prepareSeen = false // PREPARE and a later FROM in different statements do not pair
		}
		i++
	}
	return found
}
