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

// The schema gate (internal/core/paradigm, stage 1) MEASURED over every SQL
// corpus vendored in this repository, through the REAL parser — not over
// hand-built db.Schema values, which can hold shapes the parser never produces.
//
// Stage 1 computes five schema-wide warehouse signals and is wired to nothing.
// The point of building it inert is to get THIS table before deciding anything,
// and the table is uncomfortable in a way a hunch would never have been:
//
//	CORPUS                              PARADIGM  SIGNALS THAT FIRED
//	mysql/sakila_excerpt.sql            OLTP      no_audit_timestamps, star_topology
//	pagila_excerpt.sql                  OLTP      no_audit_timestamps
//	tsql/adventureworks_excerpt.sql     OLTP      no_audit_timestamps
//	tsql/adventureworksdw_real_objects  WAREHOUSE calendar_table
//	(every other corpus)                --        none
//
// Read the first and fourth rows together. The ONE genuine Kimball warehouse in
// the repository fires ONE signal; a three-table excerpt of Sakila — a rental
// shop's transactional schema — fires TWO. A naive ">= 2 signals means
// warehouse" threshold, applied today, would classify Sakila as a warehouse and
// AdventureWorksDW as not one. That is the single most useful thing stage 1
// produced, and it is exactly the decision stage 2 must NOT make from intuition.
//
// The three causes behind those numbers, each independently verifiable above:
//
//  1. AdventureWorksDW's structure is entirely UNPROVEN — the T-SQL reducer
//     drops all three shapes of ALTER TABLE ... ADD CONSTRAINT this corpus uses
//     (a pre-existing parser gap, documented in dw_integration_test.go), so its
//     three real primary keys and eight real foreign keys are invisible. Both
//     absence-based signals therefore ABSTAIN rather than affirm, and
//     star_topology has no foreign keys left to see. The gate is behaving
//     exactly as designed here: it refuses to conclude from a model it cannot
//     prove complete. Fix that parser gap and this row should change.
//  2. no_audit_timestamps fires on all three OLTP corpora because they spell
//     their audit stamp last_update (Sakila, Pagila) or ModifiedDate
//     (AdventureWorks), and the signal reuses db052's created_at/updated_at
//     convention rather than inventing a second spelling rule. A real false
//     positive, measured rather than guessed.
//  3. star_topology fires on Sakila because film_actor references actor and
//     film and neither references anything back — a textbook depth-1 star that
//     is in fact a join table. This is the architectural premise restated: no
//     single table, and no single signal, tells a warehouse from a transactional
//     schema.
//
// This file is ALSO the behavioral half of the inertness lock: every row
// asserts what paradigm.Detect returns over the same real parse. The signals
// fire; the classification does not move.

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
	// paradigmWas is what Detect returns — unchanged by the gate's existence.
	paradigmWas paradigm.Paradigm
}

// gateCorpusExpectations covers EVERY .sql file under testdata/. The test fails
// if a corpus is added without being measured here, so the table can never go
// quietly stale.
//
// Corpora with 0 tables are not broken fixtures: the *_real_objects.sql files
// vendor views, procedures and triggers BY DESIGN, and the constructed routine
// fixtures declare no tables at all.
var gateCorpusExpectations = []gateCorpusCase{
	{path: "mysql/constructed_dynamic_sql_proc.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{path: "mysql/constructed_inline_index_using.sql", tables: 2, proven: 2, paradigmWas: paradigm.ParadigmOLTP},
	{path: "mysql/constructed_non_cascading_trigger.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{
		path: "mysql/sakila_excerpt.sql", tables: 3, proven: 3,
		// A rental shop's OLTP schema firing TWO warehouse signals. film_actor
		// is a depth-1 star, and last_update is not created_at.
		fired:       []paradigm.Signal{paradigm.SignalNoAuditTimestamps, paradigm.SignalStarTopology},
		paradigmWas: paradigm.ParadigmOLTP,
	},
	{path: "mysql/sakila_real_objects.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{
		path: "pagila_excerpt.sql", tables: 5, proven: 5,
		fired:       []paradigm.Signal{paradigm.SignalNoAuditTimestamps},
		paradigmWas: paradigm.ParadigmOLTP,
	},
	{path: "pagila_real_objects.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{path: "pg_constructed_cascade_trigger.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{path: "pg_constructed_exception_handler.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{path: "pg_constructed_external_call_trigger.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{path: "pg_constructed_n2_recognized_skips.sql", tables: 2, proven: 2, paradigmWas: paradigm.ParadigmOLTP},
	{
		// 7 tables, only 2 proven: five were materialized by CREATE INDEX
		// statements the reducer could not attribute. Both absence-based
		// signals abstain, which is the whole point of the proven-structure
		// guard.
		path: "pg_constructed_unrecognized_index_forms.sql", tables: 7, proven: 2,
		paradigmWas: paradigm.ParadigmOLTP,
	},
	{
		path: "tsql/adventureworks_excerpt.sql", tables: 3, proven: 3,
		// ModifiedDate is not created_at/updated_at.
		fired:       []paradigm.Signal{paradigm.SignalNoAuditTimestamps},
		paradigmWas: paradigm.ParadigmOLTP,
	},
	{path: "tsql/adventureworks_real_objects.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{
		// THE reference warehouse, and it fires ONE signal — fewer than Sakila.
		// DimDate is recognized by name; everything else abstains because the
		// T-SQL ALTER gap leaves all three tables unproven and key-less.
		path: "tsql/adventureworksdw_real_objects.sql", tables: 3, proven: 0,
		fired:       []paradigm.Signal{paradigm.SignalCalendarTable},
		paradigmWas: paradigm.ParadigmOLTP,
	},
	{path: "tsql/constructed_dynamic_sql_proc.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
	{path: "tsql/constructed_external_call_trigger.sql", tables: 0, proven: 0, paradigmWas: paradigm.ParadigmOLTP},
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

			// INERTNESS, behavioral half: whatever the gate saw above, Detect
			// returns exactly what it returned before the gate existed.
			if got := paradigm.Detect(s).Paradigm; got != c.paradigmWas {
				t.Errorf("Detect().Paradigm = %q, want %q — stage 1 must not move detection", got, c.paradigmWas)
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
