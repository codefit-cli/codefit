package dbrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db011prefix — the PREFIX-REDUNDANT half of DB-011 ("índices duplicados/
// redundantes", docs/PRD-codefit-v1.4.md:371): an index [a] whose columns
// are a STRICT leading prefix of another index-like column list [a,b] on
// the same table — the composite already serves any lookup [a] alone would
// (a B-tree serves a query on its leading columns), so the narrower index
// is pure write/storage overhead. Closes the gap declared at
// internal/providers/typescript/coverage.go:34 ("Prefix-redundant indexes
// ([a] subsumed by [a,b]) are NOT yet detected — a known gap for a later
// slice.").
//
// The PRD assigns ONE id, DB-011, to both the exact-duplicate case (db011,
// rules.go) and this prefix-redundant case — they are the same numbered
// rule in the product's own vocabulary. This type is nonetheless its OWN
// Go struct / SurfaceItem category / Check(), sharing NO logic with db011's
// exact-match path, so the two mechanisms structurally CANNOT double-report
// the same index pair:
//   - db011 (CategoryDBDupIndex) fires ONLY on an EXACT column-list match
//     (same length, same order, same uniqueness).
//   - db011prefix (CategoryDBPrefixRedundantIndex) fires ONLY when one
//     index's columns are a STRICTLY SHORTER leading prefix of another's.
//
// Same length can never also be a strict prefix (isStrictPrefix requires
// long > short) — the two conditions partition disjoint cases by
// construction, not by a runtime exclusion check. Locked by
// TestDB011Prefix_ExactDuplicateNeverOverlapsDB011 and
// TestDB011Prefix_PrefixPairNeverFiresExactDuplicateCategory.
//
// SURFACE, never an affirmation (ADR 0015) — same rationale as db011's
// exact-duplicate case: which index to drop is the human's call, and an
// index that looks redundant today may be an in-flight migration.
//
// A UNIQUE index is NEVER subsumed, even by a wider non-unique composite: a
// UNIQUE [a] enforces a uniqueness CONSTRAINT that a composite [a,b] does
// NOT guarantee for [a] alone (two rows could share the same "a" as long as
// their "b" differs) — reporting it redundant would be a wrong affirmation
// of "safe to drop". The structural fact subsumed_index_is_unique is still
// emitted (always false, since a unique index never reaches emission) so a
// reader of an item sees the guard was checked, not merely silently absent.
//
// The primary key counts as an implicit index-like coverer, consistent with
// db001's existing treatment (rules.go's indexLike) — an index [a] under a
// PK of [a,b] is exactly as redundant as under a real composite index
// [a,b]. The structural fact subsumed_by_primary_key distinguishes the two
// sources.
type db011prefix struct{}

func (db011prefix) ID() string { return "DB-011" }

func (db011prefix) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		for _, ix := range t.Indexes {
			if ix.Unique {
				continue // a UNIQUE index is never subsumed — see type doc
			}
			subsumingCols, byPK, ok := subsumingCoverer(t, ix)
			if !ok {
				continue
			}
			out = append(out, findings.SurfaceItem{
				Category: string(surface.CategoryDBPrefixRedundantIndex),
				File:     ix.Pos.File,
				Line:     ix.Pos.Line,
				StructuralSignals: []string{
					"index_columns: " + strings.Join(ix.Columns, ", "),
					"subsumed_by_columns: " + strings.Join(subsumingCols, ", "),
				},
				StructuralFacts: map[string]bool{
					"subsumed_by_primary_key":  byPK,
					"subsumed_index_is_unique": ix.Unique,
				},
				ReasonToReview: "Index [" + strings.Join(ix.Columns, ", ") + "] is a strict leading prefix of [" +
					strings.Join(subsumingCols, ", ") + "], which already serves any lookup this index would. " +
					"Is this narrower index still needed, or is it redundant?",
			})
		}
	}
	return nil, out
}

// subsumingCoverer reports whether some other index-like coverer of t (a
// real index, or the primary key treated as an implicit index) has
// ix.Columns as a STRICT (strictly shorter) leading prefix, and whether
// that coverer is the primary key. Real indexes are checked before the
// primary key, mirroring indexLike's own append order (rules.go) —
// deterministic when a column list happens to be subsumed by more than one
// coverer. ix itself can never match against itself: isStrictPrefix
// requires the coverer to be STRICTLY LONGER than ix.Columns, and ix always
// has the same length as itself.
func subsumingCoverer(t db.Table, ix db.Index) (cols []string, byPK bool, ok bool) {
	for _, other := range t.Indexes {
		if isStrictPrefix(ix.Columns, other.Columns) {
			return other.Columns, false, true
		}
	}
	if isStrictPrefix(ix.Columns, t.PrimaryKey) {
		return t.PrimaryKey, true, true
	}
	return nil, false, false
}

// isStrictPrefix reports whether short is a non-empty STRICT (strictly
// shorter) leading prefix of long — same order, same columns, long has at
// least one more column. An EXACT match (same length) is db011's exact-
// duplicate domain, never this rule's — the length check alone makes the
// two mutually exclusive.
func isStrictPrefix(short, long []string) bool {
	if len(short) == 0 || len(long) <= len(short) {
		return false
	}
	for i := range short {
		if short[i] != long[i] {
			return false
		}
	}
	return true
}
