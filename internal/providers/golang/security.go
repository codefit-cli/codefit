package golang

import (
	"go/ast"
	"go/token"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
)

// securityChecks runs every static security detector over a parsed file.
func securityChecks(p *parsed) []findings.Finding {
	var out []findings.Finding
	add := func(f findings.Finding) { out = append(out, f) }

	ast.Inspect(p.file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			checkSecretAssign(p, node, add)
			checkMathRandAssign(p, node, add)
		case *ast.ValueSpec:
			checkSecretValueSpec(p, node, add)
		case *ast.KeyValueExpr:
			checkSecretKeyValue(p, node, add)
		case *ast.CallExpr:
			checkWeakHash(p, node, add)
			checkSQLInjection(p, node, add)
			checkCommandInjection(p, node, add)
			checkGetenvInline(p, node, add)
		}
		return true
	})
	return out
}

func secFinding(p *parsed, id string, sev findings.Severity, pos token.Pos, title, desc, sugg string) findings.Finding {
	return findings.Finding{
		ID:          id,
		Dimension:   findings.DimensionSecurity,
		Severity:    sev,
		File:        p.path,
		Line:        p.line(pos),
		Title:       title,
		Description: desc,
		Suggestion:  sugg,
		Confidence:  1.0,
	}
}

func checkSecretAssign(p *parsed, a *ast.AssignStmt, add func(findings.Finding)) {
	if len(a.Lhs) != len(a.Rhs) {
		return
	}
	for i, lhs := range a.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok {
			continue
		}
		if hardcodedSecret(ident.Name, a.Rhs[i]) {
			add(secretFinding(p, ident.Name, ident.Pos()))
		}
	}
}

func checkSecretValueSpec(p *parsed, v *ast.ValueSpec, add func(findings.Finding)) {
	for i, name := range v.Names {
		if i >= len(v.Values) {
			continue
		}
		if hardcodedSecret(name.Name, v.Values[i]) {
			add(secretFinding(p, name.Name, name.Pos()))
		}
	}
}

func checkSecretKeyValue(p *parsed, kv *ast.KeyValueExpr, add func(findings.Finding)) {
	key, ok := kv.Key.(*ast.Ident)
	if !ok {
		return
	}
	if hardcodedSecret(key.Name, kv.Value) {
		add(secretFinding(p, key.Name, key.Pos()))
	}
}

// hardcodedSecret is SEC-001's whole decision for one name/value pair, shared
// by the three AST shapes that can carry one (assignment, value spec, composite
// literal field) so they cannot disagree.
//
// It asks two questions, in this order, of ONE decoded value — the string the
// literal denotes, from stringLit:
//
//  1. The NAME gate (ADR 0075): value is a non-empty string literal and name
//     denotes a credential (looksSecret).
//  2. The VALUE veto (ADR 0086): the value's own shape does not prove it is
//     something else — see provesNotACredential.
//
// A vetoed pair is not a finding of any kind: nothing is emitted, at any
// severity or confidence. What still fires does so exactly as before.
func hardcodedSecret(name string, value ast.Expr) bool {
	val, ok := stringLit(value)
	if !ok || val == "" || !looksSecret(name) {
		return false
	}
	return !provesNotACredential(val)
}

// limitValueVeto is the DECLARED consequence of the value veto, rendered to
// agents by codefit-coverage through Capability(). It lives beside the code
// that applies the veto and is cited from capability.go by reference.
//
// It is a Go-specific prefix — which sites apply the veto and what reaches the
// predicate — followed by valueshape.DeclaredLimit, the one copy of what the
// veto does and does not see, in both directions, that the TypeScript manifest
// cites too. Restating that text here by hand is how the two drifted before.
// The prefix carries no form list of its own: TestCapability_SEC001DeclaresTheValueVeto
// parses exactly one, the constant's.
const limitValueVeto = "The Go provider applies the value veto at every site SEC-001 reaches " +
	"(an assignment, a const or var spec, a composite-literal element keyed by an identifier): the string the " +
	"literal denotes, escapes decoded by strconv.Unquote, is handed to " +
	"valueshape.NotACredential, the function TypeScript's metavariable-value-veto calls. " +
	valueshape.DeclaredLimit

// provesNotACredential reports whether denoted — the string a literal the name
// gate accepted DENOTES, as stringLit decoded it — has a shape that proves it
// is not a credential.
//
// It must be the denoted string, not the literal's source text:
// `"…?api_key\x3dabc"` denotes a URL carrying api_key=abc, and reading the
// characters between the quotes instead would lose the '=' and veto it.
//
// Every guard lives in internal/core/valueshape, and this function applies
// none of its own. limitValueVeto, above, serves their limits to an agent by
// citing valueshape.DeclaredLimit, so there is no prose here to keep in step.
func provesNotACredential(denoted string) bool {
	_, vetoed := valueshape.NotACredential(denoted)
	return vetoed
}

