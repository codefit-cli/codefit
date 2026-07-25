package dwrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dw001 — a FACT-role table with no foreign key to any DIMENSION-role table.
// A star schema exists so measures join to dimensions; a fact that reaches no
// dimension is either a degenerate fact (measures with no context), a table
// still under construction, or a fact whose joins live only in the ETL and not
// in the DDL. Which of those it is, is the AGENT's judgment — so this is
// SURFACE, never an affirmation (ADR 0017).
//
// The rule reaches ONLY for tables the S1 classification calls fact-role; an
// unclassified or OLTP table is never evaluated (spec: "Rules do not fire on
// OLTP or unclassified tables"). Roles are read from cls, never re-derived
// here — the classification is the second neutral input (ADR 0033), and a rule
// that re-guessed roles would fork the heuristic.
//
// A foreign key to another FACT, to a STAGING table, or to an unclassified
// table is deliberately NOT a dimension join: only a dimension-role target
// counts. That is the rule's trap — a fact-to-fact bridge looks joined but
// carries no dimensional context.
type dw001 struct{}

func (dw001) ID() string { return "DW-001" }

func (dw001) Check(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		if cls.Roles[t.Name] != paradigm.RoleFact {
			continue
		}
		refs := referencedTables(t)
		if anyDimension(refs, cls) {
			continue
		}
		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDWNoFactDimensionFK),
			File:     t.Pos.File,
			Line:     t.Pos.Line,
			StructuralSignals: []string{
				"table: " + t.Name,
				"foreign_keys: " + describeRefs(refs),
			},
			StructuralFacts: map[string]bool{
				"dimension_fk_detected": false,
				"has_any_foreign_key":   len(refs) > 0,
			},
			ReasonToReview: "Fact table " + t.Name + " declares no foreign key to any table classified as a " +
				"dimension. Is this a degenerate fact by design, are its dimension joins enforced only in the " +
				"ETL rather than in the schema, or is a relationship genuinely missing?",
		})
	}
	return nil, out
}

// referencedTables lists the DISTINCT tables t references by foreign key, in
// first-declared order so the emitted signal is deterministic regardless of map
// iteration.
func referencedTables(t db.Table) []string {
	seen := make(map[string]bool, len(t.ForeignKeys))
	out := make([]string, 0, len(t.ForeignKeys))
	for _, fk := range t.ForeignKeys {
		if seen[fk.RefTable] {
			continue
		}
		seen[fk.RefTable] = true
		out = append(out, fk.RefTable)
	}
	return out
}

// anyDimension reports whether any referenced table is dimension-role.
func anyDimension(refs []string, cls *paradigm.Classification) bool {
	for _, r := range refs {
		if cls.Roles[r] == paradigm.RoleDimension {
			return true
		}
	}
	return false
}

// describeRefs renders the referenced-table list for a signal, stating the
// empty case explicitly rather than emitting a blank value.
func describeRefs(refs []string) string {
	if len(refs) == 0 {
		return "(none)"
	}
	return strings.Join(refs, ", ")
}
