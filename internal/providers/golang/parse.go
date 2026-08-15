package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/namematch"
)

// parsed holds a parsed Go file plus the lookups the checks need.
type parsed struct {
	file    *ast.File
	fset    *token.FileSet
	imports map[string]string // local name -> import path
	path    string
}

func parse(path string, content []byte) (*parsed, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, content, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	return &parsed{file: f, fset: fset, imports: importMap(f), path: path}, nil
}

// importMap maps each import's local name to its import path.
func importMap(f *ast.File) map[string]string {
	m := make(map[string]string)
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := path
		if imp.Name != nil {
			name = imp.Name.Name
		} else if i := strings.LastIndex(path, "/"); i >= 0 {
			name = path[i+1:]
		}
		m[name] = path
	}
	return m
}

func (p *parsed) line(pos token.Pos) int {
	return p.fset.Position(pos).Line
}

// selectorPackagePath returns the import path of the package a selector
// expression (pkg.Sel) refers to, or "" if it is not a package selector.
func (p *parsed) selectorPackagePath(sel *ast.SelectorExpr) string {
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return p.imports[ident.Name]
}

// isStringConcatWithVar reports whether expr is a "+" expression that mixes a
// non-literal operand (a variable) in — i.e., a string built by concatenation
// rather than a single constant literal.
func isStringConcatWithVar(expr ast.Expr) bool {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return false
	}
	return hasNonLiteralOperand(bin)
}

func hasNonLiteralOperand(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return false
		}
		return hasNonLiteralOperand(e.X) || hasNonLiteralOperand(e.Y)
	case *ast.BasicLit:
		return false
	default:
		return true
	}
}

// credentialNames and securityValueNames are resolved once: namematch returns a
// fresh map per call so a consumer cannot mutate its sets, and these gates are
// asked per identifier.
var (
	credentialNames    = namematch.Credential()
	securityValueNames = namematch.SecurityValue()
)

// looksSecret reports whether an identifier NAME denotes a credential.
//
// It asks namematch, which matches by name COMPONENT with adjacent-pair joining
// — never by raw substring, and never by value length. Both of those were
// removed rather than tuned:
//
//   - Raw substring made "tokenizer" and "keyboard" credentials.
//   - A bare "key" name plus a 16-byte value was the arm that reported enum
//     constants such as CategoryDWDimensionNoSurrogateKey as hardcoded
//     credentials at Confidence 1.0. Its length guard was ANTI-CORRELATED with
//     the property it claimed to filter: descriptive kebab/snake values grow
//     past 16 bytes BECAUSE they are descriptive, while a credential has no
//     length floor, so the gate admitted the false-positive class and rejected
//     short real credentials. ADR 0056 section 1 allows teaching a check or
//     dropping it, never declaring a false fact as a limit.
//
// Deleting that arm is only safe BECAUSE of pair joining: Arm A substring-matched
// "apikey", and lower("API_KEY") is "api_key", which does not contain it. Every
// delimiter-separated credential name fired through the deleted arm alone. See
// ADR 0075.
func looksSecret(name string) bool {
	_, ok := namematch.MatchSet(name, credentialNames)
	return ok
}

// looksSecurityValue reports whether a name denotes security-sensitive CRYPTO
// MATERIAL, for the math/rand check.
//
// It shares the matching convention with looksSecret and NOT the vocabulary: a
// nonce is not a credential and an access key is not crypto material. Its own
// set legitimately contains a bare "key" component, because SEC-050 also
// requires the value to come from a math/rand call — a second condition
// SEC-001 has no equivalent of. Component matching is what stops that bare
// "key" from making monkeyIndex a security value.
func looksSecurityValue(name string) bool {
	_, ok := namematch.MatchSet(name, securityValueNames)
	return ok
}

// stringLit returns the unquoted value of a string literal expression and
// whether expr was indeed a string literal.
func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(lit.Value, "`\""), true
}
