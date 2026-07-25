package dwrules

import (
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
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
// SCD family) fills it; DW-020 (partitioning) and DW-021 (columnar index)
// remain declared-not-covered until S4/S3.
func All() []Rule {
	return []Rule{
		dw001{}, // S2 — fact table with no FK to any dimension (surface)
		dw002{}, // S2 — dimension without a surrogate key (surface)
	}
}

// OwnedCategories are the baseline Item categories the DW rules produce.
// Empty in S1 — no DW rule owns a surface category yet; the sensor's own
// OwnedCategories() is therefore unchanged this slice (design §2f).
func OwnedCategories() []string { return nil }

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
