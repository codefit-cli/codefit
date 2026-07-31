package dbrules

import (
	"sort"
	"strconv"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db050 — a table with no primary key. Structurally undeniable → an AFFIRMATION.
type db050 struct{}

func (db050) ID() string { return "DB-050" }

func (db050) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var findingsOut []findings.Finding
	var surfaceOut []findings.SurfaceItem
	for _, t := range s.Tables {
		if len(t.PrimaryKey) > 0 {
			continue
		}
		// D4/D5 (design SS4/SS5): DB-050 is the ONE absence-based rule that
		// AFFIRMS, so on an unproven table it cannot simply abstain — that
		// would trade a false positive for total loss of the dimension's
		// single deterministic signal. It ROUTES to a dedicated surface item
		// instead, carrying the raw unreduced statement(s) so the agent can
		// read the DDL itself. Every other absence-based rule abstains
		// silently (see the ABSTAIN rules below and in dwrules).
		if !t.StructureProven() {
			surfaceOut = append(surfaceOut, routeUnprovenTable(t))
			continue
		}
		findingsOut = append(findingsOut, findings.Finding{
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
	return findingsOut, surfaceOut
}

// routeUnprovenTableStatementCap and routeUnprovenTableStatementMaxLen bound
// routeUnprovenTable's payload (F6, 4R ledger obs #1282): every sibling
// carrier in this change caps its output (sensors/db's inventory: 5 tables /
// 3 reasons per note) — this one did not, so a pathological migration
// (hundreds of dropped ALTERs on one table, or one absurdly long statement)
// could balloon a single surface item unboundedly.
const (
	routeUnprovenTableStatementCap    = 5
	routeUnprovenTableStatementMaxLen = 500
)

// routeUnprovenTable builds DB-050's routed surface item for a table whose
// structure could not be proven complete (design SS5): "this table has no
// primary key" and "codefit cannot tell whether this table has a primary
// key" are different claims, so this is its OWN category, never a reuse.
func routeUnprovenTable(t db.Table) findings.SurfaceItem {
	signals := []string{"table: " + t.Name}
	shown := t.Unreduced
	if len(shown) > routeUnprovenTableStatementCap {
		shown = shown[:routeUnprovenTableStatementCap]
	}
	for _, u := range shown {
		signals = append(signals,
			"unreduced_statement: "+truncateStatement(u.Text, routeUnprovenTableStatementMaxLen),
			"unreduced_at: "+u.Pos.File+":"+strconv.Itoa(u.Pos.Line),
		)
	}
	if omitted := len(t.Unreduced) - len(shown); omitted > 0 {
		signals = append(signals, strconv.Itoa(omitted)+" more unreduced statement(s) omitted for brevity")
	}
	signals = append(signals, "reason: "+t.Note)
	return findings.SurfaceItem{
		Category:          string(surface.CategoryDBTableStructureUnproven),
		File:              t.Pos.File,
		Line:              t.Pos.Line,
		StructuralSignals: signals,
		StructuralFacts: map[string]bool{
			"table_structure_proven_complete": false,
			"primary_key_present_in_model":    len(t.PrimaryKey) > 0,
		},
		ReasonToReview: "codefit could not reduce " + strconv.Itoa(len(t.Unreduced)) + " statement(s) affecting table " +
			t.Name + ", so it cannot tell whether " + t.Name + " declares a primary key. The statement(s) are " +
			"quoted above with their file:line — read the DDL: does " + t.Name + " declare one?",
	}
}

// truncateStatement bounds a single unreduced statement's length (F6),
// appending a marker so the agent knows the quoted text is a prefix, not the
// whole statement — never silently cutting it without saying so.
func truncateStatement(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "... (truncated)"
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
		if !t.StructureProven() {
			// ABSTAIN (D4, design SS4): a dropped statement might have
			// declared the very index that would cover this FK.
			continue
		}
		coverers := db.IndexLike(t)
		for _, fk := range t.ForeignKeys {
			if db.CoveredByOrderedPrefix(coverers, fk.Columns) {
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

// describeIndexLike renders a table's index-like column lists for a signal.
// The leftmost-prefix coverage logic itself lives in core/db (db.IndexLike /
// db.CoveredByOrderedPrefix), shared with the cross rules so it never drifts
// — but that shared helper flattens each index down to a bare []string,
// losing Method. This renders from t.Indexes directly (plus the primary key,
// exactly like db.IndexLike composes its own list) so Method can be surfaced
// (F4/index-method-capture, coordinator review): a T-SQL CLUSTERED COLUMNSTORE
// INDEX carries Columns=nil, Method="columnstore" — rendering that as the
// bare literal "[]" would be indistinguishable from a rendering bug (or from
// "no index at all"), and would hide the one fact the agent needs to judge
// whether an additional ordered index is still warranted. describeIndex below
// renders that case explicitly instead.
func describeIndexLike(t db.Table) string {
	var parts []string
	for _, ix := range t.Indexes {
		parts = append(parts, describeIndex(ix))
	}
	if len(t.PrimaryKey) > 0 {
		parts = append(parts, "["+strings.Join(t.PrimaryKey, ", ")+"]")
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}

// describeIndex renders one index for a structural signal. A genuinely empty
// column list (T-SQL's CLUSTERED COLUMNSTORE INDEX, which implicitly covers
// every column and therefore names none in its own grammar) is rendered
// explicitly as "(covers all columns)" rather than the bare "[]" a normal,
// non-empty index list would produce for zero columns — the two must never
// read as the same thing. Method is appended whenever the source declared
// one, so the agent sees it even though CoveredByOrderedPrefix/
// CoveredBySetPrefix correctly never treat a columnstore index as satisfying
// an ordered/prefix lookup (it is not that kind of structure).
func describeIndex(ix db.Index) string {
	cols := "[" + strings.Join(ix.Columns, ", ") + "]"
	if len(ix.Columns) == 0 {
		cols = "(covers all columns)"
	}
	if ix.Method != "" {
		cols += " method=" + ix.Method
	}
	return cols
}

// db011 — an EXACT duplicate index (same columns in order, same uniqueness). Even
// an exact duplicate may be intentional (an in-flight migration), so which to drop
// is the human's call → SURFACE. Prefix-redundancy ([a] subsumed by [a,b]) is a
// DIFFERENT rule (db011prefix, db011prefix.go, CategoryDBPrefixRedundantIndex,
// Unit E / Phase 2.2) — the two never overlap, see db011prefix.go's doc comment.
// The reported item is deterministically the duplicate with the HIGHER Pos.Line
// (the earlier one is kept), independent of parser order.
//
// ID: both this rule and db011prefix derive from the SAME PRD number, DB-011
// (docs/PRD-codefit-v1.4.md:371, "duplicados/redundantes"), so each carries a
// letter suffix to stay individually addressable — the same sub-case
// convention as DB-052b (rules_names.go:77). This is DB-011a (the
// exact-duplicate case); db011prefix is DB-011b.
type db011 struct{}

func (db011) ID() string { return "DB-011a" }

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
			if len(ix.Columns) == 0 {
				// F4/index-method-capture (coordinator review): a zero-column
				// index (T-SQL's CLUSTERED COLUMNSTORE INDEX) has no column
				// list to compare AT ALL — without this guard, two such
				// indexes collide on the SAME {"", Unique} key and this rule
				// would claim "duplicates another index on the same columns
				// []", a claim that means nothing (there is nothing to
				// duplicate). Skip it from duplicate detection entirely
				// rather than let it fabricate a comparison the source never
				// declared.
				continue
			}
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
					"table: " + t.Name,
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
