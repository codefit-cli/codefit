package dwrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dw011 — SCD-1 and SCD-2 dimensions MIXED within one schema. One dimension
// versions its rows and keeps history; another overwrites in place. A report
// that joins both silently mixes point-in-time attributes with as-of-today
// attributes, and the numbers stop reconciling — the kind of defect that
// surfaces months later as "the Q3 report changed".
//
// SCHEMA-LEVEL: at most ONE item, anchored on the first compared dimension in
// schema order, surfacing BOTH groups. The observation is the inconsistency
// itself, not any single table, so one item per dimension would be N copies of
// one question.
//
// SURFACE, never an affirmation (ADR 0017): mixing strategies is a legitimate,
// deliberate design in many warehouses — history is expensive, and plenty of
// dimensions genuinely do not need it. codefit states which dimensions fall in
// which group; whether the split is intentional is the agent's judgment.
//
// TIME DIMENSIONS ARE EXCLUDED from the comparison. A calendar is not a
// slowly-changing dimension by definition — dates do not get new versions — so
// counting it as SCD-1 would make this rule fire on essentially every
// correctly built warehouse (they nearly all have a dim_date). That is the
// noise a name-shaped rule has to avoid to stay worth reading. Excluding it
// never HIDES a real mix: a genuine SCD-1/SCD-2 split between two other
// dimensions still fires, which is locked as a test.
//
// The SCD-2 test is the same vocabulary DW-010 uses (valid_from / valid_to /
// is_current / effective_date, separator-insensitive), so the two rules cannot
// disagree about what "keeps history" means, and it inherits the same declared
// limit: a dimension using a different history vocabulary reads as SCD-1.
type dw011 struct{}

func (dw011) ID() string { return "DW-011" }

func (dw011) Check(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	var scd2, scd1 []string
	var anchor db.Pos
	anchored := false

	for _, t := range s.Tables {
		if cls.Roles[t.Name] != paradigm.RoleDimension || isTimeDimension(t) {
			continue
		}
		if !anchored {
			anchor, anchored = t.Pos, true
		}
		if markers, _ := scdColumns(t); len(markers) > 0 {
			scd2 = append(scd2, t.Name)
			continue
		}
		scd1 = append(scd1, t.Name)
	}

	if len(scd2) == 0 || len(scd1) == 0 {
		return nil, nil
	}

	return nil, []findings.SurfaceItem{{
		Category: string(surface.CategoryDWMixedSCDStrategies),
		File:     anchor.File,
		Line:     anchor.Line,
		StructuralSignals: []string{
			"scd2_dimensions: " + strings.Join(scd2, ", "),
			"scd1_dimensions: " + strings.Join(scd1, ", "),
		},
		StructuralFacts: map[string]bool{"mixed_scd_strategies": true},
		ReasonToReview: "This schema mixes slowly-changing-dimension strategies: " + strings.Join(scd2, ", ") +
			" keep row history, while " + strings.Join(scd1, ", ") + " overwrite in place. Is that split " +
			"deliberate? A report joining both mixes point-in-time attributes with as-of-today attributes, " +
			"so the same query re-run later can return different history.",
	}}
}
