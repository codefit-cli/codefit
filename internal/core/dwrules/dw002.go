package dwrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dw002 — a DIMENSION-role table that is not keyed by a surrogate: its primary
// key is COMPOSITE, or it is a single column that is not provably an integer
// surrogate. A dimension keyed by the source system's own business key cannot
// track history (every SCD-2 strategy needs a key the warehouse owns) and binds
// every fact row to an identifier the source may reuse or re-issue.
//
// SURFACE, never an affirmation (ADR 0017): a natural-key dimension is a
// legitimate choice for a small, immutable conformed dimension, and a
// warehouse migrating off a source system may carry one deliberately. codefit
// states the key it observed; the agent decides.
//
// THE SURROGATE TEST is structural and deliberately narrow: a single-column
// primary key whose declared neutral type is an integer. That is the shape
// every well-modelled warehouse uses (AdventureWorksDW's
// DimCustomer.CustomerKey is exactly this), and it needs no name guessing.
//
// DECLARED LIMIT: a UUID/GUID surrogate key is typed as a string in the
// neutral model, so it reads as "not provably an integer surrogate" and DOES
// fire. That is a known, stated false positive rather than a silent one — the
// emitted facts (composite_primary_key, integer_primary_key,
// primary_key_column_resolved) are exactly what the agent needs to dismiss it
// in one step. Widening the test to "integer OR uuid-shaped" would require
// guessing from the column name, which this codebase deliberately avoids for a
// structural check.
//
// A dimension with NO primary key ABSTAINS: DB-050 already affirms that case
// deterministically, and double-reporting one defect under two IDs is noise.
type dw002 struct{}

func (dw002) ID() string { return "DW-002" }

func (dw002) Check(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		if cls.Roles[t.Name] != paradigm.RoleDimension {
			continue
		}
		if len(t.PrimaryKey) == 0 {
			continue // DB-050 owns the no-primary-key case
		}
		composite := len(t.PrimaryKey) > 1
		col, resolved := columnNamed(t, t.PrimaryKey[0])
		integerKey := !composite && resolved && col.Type == db.TypeInt
		if integerKey {
			continue
		}
		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDWDimensionNoSurrogateKey),
			File:     t.Pos.File,
			Line:     t.Pos.Line,
			StructuralSignals: []string{
				"table: " + t.Name,
				"primary_key: " + strings.Join(t.PrimaryKey, ", "),
				"primary_key_type: " + describeKeyType(col, composite, resolved),
			},
			StructuralFacts: map[string]bool{
				"composite_primary_key":       composite,
				"integer_primary_key":         integerKey,
				"primary_key_column_resolved": !composite && resolved,
			},
			ReasonToReview: "Dimension " + t.Name + " is keyed by (" + strings.Join(t.PrimaryKey, ", ") +
				"), which is not a single integer surrogate. Is this the source system's business key? " +
				"A dimension without a warehouse-owned surrogate cannot version rows (SCD-2), and a reissued " +
				"source key silently rewrites history in every fact that references it.",
		})
	}
	return nil, out
}

// columnNamed finds a table's column by name. resolved is false when the
// schema does not declare it — an incompletely reconstructed model, which is
// stated as a fact rather than assumed away.
func columnNamed(t db.Table, name string) (db.Column, bool) {
	for _, c := range t.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return db.Column{}, false
}

// describeKeyType renders the primary key's type for a signal, distinguishing
// "composite" and "not declared in the schema" from a real type.
func describeKeyType(col db.Column, composite, resolved bool) string {
	switch {
	case composite:
		return "(composite)"
	case !resolved:
		return "(column not declared in the parsed schema)"
	case col.RawType != "":
		return col.RawType
	default:
		return string(col.Type)
	}
}
