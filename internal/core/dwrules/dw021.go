package dwrules

import (
	"sort"
	"strconv"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dw021 — a FACT-role table with no index using a recognized columnar/
// analytic access method (db.Index.Method). A fact table is the table an
// analytic scan workload hits hardest; a row-store method (btree/hash, or no
// method at all) serves point lookups well but does not help a full-column
// aggregate scan the way a columnar/analytic method does.
//
// EXCLUDED, and why (4R review, coordinator round — the ABSENCE of this
// reasoning was itself the finding): PostgreSQL's gin/gist/spgist are
// deliberately NOT in the vocabulary, for the same non-columnar reason
// btree/hash are excluded — they are specialized LOOKUP structures over
// particular data shapes (gin: full-text/array/jsonb containment; gist/
// spgist: geometric/range/nearest-neighbor search), not column-store or
// analytic-scan structures. They are siblings over OVERLAPPING workloads
// with no coherent "columnar/analytic" line separating one from the other
// two — admitting gin while rejecting gist/spgist would be an arbitrary cut,
// not a principled one, so this rule admits NONE of the three. Only brin
// (a genuinely block-range, columnar-adjacent method) and T-SQL's
// columnstore (genuinely columnar) qualify.
//
// SURFACE, never an affirmation (ADR 0017): whether the absence MATTERS
// depends on this table's real size and query pattern, which codefit cannot
// see from static DDL — a small fact table, or one queried only by primary
// key, has no use for a columnar index. codefit states the observed shape;
// the agent decides.
//
// VOCABULARY, defined ONCE in columnarIndexMethods below: extending the
// recognized method set is a vocabulary change that never touches this
// rule's control flow — it only widens (or narrows) which db.Index.Method
// values anyColumnarIndex treats as columnar. ReasonToReview's agent-facing
// prose is DERIVED from this same map (columnarVocabularyList) rather than
// restated by hand, so the two cannot drift out of sync (S1, 4R review).
//
// The rule reaches ONLY tables the S1 classification calls fact-role, reading
// roles from cls exactly as every other DW rule does — never re-deriving them
// (locked decision A5).
//
// PER-TABLE gate on db.Table.StructureProven() (ADR 0034/db-model-
// completeness-contract), mirroring dw001's per-table pattern rather than
// dw005/dw011's whole-rule abstain: DW-021 asks a per-table question ("does
// THIS fact table have a columnar index"), so one table's dropped statement
// has no bearing on any other table's answer. A table whose structure is not
// proven complete might have had its very columnar index declared in a
// statement the parser could not reduce — CREATE INDEX ... ON ONLY, a
// standalone CREATE FULLTEXT/SPATIAL/XML/PRIMARY XML INDEX, T-SQL's CREATE
// NONCLUSTERED COLUMNSTORE INDEX, or MySQL's pre-ON USING position all mark
// their table Complete=false (index-method-capture, PR #79) — so this single
// StructureProven() gate abstains automatically on every one of those forms,
// with NO per-dialect branch anywhere in this file.
type dw021 struct{}

func (dw021) ID() string { return "DW-021" }

// columnarIndexMethods is the recognized columnar/analytic access-method
// vocabulary, keyed by db.Index.Method's own lowercased capture convention.
// PostgreSQL contributes brin; T-SQL contributes columnstore (captured
// verbatim as of index-method-capture, PR #79). MySQL contributes nothing:
// its only index methods, btree and hash, are ordinary row-store methods,
// not columnar ones — so no MySQL-specific entry, and no dialect-branching
// logic anywhere in this rule. PostgreSQL's gin/gist/spgist are deliberately
// excluded too — see the type doc comment above for why.
var columnarIndexMethods = map[string]bool{
	"brin":        true,
	"columnstore": true,
}

// columnarIndexSignalCap bounds how many index-method entries
// describeIndexMethods renders (RES-001, 4R review): every sibling carrier in
// this project caps its rendered list — dbrules.routeUnprovenTableStatementCap
// is the precedent this mirrors — so a pathological table with hundreds of
// indexes cannot balloon a single surface item unboundedly.
const columnarIndexSignalCap = 5

func (dw021) Check(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		if cls.Roles[t.Name] != paradigm.RoleFact {
			continue
		}
		if !t.StructureProven() {
			// ABSTAIN (mirrors dw001's per-table D4 pattern): a dropped or
			// genuinely unrecognized CREATE INDEX-shaped statement might have
			// declared the very columnar index this rule is asking about.
			continue
		}
		if anyColumnarIndex(t) {
			continue
		}
		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDWNoColumnarIndex),
			File:     t.Pos.File,
			Line:     t.Pos.Line,
			StructuralSignals: []string{
				"table: " + t.Name,
				"existing_index_methods: " + describeIndexMethods(t),
			},
			StructuralFacts: map[string]bool{
				"columnar_index_detected": false,
				"has_any_index":           hasAnyIndexLike(t),
			},
			ReasonToReview: "Fact table " + t.Name + " has no index using a recognized columnar/analytic " +
				"access method (" + columnarVocabularyList() + "). Whether that matters depends on this " +
				"table's real size and query pattern, which codefit cannot see from static DDL — does this " +
				"table's scan workload justify a columnar index?",
		})
	}
	return nil, out
}

