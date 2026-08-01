package sqlddl_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// The schema gate (internal/core/paradigm) MEASURED over every SQL corpus
// vendored in this repository, through the REAL parser — not over hand-built
// db.Schema values, which can hold shapes the parser never produces.
//
// Stage 1 computed six schema-wide warehouse signals and wired them to nothing.
// The point of building it inert was to get THIS table before deciding anything,
// and the table is uncomfortable in a way a hunch would never have been:
//
//	CORPUS                              PARADIGM  SIGNALS THAT FIRED
//	mysql/sakila_excerpt.sql            OLTP      no_audit_timestamps, star_topology
//	pagila_excerpt.sql                  OLTP      no_audit_timestamps
//	tsql/adventureworks_excerpt.sql     OLTP      no_audit_timestamps
//	tsql/adventureworksdw_real_objects  WAREHOUSE calendar_table, no_audit_timestamps
//	(every other corpus)                --        none
//
// Read the first and fourth rows together. The ONE genuine Kimball warehouse in
// the repository and a three-table excerpt of Sakila — a rental shop's
// transactional schema — fire the SAME NUMBER of signals, two each. A naive
// "count the signals" threshold cannot tell them apart at all, at any cutoff:
// at >= 2 both are warehouses, at >= 3 neither is. WHICH signals fired is the
// only thing that separates them.
//
// HOW THIS TABLE WAS READ WHEN THE DECISION WAS MADE, and why it still says the
// same thing: at stage 1 (ADR 0035) the warehouse fired ONE signal and Sakila
// TWO, so a >= 2 threshold got both backwards. The T-SQL
// ALTER TABLE ... ADD CONSTRAINT reducer fix (PR #82) then landed on main and
// proved the warehouse's three tables, which let no_audit_timestamps stop
// abstaining and moved this row from one signal to two. The counting argument
// got no weaker for it — it merely changed from "the threshold ranks them
// backwards" to "the threshold cannot rank them at all". Both readings are
// measured, neither is a hunch, and stage 2 selects rather than counts either
// way.
//
// STAGE 2 (ADR 0037) decided from that shape, plus the 26-corpus measurement in
// ADR 0036: the verdict SELECTS the three zero-false-positive signals and
// requires ANY ONE of them, rather than counting all six. This table is what
// that decision looks like on real vendored DDL — Sakila's two signals are
// exactly the two that do NOT vote, so it stays transactional, while
// AdventureWorksDW's calendar_table opens the gate and the no_audit_timestamps
// it now shares with Sakila is worth no votes to either. The gateOpen column
// below records it, corpus by corpus.
//
// The three causes behind those numbers, each independently verifiable above:
//
//  1. AdventureWorksDW's structure is fully PROVEN as of the reducer fix above:
//     its three real primary keys and eight real foreign keys are in the model,
//     so the absence-based signals judge it instead of abstaining. That is why
//     no_audit_timestamps affirms here (none of its three tables declares
//     created_at/updated_at) while bulk_load_shape does NOT: eight declared
//     foreign keys falsify its no-FKs premise outright, rather than leaving it
//     unable to conclude. star_topology also stays silent, for a third reason
//     again — six of those eight foreign keys point at dimension tables this
//     three-table excerpt does not vendor, and an absent spoke can never be
//     shown to be a leaf. Before the fix, ALL of these abstained on unproven
//     structure, which was correct behavior over a model the parser could not
//     prove complete and is why ADR 0035 named that fix a stage-2 prerequisite.
//  2. no_audit_timestamps fires on all three OLTP corpora because they spell
//     their audit stamp last_update (Sakila, Pagila) or ModifiedDate
//     (AdventureWorks), and the signal reuses db052's created_at/updated_at
//     convention rather than inventing a second spelling rule. A real false
//     positive, measured rather than guessed — and now visibly a signal that
//     fires on BOTH sides of the question, which is precisely why it does not
//     vote.
//  3. star_topology fires on Sakila because film_actor references actor and
//     film and neither references anything back — a textbook depth-1 star that
//     is in fact a join table. This is the architectural premise restated: no
//     single table, and no single signal, tells a warehouse from a transactional
//     schema.
//
// THE SIXTH SIGNAL, type_profile_split, fires on NOTHING here, and the two
// reasons are worth separating because only one of them is a threshold:
//
//   - every vendored OLTP corpus is a 3-to-5-table excerpt whose tables are
//     uniformly mixed — there is no split to see, which is the correct answer
//     and the acceptance bar this signal was required to clear;
//   - the reference warehouse abstains on a gap the ALTER TABLE fix above was
//     never going to touch, which is exactly what ADR 0036 predicted:
//     AdventureWorksDW brackets its type names ([int], [nvarchar](50)), the
//     T-SQL type map never matches them, and all 74 of its parsed columns land
//     on db.TypeUnknown. A SECOND, independent parser gap, still open. Locked
//     with real DDL in
//     TestSchemaGate_TypeProfileSplit_AbstainsOnBracketedTSQLTypes.
//
// This file is ALSO the end-to-end half of the wiring lock: every row asserts
// what paradigm.Detect returns over the same real parse, so the verdict and its
// consequence are measured together on real DDL.

