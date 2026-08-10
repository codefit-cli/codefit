package golang

import (
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Capability declares what the Go provider implements, measured against
// security.go/practices.go/surface.go at main@ee0b263: 6 security rule IDs, 4
// practices rule IDs (PRAC-004 dropped per ADR 0056), and exactly ONE surface
// category (authz — HTTP handlers, surface.go). Both RuleSets are
// Enumerable:false: Go's detectors have no All()/ID() rule-registry loader
// (unlike TypeScript's YAML-backed security rules), so Declared is a
// hand-maintained mirror of the switch in security.go/practices.go, not a
// derived list — Control A (internal/providers/typescript's Enumerable:true
// declaration) does not apply here. CoverageManifest is false: the Go
// provider implements no CoverageManifest() method today (P1-4b, explicitly
// out of this change's scope — it now has a landing site here, not a
// resolution).
func (*Provider) Capability() providers.Capability {
	return providers.Capability{
		Security: providers.RuleSet{
			Declared:   []string{"SEC-001", "SEC-010", "SEC-013", "SEC-040", "SEC-050", "SEC-052"},
			Enumerable: false,
		},
		Practices: providers.RuleSet{
			Declared:   []string{"PRAC-001", "PRAC-002", "PRAC-003", "PRAC-005"},
			Enumerable: false,
		},
		Surface:          []surface.Category{surface.CategoryAuthz},
		CoverageManifest: false,
	}
}
