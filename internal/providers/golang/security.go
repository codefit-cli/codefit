package golang

import (
	"go/ast"
	"go/token"

	"github.com/codefit-cli/codefit/internal/core/findings"
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
		if val, ok := stringLit(a.Rhs[i]); ok && val != "" && looksSecret(ident.Name) {
			add(secretFinding(p, ident.Name, ident.Pos()))
		}
	}
}

func checkSecretValueSpec(p *parsed, v *ast.ValueSpec, add func(findings.Finding)) {
	for i, name := range v.Names {
		if i >= len(v.Values) {
			continue
		}
		if val, ok := stringLit(v.Values[i]); ok && val != "" && looksSecret(name.Name) {
			add(secretFinding(p, name.Name, name.Pos()))
		}
	}
}

func checkSecretKeyValue(p *parsed, kv *ast.KeyValueExpr, add func(findings.Finding)) {
	key, ok := kv.Key.(*ast.Ident)
	if !ok {
		return
	}
	if val, ok := stringLit(kv.Value); ok && val != "" && looksSecret(key.Name) {
		add(secretFinding(p, key.Name, key.Pos()))
	}
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
