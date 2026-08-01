package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DW-005 over the shape of two REAL corpora, parsed by the REAL provider.
//
// dw-gamerec (D_DATE) and dw-kantor (D_Date) are not vendored in this
// repository, so what is reproduced here is their SHAPE as genuine DDL: a
// single-letter-prefixed star whose calendar dimension is keyed by an INTEGER
// smart key. That last detail is what makes this test worth running — with an
// integer key the structural date-grain signal cannot fire, so the NAME is the
// only thing that can suppress DW-005, and the measured regression is exactly
// reproduced.
//
// Why real DDL rather than a db.Table literal: a hand-built table can hold
// values the reducer never produces (a role map the classifier would never
// assign, a Complete flag the parser would never set). Everything below —
// column types, primary keys, foreign keys, the Complete flag, and therefore
// every role in the Classification — comes from the real parse.
//
// MEASURED REGRESSION this locks: after the role-name vocabulary widened,
// D_DATE / D_Date began classifying as dimensions and F_* as a fact, so DW-005
// stopped abstaining via len(facts)==0 and started emitting "fact table reaches
// no time dimension" over a schema that plainly declares a calendar. A silent
// miss turned into a confident false claim.

// starDDL renders the dw-gamerec/dw-kantor shape with the calendar dimension
// spelled as the corpus spells it.
//
// The keys are spelled _sk so this star carries schema-wide warehouse evidence
// that is INDEPENDENT of its calendar (ADR 0037). That independence is the whole
// reason for the choice: the control below renames the calendar away to prove
// DW-005 still fires on a genuine absence, and if the only evidence in the
// schema WERE the calendar, renaming it would close the schema gate, withhold
// every role, and make the control pass by abstention instead of by firing —
// a green test protecting nothing.
func starDDL(calendar string) string {
	return `CREATE TABLE ` + calendar + ` (
    date_sk integer NOT NULL PRIMARY KEY,
    full_date date NOT NULL,
    fiscal_year integer NOT NULL
);

CREATE TABLE D_GAME (
    game_sk integer NOT NULL PRIMARY KEY,
    title varchar(200) NOT NULL
);

CREATE TABLE F_GAME_SALES (
    sale_sk integer NOT NULL PRIMARY KEY,
    date_sk integer NOT NULL REFERENCES ` + calendar + ` (date_sk),
    game_sk integer NOT NULL REFERENCES D_GAME (game_sk),
    units integer NOT NULL
);
`
}

func parseStar(t *testing.T, calendar string) *db.Schema {
	t.Helper()
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres()))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "warehouse.sql", Content: []byte(starDDL(calendar))}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

func TestDW005_RealDDL_SingleLetterCalendarDimension_DoesNotFire(t *testing.T) {
	for _, calendar := range []string{"D_DATE", "D_Date", "d_date"} {
		t.Run(calendar, func(t *testing.T) {
			s := parseStar(t, calendar)
			if len(s.Tables) != 3 {
				t.Fatalf("parsed %d tables, want 3 — fixture or reducer drifted", len(s.Tables))
			}

			c := paradigm.Detect(s)

			// TEST ASSUMPTIONS, derived from the real parse rather than
			// assumed: if the star does not classify, DW-005 abstains for the
			// WRONG reason and the assertion below would pass vacuously.
			fact := tableNamed(t, s, "F_GAME_SALES")
			if !fact.StructureProven() {
				t.Fatalf("F_GAME_SALES structure is unproven — DW-005 would abstain for the wrong reason")
			}
			if len(fact.ForeignKeys) != 2 {
				t.Fatalf("F_GAME_SALES foreign keys = %d, want 2 (the fan-out the fact role needs)", len(fact.ForeignKeys))
			}
			if got := c.Roles["F_GAME_SALES"]; got != paradigm.RoleFact {
				t.Fatalf("role[F_GAME_SALES] = %q, want fact — without a fact table DW-005 abstains "+
					"and this test proves nothing", got)
			}
			if got := c.Roles[calendar]; got != paradigm.RoleDimension {
				t.Fatalf("role[%s] = %q, want dimension", calendar, got)
			}
			// The calendar's key is an INTEGER, so the structural date-grain
			// signal is unavailable and only the name can suppress the rule.
			cal := tableNamed(t, s, calendar)
			if len(cal.PrimaryKey) != 1 {
				t.Fatalf("%s primary key = %v, want exactly one column", calendar, cal.PrimaryKey)
			}
			for _, col := range cal.Columns {
				if col.Name == cal.PrimaryKey[0] && col.Type == db.TypeDateTime {
					t.Fatalf("%s is keyed by a date column — the structural signal would mask the "+
						"name vocabulary this test exists to exercise", calendar)
				}
			}

			_, surf := dwrules.Run(s, &c)
			for _, it := range surf {
				if it.Category == string(surface.CategoryDWNoTimeDimension) {
					t.Errorf("DW-005 fired over real DDL that declares %s: %q — a declared calendar "+
						"reported as absent is a confident false claim, not a miss", calendar, it.ReasonToReview)
				}
			}
		})
	}
}

// The control: the identical real-parsed star with the calendar renamed to an
// ordinary dimension MUST still fire. Without this, the test above could pass
// under a vocabulary that recognizes every dimension as a calendar.
func TestDW005_RealDDL_NoCalendarDimension_StillFires(t *testing.T) {
	s := parseStar(t, "D_REGION")
	c := paradigm.Detect(s)

	if got := c.Roles["F_GAME_SALES"]; got != paradigm.RoleFact {
		t.Fatalf("role[F_GAME_SALES] = %q, want fact", got)
	}

	_, surf := dwrules.Run(s, &c)
	found := 0
	for _, it := range surf {
		if it.Category == string(surface.CategoryDWNoTimeDimension) {
			found++
		}
	}
	if found != 1 {
		t.Errorf("DW-005 items = %d, want exactly 1 — this star genuinely has no time dimension, and "+
			"a vocabulary that stays quiet here is hiding real absences", found)
	}
}
