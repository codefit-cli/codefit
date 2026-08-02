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
//	mysql/sakila_excerpt.sql            OLTP      star_topology
//	pagila_excerpt.sql                  OLTP      (none)
//	tsql/adventureworks_excerpt.sql     OLTP      (none)
//	tsql/adventureworksdw_real_objects  WAREHOUSE calendar_table, no_audit_timestamps
//	(every other corpus)                --        none
//
// THE FIRST THREE ROWS MOVED when the audit-timestamp vocabulary was widened
// and shared (db.IsAuditTimestampName). They read
//
//	mysql/sakila_excerpt.sql            OLTP      no_audit_timestamps, star_topology
//	pagila_excerpt.sql                  OLTP      no_audit_timestamps
//	tsql/adventureworks_excerpt.sql     OLTP      no_audit_timestamps
//
// and the reason they no longer do is cause #2 below, inverted: the signal used
// to fire on all three because they spell their stamp last_update or
// ModifiedDate, which the gate did not recognize. It does now, and a
// transactional schema that DOES stamp its rows stops looking like a warehouse
// for a reason that was never true. No VERDICT moved — no_audit_timestamps
// votes on nothing (decidingSignals) — and that was measured over 29 corpora,
// not assumed.
//
// WHAT THAT COSTS THE ARGUMENT BELOW, stated rather than quietly dropped: the
// old table had Sakila and the reference warehouse firing the SAME NUMBER of
// signals, two each, which made "count the signals" unrankable at any cutoff.
// That particular pair no longer says so — Sakila fires one now, the warehouse
// two, and a >= 2 threshold would separate THESE TWO correctly. The stage-2
// decision does not rest on this pair; it rests on the 26-corpus measurement in
// ADR 0036, re-run after the widening: no_audit_timestamps went from 9 W / 5 O
// to 8 W / 3 O — quieter, still fired by three transactional corpora, and
// therefore still incapable of being a deciding signal, whose bar is ZERO
// transactional fires. WHICH signals fired remains the thing that separates a
// warehouse from a rental shop; the widening made this corpus a weaker
// ILLUSTRATION of it and a more honest measurement.
//
// HOW THIS TABLE WAS READ WHEN THE DECISION WAS MADE, kept because the readings
// are a record and not a claim about today: at stage 1 (ADR 0035) the warehouse
// fired ONE signal and Sakila TWO, so a >= 2 threshold got both backwards. The
// T-SQL ALTER TABLE ... ADD CONSTRAINT reducer fix (PR #82) then landed on main
// and proved the warehouse's three tables, which let no_audit_timestamps stop
// abstaining and moved that row from one signal to two — "the threshold cannot
// rank them at all". The audit-vocabulary widening is the third reading, above.
// All three are measured, none is a hunch, and stage 2 selects rather than
// counts under every one of them.
//
// STAGE 2 (ADR 0037) decided from that shape, plus the 26-corpus measurement in
// ADR 0036: the verdict SELECTS the three zero-false-positive signals and
// requires ANY ONE of them, rather than counting all six. This table is what
// that decision looks like on real vendored DDL — Sakila's one remaining signal
// is one that does NOT vote, so it stays transactional, while
// AdventureWorksDW's calendar_table opens the gate and the no_audit_timestamps
// beside it is worth no votes to anyone. The gateOpen column below records it,
// corpus by corpus.
//
// The three causes behind those numbers, each independently verifiable above:
//
//  1. AdventureWorksDW's structure is fully PROVEN as of the reducer fix above:
//     its three real primary keys and eight real foreign keys are in the model,
//     so the absence-based signals judge it instead of abstaining. That is why
//     no_audit_timestamps affirms here (none of its three tables declares any
//     recognized audit stamp) while bulk_load_shape does NOT: eight declared
//     foreign keys falsify its no-FKs premise outright, rather than leaving it
//     unable to conclude. star_topology also stays silent, for a third reason
//     again — six of those eight foreign keys point at dimension tables this
//     three-table excerpt does not vendor, and an absent spoke can never be
//     shown to be a leaf. Before the fix, ALL of these abstained on unproven
//     structure, which was correct behavior over a model the parser could not
//     prove complete and is why ADR 0035 named that fix a stage-2 prerequisite.
//  2. no_audit_timestamps fires on NONE of the three OLTP corpora, and used to
//     fire on all three. They spell their audit stamp last_update (Sakila,
//     Pagila) or ModifiedDate (AdventureWorks); the signal reuses DB-052's
//     vocabulary rather than inventing a second spelling rule, and that shared
//     vocabulary is now the measured one (db.IsAuditTimestampName), so a schema
//     that stamps its rows no longer reads as one that does not. What is left
//     here is the AdventureWorksDW row: a real warehouse whose three tables
//     genuinely carry no row-level stamp. The signal still fires on both sides
//     of the question over the full corpus set (8 W / 3 O), which is why it
//     still does not vote — it is simply no longer wrong about these three.
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
//   - the reference warehouse used to abstain on a SECOND, independent parser
//     gap that the ALTER TABLE fix was never going to touch, exactly as ADR
//     0036 predicted: AdventureWorksDW delimits its type names ([int],
//     [nvarchar](50)) and all 74 of its parsed columns landed on
//     db.TypeUnknown. THAT GAP IS NOW CLOSED (tsql-bracketed-type-names): all
//     74 classify, and the signal reaches the corpus for the first time. It
//     still does not FIRE here, and for an honest arithmetic reason rather
//     than an abstention — this 3-table excerpt profiles one numeric pole
//     (FactInternetSales) but only ONE text-dominated table (DimCustomer;
//     DimDate is 63% numeric), and textPoleMinTables is 2. Measured on the
//     FULL upstream install script, which is not vendored here, the signal
//     DOES fire. The abstention mechanism itself is now locked with a fixture
//     that produces genuinely unclassified types, in
//     TestSchemaGate_TypeProfileSplit_UnclassifiedBudget.
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
		// A rental shop's OLTP schema firing ONE warehouse signal: film_actor
		// is a depth-1 star. It fired TWO until the audit-timestamp vocabulary
		// learned last_update — actor and film_actor carry one (film does not,
		// which is why DB-052 still speaks about film), and ONE stamped table
		// is all it takes to falsify "not one table in this schema stamps its
		// rows". The second signal was measuring a spelling, not a schema.
		fired:      []paradigm.Signal{paradigm.SignalStarTopology},
		paradigmIs: paradigm.ParadigmOLTP,
	},
	{path: "mysql/sakila_real_objects.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{
		// NO signal fires, where no_audit_timestamps used to: four of these five
		// tables (actor, customer, address, rental) carry last_update. The fifth,
		// the payment_p2022_01 partition, does not — which is why DB-052 still
		// has something to say here while this schema-wide signal does not.
		// "Not ONE table stamps its rows" is a different claim from "this table
		// does not".
		path: "pagila_excerpt.sql", tables: 5, proven: 5,
		paradigmIs: paradigm.ParadigmOLTP,
	},
	{path: "pagila_real_objects.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "pg_constructed_cascade_trigger.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "pg_constructed_exception_handler.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "pg_constructed_external_call_trigger.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{path: "pg_constructed_n2_recognized_skips.sql", tables: 2, proven: 2, paradigmIs: paradigm.ParadigmOLTP},
	{
		// The pg_dump sequence fixture (ADR 0045). The PARSE FACTS carry this
		// row and are exactly what regressed before: it declares TWO sequences
		// and TWO tables, and the sequences used to become tables — so this row
		// measured 4/2 (two phantom entries, neither proven) before the
		// non-table relation registry. If tables ever returns to 4, a sequence
		// is back in the model; if proven drops below 2, a real table lost its
		// structure.
		//
		// No signal fires and the gate is not even evaluated: two tables is
		// below minJudgeableTables = 3 (no vacuous truths). That is not this
		// fixture avoiding the question — it is the same floor the temporary-
		// table fixture sits under, and it is worth recording that REMOVING
		// phantom relations moves a small schema DOWN across that floor exactly
		// as withholding does.
		path: "pg_constructed_pgdump_sequences.sql", tables: 2, proven: 2,
		paradigmIs: paradigm.ParadigmOLTP,
	},
	{
		// The table-shaped-head fixture (ADR 0043). The PARSE FACTS carry this
		// row: it declares TEN table-shaped statements and exactly THREE of
		// them are persistent tables (keeper, and the two UNLOGGED ones). If
		// tables ever returns to 1, the UNLOGGED admission has regressed; if it
		// climbs above 3, a session-scoped table has been let into the model.
		//
		// The signal row is unremarkable on purpose and was measured, not
		// assumed: no table declares created_at/updated_at, so the excluded
		// no_audit_timestamps signal fires and nothing else can. bulk_load_shape
		// needs four key-like columns in ONE table and eight schema-wide (there
		// is one per table, three in all); calendar_table, surrogate_key_names
		// and star_topology have no date dimension, no _sk columns and no
		// foreign keys to read. The gate stays shut and the paradigm reads oltp.
		path: "pg_constructed_table_shaped_heads.sql", tables: 3, proven: 3,
		fired:      []paradigm.Signal{paradigm.SignalNoAuditTimestamps},
		paradigmIs: paradigm.ParadigmOLTP,
	},
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
		// ModifiedDate IS an audit stamp, and the vocabulary now says so — one
		// of these three tables (Sales.Customer) carries it, which is all this
		// schema-wide signal needs to stop affirming.
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
		// so its spokes cannot be shown to be leaves.
		//
		// Re-measured a SECOND time after tsql-bracketed-type-names: this row
		// did NOT move, and the reason changed underneath it. All 74 columns
		// now classify (they read as db.TypeUnknown before), so
		// type_profile_split no longer abstains — it is EVALUATED here and
		// returns false on the arithmetic: one numeric pole (FactInternetSales,
		// 20 of 26 numeric) but only ONE text-dominated table (DimCustomer, 20
		// of 29 descriptive; DimDate is 12 of 19 numeric and qualifies as
		// neither), against textPoleMinTables = 2. Same fired set, an honest
		// verdict instead of a fail-closed one.
		path: "tsql/adventureworksdw_real_objects.sql", tables: 3, proven: 3,
		fired:      []paradigm.Signal{paradigm.SignalCalendarTable, paradigm.SignalNoAuditTimestamps},
		gateOpen:   true,
		paradigmIs: paradigm.ParadigmOLAP,
	},
	{path: "tsql/constructed_dynamic_sql_proc.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{
		// The missing-comma fixture (ADR 0042): Fact_Reservation declares a
		// four-column PRIMARY KEY on the line after its last column, with no
		// separating comma. The PARSE FACTS are not what carries this row —
		// tables and proven read 4/4 before the boundary rule too, because the
		// defect FABRICATED rather than dropped; what moved is the key itself,
		// asserted in TestSQLDDL_MissingComma_RealFixture.
		//
		// The signals were MEASURED, not assumed, and the row is worth reading:
		// three fire, and the gate opens on calendar_table (Dim_Date). This is
		// the second vendored corpus the gate opens on, and unlike
		// AdventureWorksDW it also fires star_topology — the recovered composite
		// key is not what does that (star_topology reads foreign keys), the
		// four inline T-SQL references from Fact_Reservation to three leaf
		// dimensions are.
		path: "tsql/constructed_missing_comma_table_constraint.sql", tables: 4, proven: 4,
		fired: []paradigm.Signal{
			paradigm.SignalCalendarTable,
			paradigm.SignalNoAuditTimestamps,
			paradigm.SignalStarTopology,
		},
		gateOpen:   true,
		paradigmIs: paradigm.ParadigmOLAP,
	},
	{path: "tsql/constructed_external_call_trigger.sql", tables: 0, proven: 0, paradigmIs: paradigm.ParadigmOLTP},
	{
		// The T-SQL temporary-table fixture (ADR 0043): three CREATE TABLE
		// statements, of which only dbo.Keeper is persistent — #tmpHoliday and
		// ##GlobalScratch are session-scoped and withheld. tables: 1 is the
		// assertion that matters, and it is precisely an ADMISSION lock: it
		// catches scratch space ENTERING the model (mutation-proven by widening
		// reCreateTable's name class to admit '#', which takes it to 3 and where
		// DB-050 would then affirm over it). It does NOT catch the withholding
		// branch being removed — the table-shaped-head floor keeps those
		// statements out of the model either way, and it is
		// withheld_table_test.go, not this row, that tells the two apart.
		//
		// NO signal fires, and the reason is a real consequence of withholding
		// worth recording rather than a property of this fixture: the gate
		// declines to evaluate ANY signal below minJudgeableTables = 3 (no
		// vacuous truths), and withholding the two temporary tables leaves one
		// modeled table. That is the correct reading — session scratch space is
		// not evidence about whether a schema is a warehouse — but it does mean
		// withholding can move a small schema below the judgeable floor.
		path: "tsql/constructed_temp_table_names.sql", tables: 1, proven: 1,
		paradigmIs: paradigm.ParadigmOLTP,
	},
	{
		// The run-on fixture (ADR 0041): four CREATE TABLE statements with no
		// ';' and no batch separator between them. The PARSE FACTS are the
		// whole reason this row matters — this corpus measured tables: 1,
		// proven: 1 before run-on separation, with the other three tables gone
		// without a trace. If either number ever returns to 1 the separation
		// has regressed, and this row fails before any signal expectation gets
		// the chance to pass by vacuity.
		//
		// The signal row is unremarkable ON PURPOSE and was measured, not
		// assumed: Dim_/Fact_ names alone move nothing here. calendar_table
		// does not fire (no date dimension is declared), star_topology does not
		// (Dim_Car references Dim_Price_Table, so the star's spokes are not all
		// leaves), and the one signal that does fire — no_audit_timestamps — is
		// one of the two excluded from the vote (ADR 0037), so the gate stays
		// shut and the paradigm reads oltp.
		path: "tsql/constructed_runon_no_delimiter.sql", tables: 4, proven: 4,
		fired:      []paradigm.Signal{paradigm.SignalNoAuditTimestamps},
		paradigmIs: paradigm.ParadigmOLTP,
	},
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
