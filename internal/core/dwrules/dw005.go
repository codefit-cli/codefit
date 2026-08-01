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
//
// # PARTITION CHILDREN ARE NOT CENSUS MEMBERS (ADR 0039)
//
// A declared PostgreSQL partition child is excluded from the census and from
// the completeness gate alike, through the ONE predicate both loops consult
// (inTimeDimensionCensus). A child is a restatement of its parent, not an
// independent fact or dimension to count, and it is unproven BY CONSTRUCTION
// (db.ReasonPartitionChildInheritsStructure) on a schema codefit read
// perfectly — so gating on it made this rule vanish on every declaratively
// partitioned warehouse. That was measured, not suspected: ADR 0038 §4
// recorded it as an open false negative and ADR 0039 closes it.
//
// The exclusion is paid for where it costs something: a warehouse that
// partitions its CALENDAR is still recognized, by the parent's name, through
// partitionedCalendarName.
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
	// incomplete count. ANY unproven census member aborts the whole rule.
	//
	// The gate reads the SAME predicate as the census loop below (census.go,
	// ADR 0038 §2 generalized by ADR 0039), so a table can never be gated
	// without being censused.
	if censusAbstains(s, func(t db.Table) bool { return inTimeDimensionCensus(t, cls) }) {
		return nil, nil
	}

	for _, t := range s.Tables {
		if !inTimeDimensionCensus(t, cls) {
			// A partition child is not a census member — but its PARENT's
			// name still answers this rule's question. See
			// partitionedCalendarName.
			if timeDim == "" && partitionedCalendarName(t) {
				timeDim = t.Partitioning.Of
			}
			continue
		}
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

// inTimeDimensionCensus reports whether t is a member of DW-005's census: a
// FACT- or DIMENSION-role table (role read from cls, never re-derived) that
// the source does not declare as a partition child. BOTH the completeness gate
// and the census loop consult this ONE predicate (census.go).
//
// A partition child is excluded for two independent reasons, either of which
// alone would be sufficient:
//
//   - It is not an independent table to count. A fact partitioned into 60
//     monthly children would put 61 names in fact_tables for one fact table,
//     and a dimension's children would do the same to dimensions.
//   - Its unprovenness is BY CONSTRUCTION, not a parser failure, so gating on
//     it would abstain DW-005 on every declaratively partitioned PostgreSQL
//     warehouse. That is the false negative ADR 0038 §4 measured and left
//     open, and ADR 0039 closes.
func inTimeDimensionCensus(t db.Table, cls *paradigm.Classification) bool {
	return hasWarehouseRole(t, cls) && !isPartitionChild(t)
}

// partitionedCalendarName reports whether t is declared as a partition of a
// table whose NAME is a calendar — the one place this rule reads a table it
// deliberately excluded from its census, and it is a correction, not an
// exception.
//
// The census exclusion, on its own, would trade one false negative for a false
// AFFIRMATION on exactly one shape, measured through the real parser: a
// warehouse that partitions its CALENDAR and whose fact references a specific
// partition (which every PostgreSQL before 12 REQUIRED — a foreign key could
// not target a partitioned parent). The fan-in then lands on the child, so ADR
// 0033's corroboration gate demotes the parent dim_date to unclassified, the
// child is excluded as a partition, and DW-005 would report "this schema
// declares no time dimension" over DDL that declares dim_date on its face.
// Before the exemption that outcome was masked by accident, because the
// child's by-construction unprovenness abstained the whole rule.
//
// Reading Partitioning.Of costs nothing and invents nothing: a child RESTATES
// its parent, the parent's name is already in the model, and it is checked
// with timeDimensionName — the SAME name signal this rule applies to every
// dimension, not a second vocabulary that could drift from it (the drift that
// once made DW-005 blind to D_DATE). The child's own name is deliberately NOT
// consulted: dim_date_2024 strips to "date2024", which is not a calendar, and
// accepting it would mean matching by containment — the exact widening
// timeDimensionName rejects because it would swallow dim_update and
// dim_candidate.
//
// It is checked for ANY partition child, regardless of role, because the shape
// that needs it is precisely the one where role classification lost the
// parent.
func partitionedCalendarName(t db.Table) bool {
	return isPartitionChild(t) && timeDimensionName(t.Partitioning.Of)
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