// anyColumnarIndex reports whether t carries at least one index whose method
// is in the recognized columnar/analytic vocabulary. One is enough — the
// same "either is sufficient" shape dw010's currency-column check uses. A
// primary key is deliberately NOT considered here even though it IS an
// index-like structure elsewhere in this project (db.IndexLike) — Table.
// PrimaryKey carries no Method at all, so it can never satisfy a
// columnar/analytic vocabulary test; it still needs to be REPORTED as an
// existing structure though (W1, 4R review) — see describeIndexMethods and
// hasAnyIndexLike below, which do include it.
func anyColumnarIndex(t db.Table) bool {
	for _, ix := range t.Indexes {
		if columnarIndexMethods[ix.Method] {
			return true
		}
	}
	return false
}

// hasAnyIndexLike reports whether t carries ANY index-like structure — a
// secondary index OR a primary key (W1, 4R review REL-002): a fact table
// keyed only by a PRIMARY KEY, with no secondary index at all, still has an
// index-like structure the agent should know about, even though it carries
// no columnar method. Without this, has_any_index would falsely read
// false for such a table — the exact drift this project's own manifest
// promises cannot happen between rules (db.IndexLike's "defined once"
// convention, already shared by DB-001/DB-010/DB-011b/DW-010).
func hasAnyIndexLike(t db.Table) bool {
	return len(t.Indexes) > 0 || len(t.PrimaryKey) > 0
}

// describeIndexMethods renders a table's existing index-like structures for a
// signal — every declared secondary index (stating the unspecified-method
// case explicitly) PLUS the primary key when present (W1, 4R review
// REL-002), since a PK IS an index-like structure everywhere else in this
// project (db.IndexLike) even though it carries no Method of its own. States
// the empty case ("(none)") explicitly rather than emitting a blank value,
// and caps its output (S3/RES-001) the same way dbrules.routeUnprovenTable
// caps its unreduced-statement list.
func describeIndexMethods(t db.Table) string {
	entries := indexMethodEntries(t)
	if len(entries) == 0 {
		return "(none)"
	}
	shown := entries
	if len(shown) > columnarIndexSignalCap {
		shown = shown[:columnarIndexSignalCap]
	}
	out := strings.Join(shown, ", ")
	if omitted := len(entries) - len(shown); omitted > 0 {
		out += " (and " + strconv.Itoa(omitted) + " more)"
	}
	return out
}

// indexMethodEntries lists one rendering entry per index-like structure on
// t, in declaration order: every db.Index (its method, or "(unspecified)"
// when the source declared no USING clause), then the primary key LAST when
// present — mirroring db.IndexLike's own "secondary indexes, then the PK"
// ordering convention.
func indexMethodEntries(t db.Table) []string {
	entries := make([]string, 0, len(t.Indexes)+1)
	for _, ix := range t.Indexes {
		m := ix.Method
		if m == "" {
			m = "(unspecified)"
		}
		entries = append(entries, m)
	}
	if len(t.PrimaryKey) > 0 {
		entries = append(entries, "(primary key, no access method)")
	}
	return entries
}

// columnarVocabularyList renders columnarIndexMethods' current keys, sorted,
// as agent-facing prose (S1, 4R review READ-005): ReasonToReview derives its
// vocabulary mention from this map instead of restating it by hand, so the
// doc comment's claim that the vocabulary is "defined ONCE" is actually true
// of the AGENT-FACING text too, not just the control flow.
func columnarVocabularyList() string {
	keys := make([]string, 0, len(columnarIndexMethods))
	for k := range columnarIndexMethods {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
