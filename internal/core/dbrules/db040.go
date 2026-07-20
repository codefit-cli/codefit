package dbrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db040 — a TRIGGER whose body performs DML (INSERT/UPDATE/DELETE) against a
// table OTHER than the trigger's OWN table: a cross-table cascade. It is
// SURFACE, never an affirmation (same epistemology as DB-020/DB-031): the rule
// states one structural FACT — "this trigger writes to other table(s): X, Y;
// documented_by_comment: <bool>" — and the AGENT judges whether the cascade is
// intentional or correct. DB-040 detects the cross-table write; it does not
// grade whether the cascade is right.
//
// Body source is per-dialect (ADR 0026):
//
//   - MySQL / T-SQL: the trigger carries an INLINE body — Body.Text is scanned
//     directly, and Table is the trigger's own table.
//   - PostgreSQL: the trigger has NO inline body — its logic lives in the
//     executed function. The rule resolves Schema.ExecutedProcedure(t) and scans
//     THAT function's body, still comparing writes against the TRIGGER's table.
//     A trigger naming a built-in (e.g. tsvector_update_trigger) resolves to
//     nothing, so the rule abstains — it cannot see the logic, so it says
//     nothing rather than guess.
//
// Body-completeness gate (ADR 0004/0025): whichever body is scanned, a
// Complete==false body is never evaluated — affirming a cross-table write (or
// its absence) over text the parser could not prove whole would be a lie.
//
// The scanner is a deliberately BOUNDED, string/comment-aware token walker
// (mirroring DB-020/DB-031, ADR 0018), NOT a SQL-expression parser. It skips
// single-quoted string literals and -- / /* */ comments so a DML-shaped token
// inside a string or comment never fabricates a phantom write, and it excludes
// the T-SQL "UPDATE(column)" column-changed function — "IF UPDATE(ProductID)"
// tests whether a column was updated and names no table; treating it as DML
// would invent a cross-table target.
type db040 struct{}

func (db040) ID() string { return "DB-040" }

func (db040) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Triggers {
		bodyText, complete := db040bodyToScan(s, t)
		if bodyText == "" || !complete {
			continue // unresolvable, or a body the parser could not prove whole — abstain
		}
		others, documented := crossTableWrites(bodyText, t.Table)
		if len(others) == 0 {
			continue // no cross-table write — not a cascade
		}
		signals := []string{"trigger: " + t.Name}
		for _, ot := range others {
			signals = append(signals, "writes_other_table: "+ot)
		}
		out = append(out, findings.SurfaceItem{
			Category:          string(surface.CategoryDBTriggerCrossTableCascade),
			File:              t.Pos.File,
			Line:              t.Pos.Line,
			StructuralSignals: signals,
			StructuralFacts: map[string]bool{
				"documented_by_comment": documented,
			},
			ReasonToReview: "This trigger writes to table(s) other than the one it fires on — a " +
				"cross-table cascade. Is the cascade intentional and documented, or an unintended " +
				"side effect? (DB-040 states the cross-table write as a fact; whether the cascade is " +
				"correct is a judgment codefit does not make.)",
		})
	}
	return nil, out
}

// db040bodyToScan returns the body text DB-040 should scan for trigger t, and
// whether that body is Complete. For a PostgreSQL-shaped trigger (ExecutesFunction
// set) it resolves the executed function and returns ITS body; an unresolvable
// function yields ("", false) so the caller abstains. For an inline-body trigger
// (MySQL/T-SQL) it returns the trigger's own body.
func db040bodyToScan(s *db.Schema, t db.Trigger) (string, bool) {
	if t.ExecutesFunction != "" {
		fn, ok := s.ExecutedProcedure(t)
		if !ok {
			return "", false // built-in / unresolvable — cannot see the logic, abstain
		}
		return fn.Body.Text, fn.Body.Complete
	}
	return t.Body.Text, t.Body.Complete
}

