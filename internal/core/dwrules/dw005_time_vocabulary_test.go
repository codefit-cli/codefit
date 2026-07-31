package dwrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// This file locks the TIME-DIMENSION NAME vocabulary that DW-005 (and, through
// isTimeDimension, DW-011) uses.
//
// It exists because of a measured regression: the role-name vocabulary in
// internal/core/paradigm was widened (case-insensitive, leading AND trailing
// underscore tokens, separator-free PascalCase) while this SECOND, PARALLEL
// vocabulary was left matching three hardcoded spellings. Two real corpora
// (dw-gamerec's D_DATE, dw-kantor's D_Date) went from "no fact table at all,
// rule abstains" to "fact table found, and codefit confidently reports it
// reaches no time dimension" — over schemas that plainly declare a calendar.
// A silent miss became a confident false claim, which is strictly worse.
//
// The lock is therefore in two halves, deliberately:
//
//	timeDimensionName is exercised DIRECTLY as the pure string predicate it is.
//	Names like "update" or "candidate" can never carry paradigm.RoleDimension in
//	production (roleFor demands a recognized role token first), so asserting
//	them through a hand-assigned Classification would lock a state the real
//	pipeline cannot produce. Tested at the predicate, the guard is real.
//
//	DW-005 itself is exercised only with names the real classifier CAN produce
//	as a dimension — dim_update, d_date — so the schema-level assertions never
//	depend on an impossible role map.

// A dimension keyed by an INTEGER smart key (the yyyymmdd date_key convention
// AdventureWorksDW, dw-gamerec and dw-kantor all use) is the case that matters:
// the STRUCTURAL date-grain signal cannot fire on it, so the NAME is the only
// thing that can suppress DW-005. Every name case below uses this shape, so a
// pass can never come from the structural path masking the vocabulary.
func dw005SuppressedByName(t *testing.T, dimName string) bool {
	t.Helper()
	s, c := starWith(
		dimTable("fact_sales", []string{"id"}, dwcol("id", db.TypeInt)),
		dimTable(dimName, []string{"date_key"}, dwcol("date_key", db.TypeInt)),
	)
	return len(itemsOfCategory(run005(s, c), surface.CategoryDWNoTimeDimension)) == 0
}

// The REGRESSION itself. Every spelling here is a real observed one: D_DATE is
// dw-gamerec's, D_Date is dw-kantor's, date_dim/time_dim are the trailing-token
// form TPC-DS uses, and the dim_* / Dim* forms are what already worked and must
// keep working.
func TestTimeDimensionName_RecognizedSpellings(t *testing.T) {
	for _, name := range []string{
		// dw-gamerec / dw-kantor — the measured false-claim corpora.
		"D_DATE", "D_Date", "d_date",
		// Trailing underscore token.
		"date_dim", "time_dim", "calendar_dim", "DATE_DIM",
		// Leading underscore token — the three spellings that already worked.
		"dim_date", "dim_time", "dim_calendar", "Dim_Date", "DIM_DATE",
		// Separator-free PascalCase.
		"DimDate", "DimTime", "DimCalendar",
	} {
		t.Run(name, func(t *testing.T) {
			if !timeDimensionName(name) {
				t.Errorf("timeDimensionName(%q) = false, want true — a dimension whose name states "+
					"date/time/calendar must be recognized whichever recognized role token it carries", name)
			}
		})
	}
}

// The GUARD on the widening. normalizeDWIdent strips separators, so a naive
// substring test for "date" swallows update, candidate and validate whole —
// ordinary words that have nothing to do with a calendar. Treating one of those
// as the schema's time dimension would SILENCE DW-005 on a warehouse that
// genuinely has no calendar: a false negative bought with a wider vocabulary,
// exactly the trade this project refuses.
func TestTimeDimensionName_DoesNotSwallowOrdinaryWords(t *testing.T) {
	for _, name := range []string{
		// No role token at all — must not match on their own.
		"update", "candidate", "validate", "datetime_log", "dimensional_data",
		// WITH a recognized role token stripped, the remainder must still be
		// checked by EQUALITY, never containment: "dim_update" leaves "update".
		"dim_update", "dim_candidate", "dim_validate", "dim_datetime_log",
		"d_update", "update_dim",
	} {
		t.Run(name, func(t *testing.T) {
			if timeDimensionName(name) {
				t.Errorf("timeDimensionName(%q) = true, want false — this is an ordinary word, not a "+
					"calendar; accepting it would silence DW-005 on a warehouse with no time dimension", name)
			}
		})
	}
}

// DECLARED RESIDUAL LIMIT, locked so it is a known gap rather than a surprise:
// only a name that is EXACTLY a recognized role token plus date/time/calendar
// is matched. A qualified or spelled-out calendar name is not, and such a
// warehouse still gets a DW-005 surface item to judge. That is a MISS (the
// agent is asked a question it can answer from the schema), never a false
// claim — the direction this rule is deliberately biased in.
func TestTimeDimensionName_DeclaredLimit_QualifiedCalendarNames(t *testing.T) {
	for _, name := range []string{
		"date_dimension",  // spelled out rather than tokenized
		"dim_date_full",   // qualified remainder
		"dim_fiscal_date", // qualified remainder
		"dim_datetime",    // not one of date/time/calendar
		"date",            // no role token at all
		"calendar",        // no role token at all
	} {
		t.Run(name, func(t *testing.T) {
			if timeDimensionName(name) {
				t.Errorf("timeDimensionName(%q) = true — this spelling is a DECLARED LIMIT (not "+
					"recognized). If it now matches, the vocabulary widened: update the doc comment's "+
					"stated limit and dbcoverage.go in the same change", name)
			}
		})
	}
}

// The regression at the RULE level, using the only spellings the real
// classifier can actually hand DW-005 as a dimension. Before the fix these two
// schemas produced a "fact table reaches no time dimension" item over a
// declared calendar.
func TestDW005_SingleLetterTimeDimension_DoesNotFire(t *testing.T) {
	for _, name := range []string{"D_DATE", "D_Date", "d_date", "date_dim"} {
		t.Run(name, func(t *testing.T) {
			if !dw005SuppressedByName(t, name) {
				t.Errorf("DW-005 fired over a schema whose dimension is named %q — that is a "+
					"CONFIDENT FALSE CLAIM about a declared calendar, not a miss", name)
			}
		})
	}
}

// Control for the test above: it must be able to FAIL. A dimension whose name
// is a recognized role token plus an ordinary word is NOT a calendar, and
// DW-005 must still fire — otherwise the suppression assertions above would
// pass under a vocabulary that accepts everything.
func TestDW005_RoleTokenPlusOrdinaryWord_StillFires(t *testing.T) {
	for _, name := range []string{"dim_update", "dim_customer", "d_country"} {
		t.Run(name, func(t *testing.T) {
			if dw005SuppressedByName(t, name) {
				t.Errorf("DW-005 stayed quiet over a schema whose only dimension is %q — the time "+
					"vocabulary is overmatching and is now hiding real absences", name)
			}
		})
	}
}
