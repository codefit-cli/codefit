package dwrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dw005 — the schema has fact tables but NO time dimension. Nearly every
// analytic question is time-shaped ("per day", "month over month", "same
// quarter last year"); a star with no conformed time dimension pushes that
// grain into ad-hoc date arithmetic on each fact, which is where inconsistent
// fiscal calendars and off-by-one period boundaries come from.
//
// SCHEMA-LEVEL: it emits AT MOST ONE item, anchored on the first fact table in
// schema order, because the observation is about the schema as a whole. One
// item per fact would multiply a single absence into N identical questions.
//
// SURFACE, never an affirmation (ADR 0017): the time dimension may live in a
// different schema, be a database-native calendar, or be genuinely unnecessary
// for an unhistorized mart. codefit states what it observed — the facts it
// found and the dimensions it found — and the agent decides.
//
// A time dimension is recognized by EITHER signal, deliberately:
//
//	NAME — a recognized warehouse role token (dim_/d_/*_dim/Dim…, the same
//	vocabulary paradigm classifies roles with) plus date, time or calendar:
//	dim_date, D_DATE, date_dim, DimCalendar. Cheap, and it matches the
//	overwhelming majority of warehouses. See timeDimensionName for why this
//	composes on paradigm's vocabulary instead of listing spellings.
//
//	STRUCTURE — a dimension whose PRIMARY KEY is a single date/datetime
//	column. Such a table is date-grained by construction: one row per point in
//	time. This is what keeps the rule from false-positiving on a warehouse
//	that simply named its calendar something else.
//
// The structural signal is deliberately keyed on the PRIMARY KEY, not on "the
// table contains a date column": a dimension with an updated_at audit stamp is
// not a time dimension, and accepting any date column would suppress the rule
// on almost every schema — a silent false negative, the failure mode codefit
// exists to prevent.
//
// DECLARED LIMIT: a time dimension keyed by an INTEGER smart key (the very
// common yyyymmdd date_key, which is what AdventureWorksDW's DimDate uses) is
// recognized only by its NAME — the integer key is structurally
// indistinguishable from any other surrogate. A warehouse that both uses an
// integer date key AND names its calendar unconventionally will fire here.
type dw005 struct{}

func (dw005) ID() string { return "DW-005" }

func (dw005) Check(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	var facts, dims []string
	var anchor db.Pos
	timeDim := ""

	// D4 (design SS4): SCHEMA-LEVEL ABSTAIN. This rule is a census judgment
	// ("does this schema have a time dimension at all"), so a per-table
	// continue would silently SHRINK the census and still emit — a WORSE lie
	// than abstaining, because the item would look authoritative over an
	// incomplete count. ANY unproven fact- or dimension-role table aborts the
	// whole rule.
	for _, t := range s.Tables {
		role := cls.Roles[t.Name]
		if (role == paradigm.RoleFact || role == paradigm.RoleDimension) && !t.StructureProven() {
			return nil, nil
		}
	}

	for _, t := range s.Tables {
		switch cls.Roles[t.Name] {
		case paradigm.RoleFact:
			if len(facts) == 0 {
				anchor = t.Pos
			}
			facts = append(facts, t.Name)
		case paradigm.RoleDimension:
			dims = append(dims, t.Name)
			if timeDim == "" && isTimeDimension(t) {
				timeDim = t.Name
			}
		}
	}

	if len(facts) == 0 || timeDim != "" {
		return nil, nil
	}

	return nil, []findings.SurfaceItem{{
		Category: string(surface.CategoryDWNoTimeDimension),
		File:     anchor.File,
		Line:     anchor.Line,
		StructuralSignals: []string{
			"fact_tables: " + strings.Join(facts, ", "),
			"dimensions: " + describeRefs(dims),
		},
		StructuralFacts: map[string]bool{
			"time_dimension_detected": false,
			"has_any_dimension":       len(dims) > 0,
		},
		ReasonToReview: "This schema declares fact table(s) (" + strings.Join(facts, ", ") + ") but no dimension " +
			"that is recognizably a time dimension — neither by name (a role token plus date/time/calendar, " +
			"e.g. dim_date, D_DATE, date_dim) nor by grain (a dimension keyed by a single date column). Is the " +
			"calendar defined elsewhere, or will every time-sliced query have to do its own date arithmetic " +
			"on the facts?",
	}}
}

// isTimeDimension reports whether a dimension table is a time dimension, by
// conventional NAME or by date GRAIN (a single date/datetime primary key).
func isTimeDimension(t db.Table) bool {
	if timeDimensionName(t.Name) {
		return true
	}
	if len(t.PrimaryKey) != 1 {
		return false
	}
	col, ok := columnNamed(t, t.PrimaryKey[0])
	return ok && col.Type == db.TypeDateTime
}

// timeDimensionName reports whether a table's NAME states that it is a
// calendar. It COMPOSES on paradigm.StripRoleToken rather than carrying its own
// spelling list: strip the recognized warehouse role token off either end of
// the name, then check what is left against the small time vocabulary
// date / time / calendar.
//
// WHY COMPOSED, not listed. This function used to hold a hardcoded
// {dim_date, dim_time, dim_calendar}. When paradigm's ROLE vocabulary widened
// (case-insensitive, leading and trailing tokens, PascalCase) this second,
// parallel vocabulary stayed put, and the two drifted: dw-gamerec's D_DATE and
// dw-kantor's D_Date began classifying as dimensions while remaining invisible
// HERE, so DW-005 stopped abstaining and started reporting "this fact table
// reaches no time dimension" over schemas that plainly declare a calendar — a
// silent miss turned into a confident false claim. Composing means the next
// role-vocabulary widening cannot silently re-open that hole.
//
// The remainder is matched by EQUALITY on the separator-stripped form, never by
// containment. normalizeDWIdent removes separators, so a substring test for
// "date" would accept dim_update, dim_candidate and dim_validate — treating an
// ordinary dimension as the schema's calendar and SILENCING DW-005 on a
// warehouse that genuinely has none. A rule that hides real absences to catch
// more spellings is not worth having.
//
// DECLARED LIMIT: only a name that is a recognized role token PLUS exactly
// date/time/calendar is matched. A spelled-out or qualified calendar name —
// date_dimension, dim_date_full, dim_fiscal_date, dim_datetime — is NOT
// recognized by name, and neither is a bare "calendar" carrying no role token
// (which paradigm would never classify as a dimension in the first place).
// Such a warehouse still gets the STRUCTURAL signal in isTimeDimension, and
// failing that, a DW-005 surface item to judge: a question the agent can answer
// from the schema, never a claim codefit cannot back.
func timeDimensionName(name string) bool {
	rest, ok := paradigm.StripRoleToken(name)
	if !ok {
		return false
	}
	switch normalizeDWIdent(rest) {
	case "date", "time", "calendar":
		return true
	default:
		return false
	}
}

// normalizeDWIdent lowercases an identifier and drops separators.
func normalizeDWIdent(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s))
}
