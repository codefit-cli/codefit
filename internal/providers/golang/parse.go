package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
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

// looksSecret reports whether an identifier name suggests a secret. Bare "key"
// only counts with a sufficiently long value to limit false positives.
func looksSecret(name string, valueLen int) bool {
	l := strings.ToLower(name)
	for _, kw := range []string{"secret", "passwd", "password", "token", "apikey"} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return strings.Contains(l, "key") && valueLen >= 16
}

// looksSecurityValue reports whether a name suggests a security-sensitive value
// (for the math/rand check).
func looksSecurityValue(name string) bool {
	l := strings.ToLower(name)
	for _, kw := range []string{"token", "secret", "nonce", "salt", "key", "password", "iv", "session"} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
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
