package dwrules

import (
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
// SURFACE, never an affirmation (ADR 0017): whether the absence MATTERS
// depends on this table's real size and query pattern, which codefit cannot
// see from static DDL — a small fact table, or one queried only by primary
// key, has no use for a columnar index. codefit states the observed shape;
// the agent decides.
//
// VOCABULARY, defined ONCE in columnarIndexMethods below: extending the
// recognized method set is a vocabulary change that never touches this
// rule's control flow — it only widens (or narrows) which db.Index.Method
// values anyColumnarIndex treats as columnar.
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
// PostgreSQL contributes brin and gin; T-SQL contributes columnstore
// (captured verbatim as of index-method-capture, PR #79). MySQL contributes
// nothing: its only index methods, btree and hash, are ordinary row-store
// methods, not columnar ones — so no MySQL-specific entry, and no
// dialect-branching logic anywhere in this rule.
var columnarIndexMethods = map[string]bool{
	"brin":        true,
	"gin":         true,
	"columnstore": true,
}

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
				"has_any_index":           len(t.Indexes) > 0,
			},
			ReasonToReview: "Fact table " + t.Name + " has no index using a recognized columnar/analytic " +
				"access method (brin/gin on PostgreSQL, columnstore on SQL Server). Whether that matters " +
				"depends on this table's real size and query pattern, which codefit cannot see from static " +
				"DDL — does this table's scan workload justify a columnar index?",
		})
	}
	return nil, out
}

// anyColumnarIndex reports whether t carries at least one index whose method
// is in the recognized columnar/analytic vocabulary. One is enough — the
// same "either is sufficient" shape dw010's currency-column check uses.
func anyColumnarIndex(t db.Table) bool {
	for _, ix := range t.Indexes {
		if columnarIndexMethods[ix.Method] {
			return true
		}
	}
	return false
}

// describeIndexMethods renders a table's existing index methods for a
// signal, stating the empty case (no indexes at all) and the unspecified
// case (an index with no declared method) explicitly rather than emitting a
// blank value.
func describeIndexMethods(t db.Table) string {
	if len(t.Indexes) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(t.Indexes))
	for _, ix := range t.Indexes {
		m := ix.Method
		if m == "" {
			m = "(unspecified)"
		}
		parts = append(parts, m)
	}
	return strings.Join(parts, ", ")
}
