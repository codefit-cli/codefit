package typescript

import (
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Capability declares what the TypeScript provider implements. Security is
// Enumerable:true — its 9 rule IDs are the exact set loaded by
// ruleengine.LoadFS(rules.FS, "typescript/security") (securityRules, above),
// so Control A (capability_test.go's set-equality test) checks Declared
// against the real loader, not a copy. Practices is Enumerable:false with an
// empty Declared: AnalyzePractices is a stub (see typescript.go) — nothing is
// implemented yet, so nothing is declared. Surface lists all four categories
// this provider's surfaceQueries() run (idor, authz, overfetch, nplus1).
// CoverageManifest is true: the provider implements CoverageManifest()
// (coverage.go).
func (*Provider) Capability() providers.Capability {
	return providers.Capability{
		Security: providers.RuleSet{
			Declared: []string{
				"SEC-001", "SEC-010", "SEC-011", "SEC-014",
				"SEC-052", "SEC-053", "SEC-058", "SEC-079", "SEC-080",
			},
			Enumerable: true,
		},
		Practices: providers.RuleSet{
			Declared:   nil,
			Enumerable: false,
		},
		Surface: []surface.Category{
			surface.CategoryIDOR, surface.CategoryAuthz,
			surface.CategoryOverfetch, surface.CategoryNPlus1,
		},
		CoverageManifest: true,
	}
}