func secretFinding(p *parsed, name string, pos token.Pos) findings.Finding {
	return secFinding(p, "SEC-001", findings.SeverityHigh, pos,
		"Hardcoded secret",
		"The value assigned to '"+name+"' looks like a hardcoded credential.",
		"Move it to an environment variable or the OS keychain; never commit secrets.")
}

func checkMathRandAssign(p *parsed, a *ast.AssignStmt, add func(findings.Finding)) {
	// Flag math/rand only when the result feeds a security-named variable.
	securityTarget := false
	for _, lhs := range a.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok && looksSecurityValue(ident.Name) {
			securityTarget = true
		}
	}
	if !securityTarget {
		return
	}
	for _, rhs := range a.Rhs {
		if call, ok := rhs.(*ast.CallExpr); ok && pkgCall(p, call, "math/rand") {
			add(secFinding(p, "SEC-050", findings.SeverityMedium, call.Pos(),
				"Insecure randomness for a security value",
				"math/rand is not cryptographically secure but feeds a security-sensitive value.",
				"Use crypto/rand for tokens, salts, nonces and keys."))
		}
	}
}

func checkWeakHash(p *parsed, call *ast.CallExpr, add func(findings.Finding)) {
	if pkgCall(p, call, "crypto/md5", "New", "Sum") {
		add(weakHashFinding(p, "MD5", call.Pos()))
	}
	if pkgCall(p, call, "crypto/sha1", "New", "Sum") {
		add(weakHashFinding(p, "SHA-1", call.Pos()))
	}
}

func weakHashFinding(p *parsed, algo string, pos token.Pos) findings.Finding {
	return secFinding(p, "SEC-052", findings.SeverityMedium, pos,
		"Weak hash algorithm ("+algo+")",
		algo+" is cryptographically broken for security purposes.",
		"Use SHA-256+ for integrity, or bcrypt/argon2 for passwords.")
}

var queryMethods = map[string]bool{
	"Query": true, "QueryRow": true, "QueryContext": true,
	"QueryRowContext": true, "Exec": true, "ExecContext": true,
}

func checkSQLInjection(p *parsed, call *ast.CallExpr, add func(findings.Finding)) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !queryMethods[sel.Sel.Name] {
		return
	}
	for _, arg := range call.Args {
		if isStringConcatWithVar(arg) {
			add(secFinding(p, "SEC-010", findings.SeverityHigh, call.Pos(),
				"Possible SQL injection",
				"A SQL statement is built by string concatenation with a variable.",
				"Use parameterized queries (placeholders) instead of concatenation."))
			return
		}
	}
}

func checkCommandInjection(p *parsed, call *ast.CallExpr, add func(findings.Finding)) {
	if !pkgCall(p, call, "os/exec", "Command", "CommandContext") {
		return
	}
	for _, arg := range call.Args {
		if isStringConcatWithVar(arg) {
			add(secFinding(p, "SEC-013", findings.SeverityHigh, call.Pos(),
				"Possible command injection",
				"An exec command is built by string concatenation with a variable.",
				"Pass arguments as separate slice elements and validate untrusted input."))
			return
		}
	}
}

func checkGetenvInline(p *parsed, call *ast.CallExpr, add func(findings.Finding)) {
	// Skip when this call is itself os.Getenv; we flag the enclosing use.
	if pkgCall(p, call, "os", "Getenv") {
		return
	}
	for _, arg := range call.Args {
		inner, ok := arg.(*ast.CallExpr)
		if ok && pkgCall(p, inner, "os", "Getenv") {
			add(secFinding(p, "SEC-040", findings.SeverityInfo, inner.Pos(),
				"Environment variable used without a default",
				"os.Getenv is used inline without validating it or providing a default.",
				"Read it into a variable and check for the empty string before use."))
		}
	}
}

// pkgCall reports whether call is pkgPath.Fn(...) for one of fns (any function
// when fns is empty).
func pkgCall(p *parsed, call *ast.CallExpr, pkgPath string, fns ...string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || p.selectorPackagePath(sel) != pkgPath {
		return false
	}
	if len(fns) == 0 {
		return true
	}
	for _, fn := range fns {
		if sel.Sel.Name == fn {
			return true
		}
	}
	return false
}
