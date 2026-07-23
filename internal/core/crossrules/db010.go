package crossrules

import (
	"sort"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/query"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db010 — a column the CODE filters by that the SCHEMA does not index. It is the
// first rule of the code↔schema cross (ADR 0029/0030): unlike a schema-only DB
// rule, establishing it needs BOTH inputs — the query filters (code) and the
// schema. It reuses reconcile (the exact-match certainty gate) and the shared
// leftmost-prefix coverage of core/db (db.IndexLike / db.CoveredByOrderedPrefix), so its
// notion of "covered" is identical to DB-001's and never drifts (ADR 0015).
//
// It emits SURFACE, never a deterministic finding (ADR 0030): the structural fact
// (filtered-and-uncovered) is certain once reconcile matches exactly, but whether a
// missing index MATTERS depends on the column's cardinality, the table's size and
// the write load — the same agent judgment DB-001 (FK without index) already defers
// (ADR 0004/0005). When reconcile abstains (inexact match) the rule emits nothing —
// the Complete==false floor.
//
// DB-010 is the SINGLE-column rule: it handles only filters that reconcile to
// exactly ONE real column, checking it against the leftmost-prefix coverage (a
// composite index [a,b] covers a lookup on a, not on b). A multi-column WHERE
// (a AND b) is DEFERRED WHOLE to DB-013 (composite index), never split into
// per-column concerns — a multi-column filter wants a composite index, not a
// standalone index per column (precedence, ADR 0031). The item anchors to the
// SCHEMA column (file:line in the schema), consistent with DB-001 and the whole DB
// dimension; the filtering query is context, the fix is a schema change.
type db010 struct{}

func (db010) ID() string { return "DB-010" }

func (db010) Check(s *db.Schema, filters []query.QueryFilter) ([]findings.Finding, []findings.SurfaceItem) {
	// Dedup by (table, column) across ALL filters: one item per uncovered column,
	// not one per query. Two queries filtering the same unindexed column are one
	// concern (add one index), reported once.
	type key struct{ table, col string }
	seen := map[key]bool{}
	var out []findings.SurfaceItem

	for _, f := range filters {
		table, cols, ok := reconcile(s, f)
		if !ok {
			continue // abstain — inexact match, emit nothing (Complete==false floor)
		}
		// Precedence with DB-013 (ADR 0031): DB-010 owns SINGLE-column filters only.
		// A multi-column WHERE (a AND b) wants a COMPOSITE index (@@index([a,b]),
		// DB-013's recommendation), NOT a standalone index per column — so it is left
		// whole to DB-013. Routing by the RECONCILED column count (real columns, after
		// relations/phantoms are dropped) partitions every filter between the two
		// rules with no overlap and no cross-rule suppression: neither rule inspects
		// the other's output. A column the code ALSO filters on its own arrives as its
		// own single-column filter and is caught here.
		if len(cols) != 1 {
			continue
		}
		c := cols[0]
		// FIX unique-subset (ADR 0032): a filter that constrains a unique key resolves
		// to ≤1 row → no index missing. For a single column this coincides with the
		// ordered-prefix check below (a unique/PK on c leads c), but it is wired here
		// too so DB-010 and DB-013 share one coverage floor.
		if db.CoveredByUniqueSubset(db.UniqueKeys(*table), cols) {
			continue
		}
		coverers := db.IndexLike(*table)
		if db.CoveredByOrderedPrefix(coverers, []string{c}) {
			continue // covered by an index / unique / PK leading column
		}
		col := columnByName(table, c)
		// Low-cardinality by TYPE, derivable from the schema, not the data (ADR 0032):
		// a Boolean is 2 values; an enum is a declared, bounded set. Indexing such a
		// column standalone is almost always wrong, and codefit knows it from the
		// .prisma without seeing a row. Skip — a partial-index / distribution judgment
		// is the agent's, and refining an enum by value COUNT would need the neutral
		// model to carry enum values (a separate slice).
		if isLowCardinalityType(col.Type) {
			continue
		}
		k := key{table.Name, c}
		if seen[k] {
			continue
		}
		seen[k] = true

		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDBFilteredColumnNoIndex),
			File:     col.Pos.File,
			Line:     col.Pos.Line,
			StructuralSignals: []string{
				"model: " + table.Name,
				"filtered_column: " + c,
				"existing_indexes: " + describeIndexLike(*table),
			},
			StructuralFacts: map[string]bool{"covering_index_detected": false},
			ReasonToReview: "Code filters " + table.Name + " by " + c + ", but no index (or unique " +
				"constraint or primary key) covers it as a leading column. Given this table's size and " +
				"access pattern, should " + c + " be indexed to avoid a sequential scan per query?",
		})
	}
	return nil, out
}

// isLowCardinalityType reports whether a column's declared type is bounded by
// construction — a Boolean (2 values) or an enum (a declared, finite value set).
// Filtering by such a column rarely warrants a standalone index; DB-010 skips it
// (FIX 2, ADR 0032). It is used ONLY by DB-010 (single column): a Boolean/enum as
// PART of a composite filter is legitimate, so DB-013 does not apply this.
func isLowCardinalityType(t db.Type) bool {
	return t == db.TypeBool || t == db.TypeEnum
}

// columnByName returns the column of t with the given Name (for its schema Pos).
// The name is always present — reconcile validated it against t's columns — but a
// zero Column is returned if not, so the item still anchors to the schema file.
func columnByName(t *db.Table, name string) db.Column {
	for _, c := range t.Columns {
		if c.Name == name {
			return c
		}
	}
	return db.Column{Pos: t.Pos}
}

// describeIndexLike renders a table's index-like column lists for the signal, in a
// stable order — the same "[a] [a, b]" shape DB-001's signal uses.
func describeIndexLike(t db.Table) string {
	lists := db.IndexLike(t)
	if len(lists) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(lists))
	for _, l := range lists {
		parts = append(parts, "["+strings.Join(l, ", ")+"]")
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}
