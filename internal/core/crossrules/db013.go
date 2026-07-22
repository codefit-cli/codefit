package crossrules

import (
	"sort"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/query"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db013 — a MULTI-column filter (a AND b) that no composite index covers. It is the
// multi-column sibling of DB-010 in the code↔schema cross (ADR 0031): it handles
// exactly the filters DB-010 defers — those that reconcile to two or more real
// columns — so the two rules partition every filter with no overlap and no
// cross-rule suppression. It reuses reconcile (the exact-match certainty gate) and
// the shared coverage of core/db, so its notion of "covered" never drifts.
//
// Coverage is ORDER-INSENSITIVE (db.CoveredBySetPrefix): a WHERE a=? AND b=? is
// served by a composite index on (a,b) OR (b,a) — both have {a,b} as their leading
// set. This differs from DB-010/DB-001's ordered CoveredByOrderedPrefix: an equality set
// does not respect declared order. codefit does not capture equality-vs-range (a
// declared limit), so it takes the filtered columns as an unordered set and leaves
// the column-order/range judgment to the agent.
//
// SURFACE, never a deterministic finding (ADR 0030/0031): the structural fact (a
// multi-column filter with no covering composite index) is certain once reconcile
// matches, but whether the composite index matters is the agent's judgment. Emits
// nothing when reconcile abstains (Complete==false floor). The item anchors to the
// SCHEMA table (the fix, @@index([a,b]), is a table-level change); the filtered
// columns are named in the signals. Deduplicated by (table, column SET).
type db013 struct{}

func (db013) ID() string { return "DB-013" }

func (db013) Check(s *db.Schema, filters []query.QueryFilter) ([]findings.Finding, []findings.SurfaceItem) {
	seen := map[string]bool{}
	var out []findings.SurfaceItem

	for _, f := range filters {
		table, cols, ok := reconcile(s, f)
		if !ok {
			continue // abstain — inexact match (Complete==false floor)
		}
		// DB-013 owns MULTI-column filters only; a single real column is DB-010's
		// (precedence, ADR 0031). Routing by the reconciled column count partitions
		// the filters between the two rules with no overlap.
		if len(cols) < 2 {
			continue
		}
		if db.CoveredBySetPrefix(db.IndexLike(*table), cols) {
			continue // a composite index already has this column set as its leading columns
		}
		// Dedup by (table, column SET): (a,b) and (b,a) are one concern — one
		// @@index fixes both. The sorted set is also the stable signal order.
		set := append([]string(nil), cols...)
		sort.Strings(set)
		key := table.Name + "\x00" + strings.Join(set, ",")
		if seen[key] {
			continue
		}
		seen[key] = true

		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDBNoCompositeIndex),
			File:     table.Pos.File,
			Line:     table.Pos.Line,
			StructuralSignals: []string{
				"model: " + table.Name,
				"filtered_columns: " + strings.Join(set, ", "),
				"existing_indexes: " + describeIndexLike(*table),
			},
			StructuralFacts: map[string]bool{"covering_composite_index_detected": false},
			ReasonToReview: "Code filters " + table.Name + " by " + strings.Join(set, " and ") +
				" together, but no composite index has these columns as its leading columns. Given this " +
				"table's size and access pattern, should a composite index (e.g. @@index([" + strings.Join(set, ", ") +
				"])) be added? Column order in the index matters for range filters — codefit reports the set, " +
				"you judge the order.",
		})
	}
	return nil, out
}
