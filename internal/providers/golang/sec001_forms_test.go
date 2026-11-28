package golang_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/golang"
)

// The value forms Go's SEC-001 never reaches (issue #148). hardcodedSecret asks
// stringLit for the value, and stringLit accepts exactly one *ast.BasicLit of
// kind STRING: any other expression is not a string literal to it, so the pair
// is neither vetoed nor reported, whatever the expression holds. The value-axis
// parity table (internal/core/valueshape/crossprovider_test.go) already records
// the concatenation against TypeScript; this is the Go provider's own negative
// fixture, at every site, with the reason for each form, and it ties each form to
// the sentence of the served SEC-001 limit that declares it.
//
// If a row starts firing, Go's SEC-001 widened: move the row and update
// capability.go and COVERAGE.md in the same commit.
func TestSEC001ValueFormsGoNeverReaches(t *testing.T) {
	cases := []struct {
		form  string
		value string // Go source for the whole value expression
		// declared is the phrase of the served SEC-001 limit that names this
		// form; it must appear in the Go capability's SEC-001 limits.
		declared string
		reason   string
	}{
		{"concatenation of a secret", `"s3cr" + "3t"`, "a concatenation of literals",
			"a *ast.BinaryExpr, not a basic literal, so stringLit refuses it"},
		{"concatenation of a path", `"lib/" + "config.ts"`, "a concatenation of literals",
			"unseen, not vetoed: the expression never reaches the value veto either"},
		{"parenthesized literal", `("s3cr3t")`, "a parenthesized literal",
			"a *ast.ParenExpr wraps the literal, and stringLit does not look inside it"},
		{"conversion", `string("s3cr3t")`, "a conversion",
			"a *ast.CallExpr, not a basic literal"},
		{"reference to a constant", `defaultSigningValue`, "a reference to a constant or a variable",
			"an *ast.Ident; the provider parses with no type information, so it cannot follow the name to its value"},
		// Go has no ||, ?? or ternary on strings. The idiom that gives a string
		// a fallback is a call, which is unseen like any other.
		{"fallback through cmp.Or", `cmp.Or(os.Getenv("SIGNING_KEY"), "s3cr3t")`, "a call that returns a fallback",
			"a *ast.CallExpr: the literal is an argument, and SEC-001 reads only the value itself"},
	}

	served := servedSEC001Limits()

	for _, tc := range cases {
		if tc.reason == "" {
			t.Errorf("%s: no reason written", tc.form)
			continue
		}
		if !strings.Contains(served, tc.declared) {
			t.Errorf("%s: the served SEC-001 limits do not declare it (missing %q), so an agent reads silence as clean",
				tc.form, tc.declared)
		}
		for _, site := range sec001Sites {
			t.Run(tc.form+"/"+site.shape, func(t *testing.T) {
				// The probe value is not what keeps the row quiet: the same site
				// fires on the bare literal.
				requireSiteReachesTheGate(t, site)
				if got := sec001Of(t, site.src(gatedName, tc.value)); len(got) != 0 {
					t.Errorf("SEC-001 fired on %s — %s; Go's SEC-001 widened, so move the row and update the declaration: %+v",
						tc.value, tc.reason, got)
				}
			})
		}
	}
}

// TestSEC001ShapesGoNeverReaches is the sibling for the LEFT-hand side: shapes
// whose name never reaches the name gate, so whatever literal they hold is
// neither vetoed nor reported. securityChecks reads an assignment's left side
// only as an *ast.Ident and a composite-literal key only as an *ast.Ident.
// Measured 2026-11-28 (found by the T7 verifier, the same at the pre-feature
// baseline); declared, not fixed.
func TestSEC001ShapesGoNeverReaches(t *testing.T) {
	cases := []struct {
		shape    string
		src      string
		declared string
		reason   string
	}{
		{"selector on the left of an assignment",
			"package p\n\ntype cfg struct{ signingKey string }\n\nfunc f(c *cfg) { c.signingKey = \"s3cr3t\" }\n",
			"an assignment to a field through a selector",
			"the left side is an *ast.SelectorExpr; checkSecretAssign reads only an *ast.Ident"},
		{"string map key",
			"package p\n\nvar cfg = map[string]string{\"signingKey\": \"s3cr3t\"}\n",
			"a string map key",
			"the key is an *ast.BasicLit; checkSecretKeyValue reads only an *ast.Ident key"},
	}
	served := servedSEC001Limits()
	for _, tc := range cases {
		if tc.reason == "" {
			t.Errorf("%s: no reason written", tc.shape)
			continue
		}
		if !strings.Contains(served, tc.declared) {
			t.Errorf("%s: the served SEC-001 limits do not declare it (missing %q)", tc.shape, tc.declared)
		}
		if got := sec001Of(t, tc.src); len(got) != 0 {
			t.Errorf("SEC-001 fired on a %s — %s; Go's SEC-001 widened, so move the row and update the declaration: %+v",
				tc.shape, tc.reason, got)
		}
	}
	// The name and the value are not what keeps these quiet: as a plain
	// assignment and as an identifier-keyed field they fire.
	for _, site := range sec001Sites {
		requireSiteReachesTheGate(t, site)
	}
}

// servedSEC001Limits joins every SEC-001 limit the Go provider serves.
func servedSEC001Limits() string {
	var limits strings.Builder
	for _, l := range golang.New().Capability().Security.Limits {
		if l.ID == "SEC-001" {
			limits.WriteString(l.Limit)
			limits.WriteString("\n")
		}
	}
	return limits.String()
}
