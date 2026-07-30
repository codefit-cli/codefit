package dwrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dw010 — a slowly-changing (SCD-2) dimension with no index serving its
// CURRENCY lookup. An SCD-2 dimension keeps every historical version of a row;
// the overwhelmingly common query is "give me the CURRENT version", expressed
// as WHERE is_current = true or WHERE valid_to IS NULL. With no index leading
// on either column, that query scans the entire version history — and the
// history is precisely what grows without bound.
//
// SURFACE, never an affirmation (ADR 0017): whether the scan matters depends
// on the dimension's row count and how it is joined, the same judgment DB-001
// (FK without index) already defers to the agent.
//
// SCOPE — the rule evaluates a dimension only when it carries at least one
// slowly-changing column, so an ordinary type-1 dimension is never asked for
// an index it has no use for. The vocabulary is deliberately small and
// conventional: valid_from, valid_to, is_current, effective_date, matched with
// separators stripped so validTo / IsCurrent (how a Prisma model spells them)
// are recognized too.
//
// COVERAGE is delegated to db.IndexLike + db.CoveredByOrderedPrefix — the same
// shared helpers DB-001 and DB-010 use — so "what serves a lookup" is defined
// ONCE in the neutral model and cannot drift per rule. That brings two
// behaviors along for free, both test-locked: the primary key counts as an
// implicit index, and a composite index NOT leading on the currency column
// (e.g. [customer_code, is_current]) does NOT cover it.
//
// EITHER currency column being covered is enough to go quiet: one index can
// answer "which row is current", and demanding both would be noise. When the
// dimension has history columns but NEITHER currency column, the rule abstains
// rather than inventing a demand — there is no currency lookup to serve.
//
// DECLARED LIMIT: an SCD-2 dimension using a different vocabulary is not
// recognized. AdventureWorksDW's own DimProduct uses StartDate/EndDate/Status,
// which this rule does not match — widening the vocabulary would start
// colliding with ordinary validity ranges on non-SCD tables, so it stays
// conventional and the gap is stated instead.
type dw010 struct{}

func (dw010) ID() string { return "DW-010" }

// scdColumnNames are the conventional slowly-changing-dimension markers, in
// canonical (separator-stripped, lowercased) form.
var scdColumnNames = map[string]string{
	"validfrom":     "valid_from",
	"validto":       "valid_to",
	"iscurrent":     "is_current",
	"effectivedate": "effective_date",
}

// currencyColumnNames are the subset of SCD markers a "which row is current"
// lookup filters on — the columns an index must lead with.
var currencyColumnNames = map[string]bool{"validto": true, "iscurrent": true}

func (dw010) Check(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		if cls.Roles[t.Name] != paradigm.RoleDimension {
			continue
		}
		scd, currency := scdColumns(t)
		if len(scd) == 0 || len(currency) == 0 {
			// Not an SCD-2 dimension, or no currency lookup to serve.
			continue
		}
		coverers := db.IndexLike(t)
		if anyCovered(coverers, currency) {
			continue
		}
		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDWSCD2NoCurrencyIndex),
			File:     t.Pos.File,
			Line:     t.Pos.Line,
			StructuralSignals: []string{
				"table: " + t.Name,
				"scd_columns: " + strings.Join(scd, ", "),
				"currency_columns: " + strings.Join(currency, ", "),
				"existing_indexes: " + describeIndexLike(coverers),
			},
			StructuralFacts: map[string]bool{
				"currency_index_detected": false,
				"has_any_index_like":      len(coverers) > 0,
			},
			ReasonToReview: "Dimension " + t.Name + " keeps row history (" + strings.Join(scd, ", ") +
				") but no index leads with " + strings.Join(currency, " or ") + ". Every \"current version\" " +
				"lookup then scans the whole history, which is the part that grows without bound. Given this " +
				"dimension's size and how it is joined, should the currency column be indexed?",
		})
	}
	return nil, out
}

// scdColumns returns a table's slowly-changing marker columns and, of those,
// the currency columns — both in the table's own declared column order, using
// the column's REAL name (not the canonical form) so the signal points at what
// the schema actually wrote.
func scdColumns(t db.Table) (scd, currency []string) {
	for _, c := range t.Columns {
		key := normalizeDWIdent(c.Name)
		if _, ok := scdColumnNames[key]; !ok {
			continue
		}
		scd = append(scd, c.Name)
		if currencyColumnNames[key] {
			currency = append(currency, c.Name)
		}
	}
	return scd, currency
}

// anyCovered reports whether some index-like list leads with ANY of cols. One
// index is enough to serve the currency lookup.
func anyCovered(coverers [][]string, cols []string) bool {
	for _, c := range cols {
		if db.CoveredByOrderedPrefix(coverers, []string{c}) {
			return true
		}
	}
	return false
}

// describeIndexLike renders index-like column lists for a signal, stating the
// empty case explicitly. It mirrors dbrules' helper of the same name; the two
// packages do not share unexported helpers, and duplicating four lines of
// FORMATTING is preferable to exporting a presentation detail from the neutral
// model — the COVERAGE semantics, which is what must never drift, are already
// shared via db.IndexLike/db.CoveredByOrderedPrefix.
func describeIndexLike(lists [][]string) string {
	if len(lists) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(lists))
	for _, l := range lists {
		parts = append(parts, "["+strings.Join(l, ", ")+"]")
	}
	return strings.Join(parts, " ")
}
