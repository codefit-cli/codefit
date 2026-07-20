package dbrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db041 — a TRIGGER whose body invokes an EXTERNAL-EFFECTING call: one that
// reaches OUTSIDE the database (a shell exec, OLE automation, email, a remote /
// cross-database query, an async notification, or a pipe to a program). It is
// SURFACE, never an affirmation (same epistemology as DB-020/DB-031/DB-040): the
// rule states the structural FACT "this trigger makes external call(s): X" and
// the AGENT judges whether the call is safe. DB-041 detects the external call;
// it does not decide whether it is exploitable.
//
// STRICT vocabulary — the line between real risk and noise. An EXECUTE/CALL of
// an INTERNAL stored procedure (EXECUTE dbo.uspLogError, CALL recompute_totals)
// is a normal routine call and does NOT fire; only calls that leave the database
// count. This is the rule's signature trap (cf. RAISE EXCEPTION in DB-031 and
// UPDATE(column) in DB-040): a token that looks like a call but is not external.
//
//   - T-SQL:      xp_cmdshell, sp_OA* (OLE automation), sp_send_dbmail,
//     OPENROWSET, OPENQUERY.
//   - PostgreSQL: dblink / dblink_exec (remote query), NOTIFY / pg_notify (async
//     signal out of the transaction), COPY ... PROGRAM (shell pipe).
//   - MySQL:      sys_exec / sys_eval (UDFs — structurally rare; see COVERAGE:
//     MySQL is detectable-without-dogfood, the scanner would fire if
//     one appeared, but no idiomatic real case is dogfooded).
//
// Body source is per-dialect and a PostgreSQL trigger is resolved to its executed
// function (ADR 0026), exactly as DB-040 does (shared triggerBodyToScan). Gated
// on Body.Complete (ADR 0004/0025). The scanner is the same bounded, string/
// comment-aware token walker: an external-call token inside a string literal or a
// comment does not fire, and a token used as a member access (NEW.notify) is not
// mistaken for the NOTIFY statement.
type db041 struct{}

func (db041) ID() string { return "DB-041" }

func (db041) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Triggers {
		bodyText, complete := triggerBodyToScan(s, t)
		if bodyText == "" || !complete {
			continue // unresolvable, or a body the parser could not prove whole — abstain
		}
		calls := externalCalls(bodyText)
		if len(calls) == 0 {
			continue // no external-effecting call — an internal routine call is not one
		}
		signals := []string{"trigger: " + t.Name}
		for _, c := range calls {
			signals = append(signals, "external_call: "+c)
		}
		out = append(out, findings.SurfaceItem{
			Category:          string(surface.CategoryDBTriggerExternalCall),
			File:              t.Pos.File,
			Line:              t.Pos.Line,
			StructuralSignals: signals,
			StructuralFacts: map[string]bool{
				"external_call_found": true,
			},
			ReasonToReview: "This trigger invokes a call that reaches OUTSIDE the database " +
				"(shell/OLE/email/remote/notify). A trigger with external side effects is hard to " +
				"reason about and can fail or leak in surprising ways. Is the external call necessary " +
				"and safe? (DB-041 states the external call as a fact; whether it is exploitable is a " +
				"judgment codefit does not make. An EXECUTE of an internal stored procedure is not " +
				"counted here.)",
		})
	}
	return nil, out
}

// externalExactTokens are single lowercased identifier tokens that are external-
// effecting on their own.
var externalExactTokens = map[string]bool{
	"xp_cmdshell":    true, // T-SQL shell exec
	"sp_send_dbmail": true, // T-SQL email
	"openrowset":     true, // T-SQL ad-hoc remote/external source
	"openquery":      true, // T-SQL linked-server query
	"notify":         true, // PostgreSQL async signal
	"pg_notify":      true, // PostgreSQL async signal (function form)
	"sys_exec":       true, // MySQL sys UDF — shell exec
	"sys_eval":       true, // MySQL sys UDF — shell eval
}

// externalCalls scans a routine body and returns the deduplicated set of
// external-effecting call tokens it invokes (in first-seen order), string- and
// comment-aware. A token used as a member access (preceded by '.') is ignored,
// and COPY ... PROGRAM is recognized as the pair.
func externalCalls(body string) []string {
	var found []string
	seen := map[string]bool{}
	copySeen := false
	// prevSig is the last significant (non-space) byte before the current token —
	// used to skip a member access like NEW.notify.
	var prevSig byte

	i, n := 0, len(body)
	for i < n {
		c := body[i]

		if c == '-' && i+1 < n && body[i+1] == '-' {
			i += 2
			for i < n && body[i] != '\n' {
				i++
			}
			prevSig = 0
			continue
		}
		if c == '/' && i+1 < n && body[i+1] == '*' {
			i += 2
			for i+1 < n && (body[i] != '*' || body[i+1] != '/') {
				i++
			}
			i += 2
			prevSig = 0
			continue
		}
		if c == '\'' {
			i++
			for i < n && body[i] != '\'' {
				i++
			}
			i++
			prevSig = '\''
			continue
		}
		if isIdentStart(c) {
			start := i
			for i < n && isIdentByte(body[i]) {
				i++
			}
			memberAccess := prevSig == '.'
			word := strings.ToLower(body[start:i])

			if tok := externalToken(word, &copySeen); tok != "" && !seen[tok] {
				// Only the generic token NOTIFY needs the member-access guard: a
				// column reference like NEW.notify is not the NOTIFY statement.
				// The distinctive tokens (xp_cmdshell, sp_send_dbmail, sp_OA*,
				// openrowset, dblink) are real even when schema-qualified
				// (msdb.dbo.sp_send_dbmail), so qualification must NOT suppress them.
				if tok != "notify" || !memberAccess {
					seen[tok] = true
					found = append(found, tok)
				}
			}
			prevSig = body[i-1]
			continue
		}
		if c == ';' {
			copySeen = false // a COPY and a later PROGRAM in different statements do not pair
		}
		if !isSpaceByte(c) {
			prevSig = c
		}
		i++
	}
	return found
}

// externalToken classifies an identifier word as an external-effecting call
// token, returning its canonical name or "". It also tracks COPY ... PROGRAM via
// copySeen: COPY arms it, and a later PROGRAM in the same statement fires.
func externalToken(word string, copySeen *bool) string {
	switch {
	case word == "copy":
		*copySeen = true
		return ""
	case word == "program" && *copySeen:
		return "copy program"
	case externalExactTokens[word]:
		return word
	case strings.HasPrefix(word, "sp_oa"): // sp_OACreate / sp_OAMethod / … (OLE automation)
		return word
	case strings.HasPrefix(word, "dblink"): // dblink / dblink_exec / dblink_connect (remote)
		return word
	}
	return ""
}
