package crossrules

import (
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/query"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// Rule is one deterministic check over BOTH neutral inputs — the schema AND the
// code's query filters. It is a DISTINCT family from dbrules.Rule (schema-only):
// the cross needs two arguments, so it has its own runner. Like dbrules, a rule
// emits affirmations (Findings, Confidence 1.0) and/or surface (SurfaceItems); it
// never sees a provider — both inputs are neutral (ADR 0029).
type Rule interface {
	ID() string
	Check(*db.Schema, []query.QueryFilter) ([]findings.Finding, []findings.SurfaceItem)
}

// All is the enumerated cross-rule set: DB-010 (single-column filter without a
// covering index, ADR 0030) and DB-013 (multi-column filter without a covering
// composite index, ADR 0031). The two partition every reconciled filter by its
// column count — DB-010 the single-column ones, DB-013 the multi-column ones — so
// they never double-report one filter. Both reuse reconcile and the shared coverage
// of core/db.
func All() []Rule { return []Rule{db010{}, db013{}} }

// OwnedCategories are the baseline Item categories the cross rules produce. The
// adapter unions them into the db-dimension baseline scope (ADR 0019/0029) so the
// cross's surface items are tracked and pruned like every other db item — they are
// produced by the cross, not the schema-only db sensor, so they are declared here.
func OwnedCategories() []string {
	return []string{
		string(surface.CategoryDBFilteredColumnNoIndex),
		string(surface.CategoryDBNoCompositeIndex),
	}
}

// Run executes the production rule set (All()) over the schema and query filters.
// A nil schema (no database) yields nothing; nil filters (the provider has no
// QueryExtractor, or nothing filters a column) means every rule sees an empty code
// side.
func Run(s *db.Schema, filters []query.QueryFilter) ([]findings.Finding, []findings.SurfaceItem) {
	return RunWith(s, filters, All())
}

// RunWith executes an EXPLICIT rule set over the schema and query filters — the
// injectable core of Run. Production calls Run (which passes All()); the seam gate
// passes an empty set so it proves the MERGE mechanism (that appending the runner's
// output leaves the db result byte-identical) independently of which rules All()
// happens to hold — otherwise the first real rule would put the seam gate red.
func RunWith(s *db.Schema, filters []query.QueryFilter, rules []Rule) ([]findings.Finding, []findings.SurfaceItem) {
	if s == nil {
		return nil, nil
	}
	var fs []findings.Finding
	var surf []findings.SurfaceItem
	for _, r := range rules {
		f, si := r.Check(s, filters)
		fs = append(fs, f...)
		surf = append(surf, si...)
	}
	return fs, surf
}

// reconcile joins one QueryFilter to the schema by LOGICAL name — the certainty
// gate at the heart of the cross. It resolves ONLY when unambiguous and abstains
// otherwise, the Complete==false discipline (ADR 0025, ADR 0029) applied to the
// code↔schema join: a mutilated cross is worse than an absent one.
//
// Resolution, in order:
//  1. Table: EXACTLY one db.Table whose Name equals f.Model. Zero or more than one
//     candidate → abstain (ok=false). The match is exact identity, NEVER fuzzy or
//     case-insensitive: all naming-convention knowledge (the Prisma accessor
//     "user" → model "User") lives in the provider, which already normalized
//     f.Model; if that normalization missed, an abstain is the honest outcome, not
//     a forced match.
//  2. Columns: each f.Columns entry must name a real db.Column of the resolved
//     table. Existing ones are kept (in f's order); a name that is not a column — a
//     relation field, an OR/NOT artifact, a typo — is dropped, since only the
//     schema knows it is not a column. If NONE resolve → abstain.
//
// On success it returns the resolved table and the validated columns. On abstain it
// returns (nil, nil, false). reconcile is the shared helper every cross-rule uses;
// it is exercised directly by the tests in this slice (the rule set is still
// empty).
func reconcile(s *db.Schema, f query.QueryFilter) (*db.Table, []string, bool) {
	var table *db.Table
	for i := range s.Tables {
		if s.Tables[i].Name == f.Model {
			if table != nil {
				return nil, nil, false // ambiguous: more than one table by this name
			}
			table = &s.Tables[i]
		}
	}
	if table == nil {
		return nil, nil, false // no table by this name (includes an empty Model)
	}
	cols := make(map[string]bool, len(table.Columns))
	for _, c := range table.Columns {
		cols[c.Name] = true
	}
	var valid []string
	for _, c := range f.Columns {
		if cols[c] {
			valid = append(valid, c)
		}
	}
	if len(valid) == 0 {
		return nil, nil, false // no filtered column maps to a real column
	}
	return table, valid, true
}
