package dbrules

import (
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
)

// Rule is one deterministic DB-structure check over the neutral schema. It emits
// affirmations (Findings, Confidence 1.0) and/or surface (SurfaceItems, questions
// the agent reasons). A rule never sees a concrete ORM — only db.Schema — so it is
// inherited by every future provider that can produce one (ADR 0015).
type Rule interface {
	ID() string
	Check(*db.Schema) ([]findings.Finding, []findings.SurfaceItem)
}

// All is the enumerated rule set of this slice (schema-only OLTP). Instantiated by
// hand, like the sensors — no registry (YAGNI until there are many rules).
func All() []Rule {
	return []Rule{
		db050{}, db001{}, db011{}, db002{}, // slice 2 — structural
		db051{}, db052{}, db053{}, db003{}, // slice 2b — name heuristics (surface)
		db020{},       // Phase 2.2 — view sensitive-column exposure (surface)
		db011prefix{}, // Phase 2.2 (Unit E) — prefix-redundant index (surface)
		db031{},       // 0.2.3 — routine without exception handling (surface, body-derived)
		db040{},       // 0.2.3 — trigger cross-table cascade (surface, body-derived)
		db041{},       // 0.2.3 — trigger external-effecting call (surface, body-derived)
		db030{},       // 0.2.3 — dynamic SQL construction in a routine (surface, body-derived)
	}
}

// Run executes every rule over the schema and returns the union of their findings
// and surface. A nil schema yields nothing.
func Run(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	if s == nil {
		return nil, nil
	}
	var fs []findings.Finding
	var surf []findings.SurfaceItem
	for _, r := range All() {
		f, si := r.Check(s)
		fs = append(fs, f...)
		surf = append(surf, si...)
	}
	return fs, surf
}