// crossTableWrites scans a routine body for INSERT/UPDATE/DELETE statements whose
// target table differs from ownTable, returning the deduplicated set of other
// tables written (in first-seen order) and whether a comment preceded any of
// them (the documented_by_comment fact). It is string- and comment-aware and
// excludes the T-SQL UPDATE(column) function.
func crossTableWrites(body, ownTable string) (others []string, documented bool) {
	own := normalizeTableRef(ownTable)
	seen := map[string]bool{}
	commentSeen := false
	// prevWord is the uppercased previous identifier token. It disambiguates a
	// DML keyword in the trigger's EVENT clause (AFTER UPDATE, INSTEAD OF DELETE,
	// INSERT OR UPDATE) from a real DML statement: an INSERT/UPDATE/DELETE
	// immediately preceded by AFTER/BEFORE/OF/OR names the fired-on event, not a
	// written table, so it is skipped.
	prevWord := ""

	i, n := 0, len(body)
	for i < n {
		c := body[i]

		// -- line comment.
		if c == '-' && i+1 < n && body[i+1] == '-' {
			commentSeen = true
			i += 2
			for i < n && body[i] != '\n' {
				i++
			}
			continue
		}
		// /* */ block comment.
		if c == '/' && i+1 < n && body[i+1] == '*' {
			commentSeen = true
			i += 2
			for i+1 < n && (body[i] != '*' || body[i+1] != '/') {
				i++
			}
			i += 2
			continue
		}
		// single-quoted string literal — a DML-shaped token inside is not a write.
		if c == '\'' {
			i++
			for i < n && body[i] != '\'' {
				i++
			}
			i++
			continue
		}
		// an identifier word.
		if isIdentStart(c) {
			start := i
			for i < n && isIdentByte(body[i]) {
				i++
			}
			word := strings.ToUpper(body[start:i])

			// A DML keyword in the trigger's event clause (AFTER UPDATE, INSTEAD
			// OF DELETE, INSERT OR UPDATE) is the fired-on event, not a statement.
			if isTriggerEventContext(prevWord) && (word == "INSERT" || word == "UPDATE" || word == "DELETE") {
				prevWord = word
				continue
			}

			var target string
			switch word {
			case "INSERT":
				// INSERT INTO <table>
				j := skipWsAndComments(body, i)
				if kw, k := readWord(body, j); strings.ToUpper(kw) == "INTO" {
					target = readTableRef(body, skipWsAndComments(body, k))
				}
			case "DELETE":
				// DELETE [FROM] <table>
				j := skipWsAndComments(body, i)
				kw, k := readWord(body, j)
				if strings.ToUpper(kw) == "FROM" {
					j = skipWsAndComments(body, k)
				}
				target = readTableRef(body, j)
			case "UPDATE":
				// UPDATE <table> — but NOT the T-SQL UPDATE(column) function, whose
				// next non-space byte is '('.
				j := skipWsAndComments(body, i)
				if j < n && body[j] != '(' {
					target = readTableRef(body, j)
				}
			}

			if target != "" && !strings.EqualFold(normalizeTableRef(target), own) {
				nt := normalizeTableRef(target)
				if !seen[nt] {
					seen[nt] = true
					others = append(others, nt)
					if commentSeen {
						documented = true
					}
				}
			}
			prevWord = word
			continue
		}
		i++
	}
	return others, documented
}

// isTriggerEventContext reports whether prevWord marks a DML keyword as part of
// a trigger's event clause (AFTER UPDATE, BEFORE INSERT, INSTEAD OF DELETE,
// INSERT OR UPDATE) rather than a statement in the body.
func isTriggerEventContext(prevWord string) bool {
	switch prevWord {
	case "AFTER", "BEFORE", "OF", "OR":
		return true
	}
	return false
}

// skipWsAndComments advances past whitespace and -- / /* */ comments, returning
// the index of the next significant byte.
func skipWsAndComments(body string, i int) int {
	n := len(body)
	for i < n {
		if isSpaceByte(body[i]) {
			i++
			continue
		}
		if body[i] == '-' && i+1 < n && body[i+1] == '-' {
			i += 2
			for i < n && body[i] != '\n' {
				i++
			}
			continue
		}
		if body[i] == '/' && i+1 < n && body[i+1] == '*' {
			i += 2
			for i+1 < n && (body[i] != '*' || body[i+1] != '/') {
				i++
			}
			i += 2
			continue
		}
		break
	}
	return i
}

// readWord reads an identifier word starting at i, returning it and the index
// just past it; ("", i) if i does not start an identifier.
func readWord(body string, i int) (string, int) {
	n := len(body)
	if i >= n || !isIdentStart(body[i]) {
		return "", i
	}
	start := i
	for i < n && isIdentByte(body[i]) {
		i++
	}
	return body[start:i], i
}

// readTableRef reads a (possibly schema-qualified, possibly quoted/bracketed)
// table reference starting at i — a run of identifier bytes, quotes, brackets,
// backticks, and dots — and returns it raw. "" if i does not start one.
func readTableRef(body string, i int) string {
	n := len(body)
	start := i
	for i < n {
		b := body[i]
		if isIdentByte(b) || b == '"' || b == '[' || b == ']' || b == '`' || b == '.' {
			i++
			continue
		}
		break
	}
	return body[start:i]
}

// normalizeTableRef reduces a table reference to its bare final component: it
// takes the last dot-separated segment and strips surrounding quotes/brackets/
// backticks, so `"Purchasing"."PurchaseOrderDetail"` and `PurchaseOrderDetail`
// compare equal.
func normalizeTableRef(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ".")
	last := parts[len(parts)-1]
	return strings.Trim(last, "\"[]`")
}
