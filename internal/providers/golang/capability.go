package golang

import (
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Capability declares what the Go provider implements, measured against
// security.go/practices.go/surface.go at main@ee0b263: 6 security rule IDs, 4
// practices rule IDs, and exactly ONE surface category (authz — HTTP
// handlers, surface.go). Both RuleSets are Enumerable:false: Go's detectors
// have no All()/ID() rule-registry loader (unlike TypeScript's YAML-backed
// security rules), so Declared is a hand-maintained mirror of the switch in
// security.go/practices.go, not a derived list — Control A
// (internal/providers/typescript's Enumerable:true declaration) does not
// apply here. CoverageManifest is false: the Go provider implements no
// CoverageManifest() method today (P1-4b's coverage-manifest half stays
// open; R1, ADR 0065, makes codefit-coverage derive an honest answer for
// go without one).
//
// Practices.Excluded carries PRAC-004 — permanently dropped per ADR 0056,
// not merely unimplemented: it fired on every `go` statement claiming "no
// visible WaitGroup or channel" with zero synchronization detection,
// because go/ast without go/types cannot see synchronization living in a
// callee, a struct field, or an errgroup, and the practices dimension is
// deterministic by spec (no surface channel to route an uncertain signal
// into). This is P1-4b's owed manifest entry: the drop was recorded only
// in the ADR and the CHANGELOG until now, so an agent asking what codefit
// covers for Go was left to infer the hole from PRAC-004's absence.
func (*Provider) Capability() providers.Capability {
	return providers.Capability{
		Security: providers.RuleSet{
			Declared:   []string{"SEC-001", "SEC-010", "SEC-013", "SEC-040", "SEC-050", "SEC-052"},
			Enumerable: false,
		},
		Practices: providers.RuleSet{
			Declared:   []string{"PRAC-001", "PRAC-002", "PRAC-003", "PRAC-005"},
			Enumerable: false,
			Excluded: []providers.ExcludedRule{
				{
					ID: "PRAC-004",
					Reason: "permanently dropped (ADR 0056): fired on every `go` statement " +
						"claiming \"no visible WaitGroup or channel\" with no synchronization " +
						"detection at all — synchronization can live in the callee, a struct " +
						"field, or an errgroup, none of which go/ast without go/types can see, " +
						"and the practices dimension is deterministic by spec, with no surface " +
						"channel to route an uncertain signal into",
				},
			},
		},
		Surface:          []surface.Category{surface.CategoryAuthz},
		CoverageManifest: false,
	}
}
