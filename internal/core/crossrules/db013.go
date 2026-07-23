package crossrules

import (
	"sort"
	"strconv"
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

// setGroup accumulates the models that share one uncovered column set — the FIX 4
// aggregation: (salonId, deletedAt) appearing across 15 models is ONE architectural
// observation, not 15 findings.
type setGroup struct {
	set    []string    // sorted column set
	models []*db.Table // models exhibiting it, deduped, later sorted by name
}

func (db013) Check(s *db.Schema, filters []query.QueryFilter) ([]findings.Finding, []findings.SurfaceItem) {
	groups := map[string]*setGroup{}
	seenModelSet := map[string]bool{} // dedup a (model, set) pair
	var order []string                // first-seen order of set keys, re-sorted at the end

	for _, f := range filters {
		table, cols, ok := reconcile(s, f)
		if !ok {
			continue // abstain — inexact match (Complete==false floor)
		}
		// DB-013 owns MULTI-column filters only; a single real column is DB-010's
		// (precedence, ADR 0031).
		if len(cols) < 2 {
			continue
		}
		// FIX high-arity abstention (ADR 0032): with 4+ filtered columns, which
		// subset to index depends on selectivity codefit cannot see → it does not
		// know, so it stays silent. A 4-column @@index is a guess with an item's
		// format, not an actionable suggestion — the same floor as cross-naming-space
		// and the range limit.
		if len(cols) >= arityAbstainThreshold {
			continue
		}
		// FIX unique-subset (ADR 0032): a filter constraining a unique key (the PK, a
		// @unique, or a @@unique subset) resolves to ≤1 row → no composite index
		// helps. Kills the (id, salonId) tenant-scoped-lookup false positive.
		if db.CoveredByUniqueSubset(db.UniqueKeys(*table), cols) {
			continue
		}
		if db.CoveredBySetPrefix(db.IndexLike(*table), cols) {
			continue // a composite index already has this column set as its leading columns
		}

		set := append([]string(nil), cols...)
		sort.Strings(set)
		key := strings.Join(set, ",")
		if seenModelSet[table.Name+"\x00"+key] {
			continue // (a,b) and (b,a) on the same model are one concern
		}
		seenModelSet[table.Name+"\x00"+key] = true

		g := groups[key]
		if g == nil {
			g = &setGroup{set: set}
			groups[key] = g
			order = append(order, key)
		}
		g.models = append(g.models, table)
	}

	sort.Strings(order) // stable output, independent of filter order
	var out []findings.SurfaceItem
	for _, key := range order {
		g := groups[key]
		sort.Slice(g.models, func(i, j int) bool { return g.models[i].Name < g.models[j].Name })
		anchor := g.models[0] // a clickable starting point; the concern spans all models
		names := make([]string, len(g.models))
		for i, m := range g.models {
			names[i] = m.Name
		}
		setStr := strings.Join(g.set, ", ")

		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDBNoCompositeIndex),
			File:     anchor.Pos.File,
			Line:     anchor.Pos.Line,
			// Snippet is set EXPLICITLY to the column set (not a source line): the
			// grouped item spans models, and a per-set snippet makes the baseline
			// fingerprint distinct-per-set and stable regardless of which model anchors
			// it (FIX 4, ADR 0032). It also fixes a latent collision where two sets on
			// one table shared the model line's snippet.
			Snippet: setStr,
			StructuralSignals: []string{
				"filtered_columns: " + setStr,
				"affected_models: " + strings.Join(names, ", "),
			},
			StructuralFacts: map[string]bool{"covering_composite_index_detected": false},
			ReasonToReview:  db013Reason(setStr, names),
		})
	}
	return nil, out
}

// arityAbstainThreshold is the filtered-column count at or above which DB-013
// abstains (FIX 3, ADR 0032): it emits only for 2- or 3-column filters. Real
// composite indexes rarely exceed 3 columns; beyond that the useful subset depends
// on selectivity codefit cannot observe.
const arityAbstainThreshold = 4

// db013Reason phrases the grouped concern, naming the recurrence when it spans more
// than one model (an architectural pattern, not N separate findings).
func db013Reason(setStr string, models []string) string {
	base := "Code filters by " + setStr + " together, but no composite index has these columns as " +
		"its leading columns. Given the table's size and access pattern, should a composite index " +
		"(e.g. @@index([" + setStr + "])) be added? Column order in the index matters for range filters — " +
		"codefit reports the set, you judge the order."
	if len(models) > 1 {
		base = "This column set is filtered-and-uncovered across " + itoa(len(models)) + " models (" +
			strings.Join(models, ", ") + ") — likely one architectural pattern (e.g. tenant scoping + " +
			"soft-delete). " + base
	} else {
		base = "Model " + models[0] + ": " + base
	}
	return base
}

func itoa(n int) string { return strconv.Itoa(n) }