// gateCorpusCase is one measured corpus.
type gateCorpusCase struct {
	// path is relative to testdata/.
	path string
	// tables and proven are the parse facts the signals depend on. They are
	// asserted so that a parser change which silently empties a corpus cannot
	// leave the signal expectations passing by vacuity — the exact failure mode
	// CLAUDE.md's "a fixture is verified by its CONTENT" rule exists to catch.
	tables, proven int
	// fired is the EXACT set of signals, in paradigm's fixed order.
	fired []paradigm.Signal
	// gateOpen is the VERDICT those signals produce (ADR 0037). It is asserted
	// separately from fired because the two are not the same question: a corpus
	// can fire two signals and still be refused.
	gateOpen bool
	// paradigmIs what Detect returns.
	paradigmIs paradigm.Paradigm
}

// gateCorpusExpectations covers EVERY .sql file under testdata/. The test fails
// if a corpus is added without being measured here, so the table can never go
// quietly stale.
//
// Corpora with 0 tables are not broken fixtures: the *_real_objects.sql files
// vendor views, procedures and triggers BY DESIGN, and the constructed routine
// fixtures declare no tables at all.
var gateCorpusExpectations = []gateCorpusCase{
	{path: "mysql/constructed_dynamic_sql_proc.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "mysql/constructed_inline_index_using.sql", tables: 2, proven: 2, paradigmIs: paradigm.ParadigmOLTP},
	{path: "mysql/constructed_non_cascading_trigger.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{
		path: "mysql/sakila_excerpt.sql", tables: 3, proven: 3,
		// A rental shop's OLTP schema firing TWO warehouse signals. film_actor
		// is a depth-1 star, and last_update is not created_at.
		fired:      []paradigm.Signal{paradigm.SignalNoAuditTimestamps, paradigm.SignalStarTopology},
		paradigmIs: paradigm.ParadigmOLTP,
	},
	{path: "mysql/sakila_real_objects.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{
		path: "pagila_excerpt.sql", tables: 5, proven: 5,
		fired:      []paradigm.Signal{paradigm.SignalNoAuditTimestamps},
		paradigmIs: paradigm.ParadigmOLTP,
	},
	{path: "pagila_real_objects.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "pg_constructed_cascade_trigger.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "pg_constructed_exception_handler.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "pg_constructed_external_call_trigger.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "pg_constructed_n2_recognized_skips.sql", tables: 2, proven: 2, paradigmIs: paradigm.ParadigmOLTP},
	{
		// 7 tables, only 2 proven: five were materialized by CREATE INDEX
		// statements the reducer could not attribute. Both absence-based
		// signals abstain, which is the whole point of the proven-structure
		// guard.
		path: "pg_constructed_unrecognized_index_forms.sql", tables: 7, proven: 2,
		paradigmIs: paradigm.ParadigmOLTP,
	},
	{
		path: "tsql/adventureworks_excerpt.sql", tables: 3, proven: 3,
		// ModifiedDate is not created_at/updated_at.
		fired:      []paradigm.Signal{paradigm.SignalNoAuditTimestamps},
		paradigmIs: paradigm.ParadigmOLTP,
	},
	{path: "tsql/adventureworks_real_objects.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{
		// THE reference warehouse, and the ONLY vendored corpus the gate OPENS.
		// It is the stage-2 decision in miniature: ONE deciding signal
		// (calendar_table, from DimDate's name) beats Sakila's TWO excluded
		// ones. no_audit_timestamps fires alongside it and does NOT vote —
		// which is exactly the point of separating fired from deciding.
		//
		// Re-measured after the T-SQL ALTER TABLE ... ADD CONSTRAINT reducer
		// fix (PR #82) landed on main: all three tables are proven now, and
		// their three primary keys and eight foreign keys are in the model.
		// That moved two rows here — no_audit_timestamps stopped abstaining on
		// unproven tables and now affirms (none of the three declares
		// created_at/updated_at), and the paradigm reads olap, because the A5
		// corroboration gate finally has structure to corroborate the
		// recognized PascalCase names with. star_topology and bulk_load_shape
		// still do not fire, and for NEW reasons worth keeping straight:
		// bulk_load_shape because eight declared foreign keys falsify its
		// no-FKs premise outright, star_topology because six of those eight
		// point at dimension tables this three-table excerpt does not vendor,
		// so its spokes cannot be shown to be leaves. type_profile_split still
		// abstains on the SEPARATE bracketed-type gap (ADR 0036), which that
		// fix was never going to move.
		path: "tsql/adventureworksdw_real_objects.sql", tables: 3, proven: 3,
		fired:      []paradigm.Signal{paradigm.SignalCalendarTable, paradigm.SignalNoAuditTimestamps},
		gateOpen:   true,
		paradigmIs: paradigm.ParadigmOLAP,
	},
	{path: "tsql/constructed_dynamic_sql_proc.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "tsql/constructed_external_call_trigger.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
}

// dialectFor picks the dialect a corpus is written in, from the directory
// convention this testdata tree already uses.
func dialectFor(rel string) sqlddl.Dialect {
	switch {
	case strings.HasPrefix(rel, "mysql/"):
		return sqlddl.MySQL()
	case strings.HasPrefix(rel, "tsql/"):
		return sqlddl.SQLServer()
	default:
		return sqlddl.Postgres()
	}
}

// vendoredCorpora lists every .sql file under testdata/, slash-separated and
// sorted.
func vendoredCorpora(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sql") {
			rel, relErr := filepath.Rel("testdata", path)
			if relErr != nil {
				return relErr
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk testdata: %v", err)
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("found no .sql corpora under testdata — the walk is broken, so any result below is meaningless")
	}
	return out
}

// TestSchemaGate_EveryVendoredCorpusIsMeasured keeps the table above honest: a
// corpus added to testdata without a measured row here fails, instead of
// quietly sitting outside the measurement.
func TestSchemaGate_EveryVendoredCorpusIsMeasured(t *testing.T) {
	measured := map[string]bool{}
	for _, c := range gateCorpusExpectations {
		measured[c.path] = true
	}
	for _, rel := range vendoredCorpora(t) {
		if !measured[rel] {
			t.Errorf("corpus %s has no measured row in gateCorpusExpectations; measure it, do not skip it", rel)
		}
	}
	if got, want := len(gateCorpusExpectations), len(vendoredCorpora(t)); got != want {
		t.Errorf("gateCorpusExpectations has %d rows for %d corpora (a row names a file that no longer exists)", got, want)
	}
}

// TestSchemaGate_SignalsOverVendoredCorpora is the measurement itself, run
// through the real parser on every real corpus.
func TestSchemaGate_SignalsOverVendoredCorpora(t *testing.T) {
	for _, c := range gateCorpusExpectations {
		t.Run(c.path, func(t *testing.T) {
			path := filepath.Join("testdata", filepath.FromSlash(c.path))
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			p := sqlddl.New(sqlddl.WithDialect(dialectFor(c.path)))
			s, err := p.ParseSchema([]providers.SourceFile{{Path: path, Content: content}})
			if err != nil {
				t.Fatalf("ParseSchema: %v", err)
			}

			// Parse facts first: without these, a corpus that stopped parsing
			// would still "pass" its empty signal expectation by vacuity.
			if len(s.Tables) != c.tables {
				t.Fatalf("parsed %d tables, want %d", len(s.Tables), c.tables)
			}
			proven := 0
			for _, tbl := range s.Tables {
				if tbl.StructureProven() {
					proven++
				}
			}
			if proven != c.proven {
				t.Fatalf("%d tables structure-proven, want %d", proven, c.proven)
			}

			e := paradigm.WarehouseSignals(s)
			if !sameSignals(e.Fired, c.fired) {
				t.Errorf("Fired = %v, want %v", e.Fired, c.fired)
			}
			// The VERDICT, asserted separately from the evidence: Sakila fires
			// two signals and is still refused, which is the row that would
			// break first if anyone reverted to counting.
			if got := e.Qualifies(); got != c.gateOpen {
				t.Errorf("Qualifies() = %v, want %v (Fired = %v)", got, c.gateOpen, e.Fired)
			}

			// And the consequence, end to end on real DDL.
			cls := paradigm.Detect(s)
			if cls.Paradigm != c.paradigmIs {
				t.Errorf("Detect().Paradigm = %q, want %q", cls.Paradigm, c.paradigmIs)
			}
			if cls.Gate.Open != c.gateOpen {
				t.Errorf("Detect().Gate.Open = %v, want %v — the verdict must reach the Classification",
					cls.Gate.Open, c.gateOpen)
			}
		})
	}
}

func sameSignals(got, want []paradigm.Signal) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
