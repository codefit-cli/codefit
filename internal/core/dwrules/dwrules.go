package dwrules

import (
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// Rule is one deterministic check over BOTH neutral inputs — the schema AND
// its paradigm/role classification. It is a DISTINCT family from
// dbrules.Rule (schema-only): a DW rule needs the paradigm fact, so it has
// its own runner. Like dbrules and crossrules, a rule emits affirmations
// (Findings, Confidence 1.0) and/or surface (SurfaceItems); it never sees a
// provider — both inputs are neutral (ADR 0029/0033).
type Rule interface {
	ID() string
	Check(*db.Schema, *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem)
}

// All is the enumerated DW rule set, instantiated by hand exactly like
// dbrules.All()/crossrules.All() — no registry (YAGNI). S2 (the star-schema +
// SCD family) and S3 (the columnar-index rule) fill it; DW-020
// (partitioning) remains declared-not-covered until S4 — its parser floor
// (db.Table.Partitioning) landed with partition-capture, but no rule reads
// that field yet, and a floor is not a check.
func All() []Rule {
	return []Rule{
		dw001{}, // S2 — fact table with no FK to any dimension (surface)
		dw002{}, // S2 — dimension without a surrogate key (surface)
		dw005{}, // S2 — facts present, no time dimension (surface)
		dw010{}, // S2 — SCD-2 dimension without a currency index (surface)
		dw011{}, // S2 — mixed SCD strategies in one schema (surface)
		dw021{}, // S3 — fact table with no columnar/analytic index (surface)
	}
}

// OwnedCategories are the baseline Item categories the DW rules produce — one
// per rule, in All() order. The DB sensor appends them to its own
// OwnedCategories() (design §2f): DW rules run INSIDE the sensor, so their
// categories are the SENSOR's, unlike crossrules, whose categories are unioned
// in the MCP adapter because the cross runs outside any sensor.
//
// A category that is emitted but NOT declared here can never be baselined or
// pruned (ADR 0019) — it is the one way to corrupt an existing baseline — so
// this list is test-locked against All() rather than maintained by memory.
func OwnedCategories() []string {
	return []string{
		string(surface.CategoryDWNoFactDimensionFK),       // DW-001
		string(surface.CategoryDWDimensionNoSurrogateKey), // DW-002
		string(surface.CategoryDWNoTimeDimension),         // DW-005
		string(surface.CategoryDWSCD2NoCurrencyIndex),     // DW-010
		string(surface.CategoryDWMixedSCDStrategies),      // DW-011
		string(surface.CategoryDWNoColumnarIndex),         // DW-021
	}
}

// Run executes the production rule set (All()) over the schema and its
// classification.
func Run(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	return RunWith(s, cls, All())
}

// RunWith executes an EXPLICIT rule set over the schema and classification —
// the injectable core of Run. Production calls Run (which passes All());
// the seam-gate test passes an empty/nil set so it proves the MERGE
// mechanism (that appending the runner's output leaves the caller's result
// byte-identical) independently of which rules All() happens to hold —
// otherwise the first real rule would put the seam gate red. A nil schema OR
// a nil classification yields nothing, mirroring dbrules.Run/
// crossrules.RunWith — unreachable via the production sensor today (it
// always builds a real Classification), but forward-safety for S2, where
// every real dwrules.Rule dereferences cls (SUGGESTION, S1 review ledger).
func RunWith(s *db.Schema, cls *paradigm.Classification, rules []Rule) ([]findings.Finding, []findings.SurfaceItem) {
	if s == nil || cls == nil {
		return nil, nil
	}
	var fs []findings.Finding
	var surf []findings.SurfaceItem
	for _, r := range rules {
		f, si := r.Check(s, cls)
		fs = append(fs, f...)
		surf = append(surf, si...)
	}
	return fs, surf
}
