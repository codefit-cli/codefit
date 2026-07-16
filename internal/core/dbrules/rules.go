package dbrules

import (
	"sort"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db050 — a table with no primary key. Structurally undeniable → an AFFIRMATION.
type db050 struct{}

func (db050) ID() string { return "DB-050" }

func (db050) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.Finding
	for _, t := range s.Tables {
		if len(t.PrimaryKey) > 0 {
			continue
		}
		out = append(out, findings.Finding{
			ID:            "DB-050",
			Dimension:     findings.DimensionDB,
			Severity:      findings.SeverityMedium,
			File:          t.Pos.File,
			Line:          t.Pos.Line,
			Title:         "Table without a primary key",
			Description:   "Table " + t.Name + " has no primary key.",
			Suggestion:    "Add a primary key so every row is uniquely addressable (required for reliable updates, replication, and indexing).",
			Confidence:    1.0,
			Probabilistic: false,
		})
	}
	return out, nil
}

// db001 — a foreign key with no covering index. Whether it matters depends on the
// table's size/access pattern → SURFACE (a question). "Covered" = some index-like
// column list has the FK columns as a LEADING PREFIX; the index-like set is every
// index PLUS the primary key treated as an implicit index (ADR 0015).
type db001 struct{}

func (db001) ID() string { return "DB-001" }

func (db001) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		coverers := indexLike(t)
		for _, fk := range t.ForeignKeys {
			if coveredByPrefix(coverers, fk.Columns) {
				continue
			}
			out = append(out, findings.SurfaceItem{
				Category: string(surface.CategoryDBFKNoIndex),
				File:     fk.Pos.File,
				Line:     fk.Pos.Line,
				StructuralSignals: []string{
					"fk_columns: " + strings.Join(fk.Columns, ", "),
					"existing_indexes: " + describeIndexLike(t),
				},
				StructuralFacts: map[string]bool{"covering_index_detected": false},
				ReasonToReview: "The foreign key (" + strings.Join(fk.Columns, ", ") + " -> " + fk.RefTable +
					") has no index whose leading columns cover it. Given this table's size and access pattern, " +
					"should it be indexed to avoid slow joins and unindexed foreign-key constraint checks?",
			})
		}
	}
	return nil, out
}

// indexLike returns every index-like leading-column list of a table: each index's
// columns, plus the primary key as an implicit index.
func indexLike(t db.Table) [][]string {
	out := make([][]string, 0, len(t.Indexes)+1)
	for _, ix := range t.Indexes {
		out = append(out, ix.Columns)
	}
	if len(t.PrimaryKey) > 0 {
		out = append(out, t.PrimaryKey)
	}
	return out
}

// coveredByPrefix reports whether some coverer has cols as a leading prefix (same
// order). A B-tree serves a lookup on the leading columns of an index, so an index
// [a,b] covers a FK on [a] and on [a,b], but not on [b] alone.
func coveredByPrefix(coverers [][]string, cols []string) bool {
	for _, c := range coverers {
		if hasLeadingPrefix(c, cols) {
			return true
		}
	}
	return false
}

func hasLeadingPrefix(index, prefix []string) bool {
	if len(prefix) == 0 || len(index) < len(prefix) {
		return false
	}
	for i := range prefix {
		if index[i] != prefix[i] {
			return false
		}
	}
	return true
}

func describeIndexLike(t db.Table) string {
	lists := indexLike(t)
	if len(lists) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(lists))
	for _, l := range lists {
		parts = append(parts, "["+strings.Join(l, ", ")+"]")
	}
	return strings.Join(parts, " ")
}

// db011 — an EXACT duplicate index (same columns in order, same uniqueness). Even
// an exact duplicate may be intentional (an in-flight migration), so which to drop
// is the human's call → SURFACE. Prefix-redundancy ([a] subsumed by [a,b]) is a
// DIFFERENT rule (db011prefix, db011prefix.go, CategoryDBPrefixRedundantIndex,
// Unit E / Phase 2.2) — the two never overlap, see db011prefix.go's doc comment.
// The reported item is deterministically the duplicate with the HIGHER Pos.Line
// (the earlier one is kept), independent of parser order.
type db011 struct{}

func (db011) ID() string { return "DB-011" }

func (db011) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		idxs := append([]db.Index(nil), t.Indexes...)
		sort.SliceStable(idxs, func(i, j int) bool { return idxs[i].Pos.Line < idxs[j].Pos.Line })
		type key struct {
			cols   string
			unique bool
		}
		seen := map[key]bool{}
		for _, ix := range idxs {
			k := key{strings.Join(ix.Columns, "\x00"), ix.Unique}
			if seen[k] {
				out = append(out, findings.SurfaceItem{
					Category: string(surface.CategoryDBDupIndex),
					File:     ix.Pos.File,
					Line:     ix.Pos.Line,
					StructuralSignals: []string{
						"index_columns: " + strings.Join(ix.Columns, ", "),
						"unique: " + boolStr(ix.Unique),
					},
					StructuralFacts: map[string]bool{"exact_duplicate_index": true},
					ReasonToReview: "This index duplicates another index on the same columns [" + strings.Join(ix.Columns, ", ") +
						"] with the same uniqueness. A duplicate index only costs writes and storage — is one of them redundant?",
				})
				continue
			}
			seen[k] = true
		}
	}
	return nil, out
}

// db002 — a multivalued (array) column violates 1NF, but a native array (e.g.
// Postgres) is legitimate sometimes → SURFACE.
type db002 struct{}

func (db002) ID() string { return "DB-002" }

func (db002) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		for _, c := range t.Columns {
			if !c.List {
				continue
			}
			out = append(out, findings.SurfaceItem{
				Category: string(surface.CategoryDBMultivalued),
				File:     c.Pos.File,
				Line:     c.Pos.Line,
				StructuralSignals: []string{
					"column: " + c.Name,
					"type: " + c.RawType + "[]",
				},
				StructuralFacts: map[string]bool{"multivalued_column": true},
				ReasonToReview: "Column " + c.Name + " is multivalued (an array). Is a normalized relation more " +
					"appropriate here (1NF), or is a native array intentional for this data?",
			})
		}
	}
	return nil, out
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
