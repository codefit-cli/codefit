package dwrules

import (
	"strconv"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dw020 — a CENSUS of this schema's fact tables for declared TABLE
// partitioning. A large fact table that is never partitioned makes every
// retention delete a full-table scan, every stale-period query read the whole
// history, and every backfill rewrite rows nobody asked about. Whether a
// given fact table is large enough for that to matter is precisely what
// codefit cannot see.
//
// # SCHEMA-LEVEL: at most ONE item, never one per table
//
// This is the rule's defining decision, and it was made from a measurement,
// not from taste: across a 26-corpus survey of public schemas, NO analytic
// corpus declares table partitioning at all. A per-table rule would therefore
// fire on ~100% of fact tables in ~100% of warehouses — zero discrimination,
// which is the same defect that makes DB-052 the worst-measured rule in this
// project (318 items across 21 of 22 corpora). One item per schema carries
// the same information at 1/N the noise, and the agent reasons once instead
// of N times.
//
// DW-005 and DW-011 are the direct precedent: both are schema-level census
// judgments, and this rule follows their idiom rather than inventing one.
// It anchors on the FIRST unpartitioned census member in schema order (the
// item is about those tables) and names BOTH groups in its signals, the way
// DW-011 names both SCD groups.
//
// The lists are rendered UNCAPPED, exactly like DW-005's and DW-011's — and
// unlike DW-021's, which caps because a single table can carry hundreds of
// indexes. The one thing that could balloon this item is a partitioned fact
// table's children (a five-year monthly range is 60 tables), and those are
// excluded from the census by construction, below. What remains is the
// schema's fact-table count, which is small in every warehouse this project
// has measured.
//
// # WHEN IT FIRES
//
// When AT LEAST ONE census member declares no partitioning. That includes the
// MIXED case — some fact tables partitioned, others not — and that inclusion
// is deliberate, for two reasons:
//
//	An inconsistency is MORE interesting than a uniform absence, not less.
//	A team that partitioned fact_sales knows how; that fact_returns is not
//	partitioned is then a decision or an oversight, and the agent has real
//	evidence to tell them apart.
//
//	Suppressing on a partitioned sibling would be a counter-signal that
//	SILENCES — the exact shape ADR 0017 forbids (lever 2: a counter-signal is
//	exposed as a fact, never used to suppress). One partitioned fact table
//	would otherwise mute the question for every other fact table in the
//	schema, which is a silent false negative.
//
// So the mixed case is REPORTED, and the counter-signal is exposed instead:
// partitioned_fact_tables lists them and some_fact_tables_partitioned is
// true, so an agent can dismiss the item in one read when the split is
// deliberate.
//
// # SURFACE, never an affirmation (ADR 0017)
//
// Whether partitioning matters depends on the table's real row count, growth
// rate and retention policy — RUNTIME facts, absent from static DDL. codefit
// cannot know them. The AGENT can reason about the domain: a click-tracking
// fact will be enormous; a monthly budget snapshot will not. So this rule
// hands over the census and the question, never a verdict.
//
// # PARTITION CHILDREN ARE NOT UNPARTITIONED FACT TABLES
//
// A PostgreSQL fact table partitioned into 60 monthly children yields 60
// EXTRA tables in the neutral model, each carrying Partitioning.Of. That
// back-reference exists precisely so a rule can tell them apart from real
// fact tables, and every child is excluded from the census here
// (isPartitionChild).
//
// This is not hypothetical: measured through the real parser and the real
// classifier, a child that declares its own foreign keys (which PostgreSQL 10
// and earlier REQUIRED, since a partitioned parent could not carry them)
// comes out FACT-ROLE. It would have been censused.
//
// WHICH SIDE it would have inflated depends on the form, and both are wrong —
// a partition is not a fact table:
//
//	CREATE TABLE c PARTITION OF p — the child's own Declaration is
//	non-empty, so counting it would report ONE partitioned fact table as 61,
//	drowning the census in restatements of a single fact. Measured by
//	mutation: dropping the exclusion puts fact_sales_2024_01 in
//	partitioned_fact_tables.
//
//	A child the parser reads with no partition grammar of its own would land
//	on the UNPARTITIONED side instead — reporting a correctly partitioned
//	warehouse as N unpartitioned fact tables, the exact inversion of the
//	truth. See the declared limit below: that form carries an empty Of and
//	this rule cannot exclude it at all.
//
// DECLARED LIMIT, because Of is not a complete answer: a child attached by
// ALTER TABLE ... ATTACH PARTITION, or dumped as a standalone CREATE TABLE
// with no partition grammar of its own — which is what pg_dump actually emits
// — carries an EMPTY Of and is indistinguishable here from an ordinary table.
// Such a child, if it earns a fact role, IS censused as unpartitioned. That
// limit lives in the model (see db.Partitioning's type doc) and cannot be
// closed by this rule; it is stated in the coverage manifest rather than
// papered over.
//
// # COMPLETENESS: SCHEMA-LEVEL ABSTENTION, WITH THE CHILDREN EXEMPT
//
// D4 (design SS4) / ADR 0034 §2.5: a census judgment abstains as a WHOLE when
// any census member is unproven. A per-table skip would silently SHRINK the
// census and still emit an item that looks authoritative over an undercounted
// schema — a worse lie than saying nothing.
//
// The gate reads the SAME census predicate, and that is load-bearing rather
// than tidy. A PARTITION OF child is marked unproven BY CONSTRUCTION
// (db.ReasonPartitionChildInheritsStructure — its columns and keys are
// declared on the parent, not in its own statement), with no parser failure
// involved. Gating on it would make DW-020 abstain on every declaratively
// partitioned PostgreSQL warehouse — that is, structurally incapable of ever
// observing its own positive case. A table excluded from the census cannot
// abstain the census.
//
// # THE SCHEMA GATE (ADR 0037)
//
// No behavior of its own is needed, and that is the deliberate part. Roles
// come from cls and are never re-derived (locked decision A5), so a CLOSED
// gate leaves every table RoleUnclassified, the census comes out empty, and
// the rule says nothing. Partitioning is a WAREHOUSE question; asking it of a
// schema codefit did not judge to be a warehouse would re-open the exact hole
// ADR 0037 closed. Test-locked, not assumed.
//
// DECLARED CONSEQUENCE of reading roles rather than partitioning: a
// partitioned parent whose foreign keys live on its CHILDREN (the PostgreSQL
// 10 pattern) gets no fan-out of its own, so paradigm demotes it to
// unclassified and it never enters the census at all — the schema's
// partitioning is then invisible to this rule. That is the A5 corroboration
// gate doing its job, not a DW-020 bug, and widening it here would fork the
// role heuristic.
type dw020 struct{}

func (dw020) ID() string { return "DW-020" }

func (dw020) Check(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	for _, t := range s.Tables {
		if inPartitioningCensus(t, cls) && !t.StructureProven() {
			return nil, nil
		}
	}

	var partitioned, unpartitioned []string
	var anchor db.Pos
	children := 0
	for _, t := range s.Tables {
		if isPartitionChild(t) {
			children++
		}
		if !inPartitioningCensus(t, cls) {
			continue
		}
		if t.Partitioning.Declaration != "" {
			partitioned = append(partitioned, t.Name)
			continue
		}
		if len(unpartitioned) == 0 {
			anchor = t.Pos
		}
		unpartitioned = append(unpartitioned, t.Name)
	}

	if len(unpartitioned) == 0 {
		return nil, nil
	}

	return nil, []findings.SurfaceItem{{
		Category: string(surface.CategoryDWFactsNotPartitioned),
		File:     anchor.File,
		Line:     anchor.Line,
		StructuralSignals: []string{
			"unpartitioned_fact_tables: " + describeRefs(unpartitioned),
			"partitioned_fact_tables: " + describeRefs(partitioned),
			"partition_children_declared: " + strconv.Itoa(children),
		},
		StructuralFacts: map[string]bool{
			"some_fact_tables_partitioned": len(partitioned) > 0,
			"partition_children_declared":  children > 0,
		},
		ReasonToReview: partitioningReason(unpartitioned, partitioned),
	}}
}

// isPartitionChild reports whether the SOURCE declares t as a partition of
// another table. db.Partitioning.Of is the model's own back-reference; an
// empty Of is NOT proof that a table is not a partition (see dw020's doc
// comment and db.Partitioning's).
func isPartitionChild(t db.Table) bool { return t.Partitioning.Of != "" }

// inPartitioningCensus reports whether t is a member of DW-020's census: a
// FACT-role table (role read from cls, never re-derived) that the source does
// not declare as a partition child. Both the completeness gate and the census
// loop consult this ONE predicate, so a table can never be censused without
// being gated, nor gated without being censused — the divergence that would
// let a child's by-construction unprovenness abstain a rule the child is not
// even part of.
func inPartitioningCensus(t db.Table, cls *paradigm.Classification) bool {
	return cls.Roles[t.Name] == paradigm.RoleFact && !isPartitionChild(t)
}

// partitioningReason renders the agent-facing question. It states the census,
// exposes the partitioned counter-signal as a FACT rather than a dismissal,
// and hands over the judgment codefit cannot make — the row counts and growth
// rates that decide whether partitioning is worth anything here are runtime
// facts no static DDL carries.
func partitioningReason(unpartitioned, partitioned []string) string {
	b := strings.Builder{}
	b.WriteString("This schema's fact tables were censused for declared table partitioning. Declaring none: ")
	b.WriteString(strings.Join(unpartitioned, ", "))
	b.WriteString(". ")
	if len(partitioned) > 0 {
		b.WriteString("Already partitioned: ")
		b.WriteString(strings.Join(partitioned, ", "))
		b.WriteString(" — so this schema partitions some fact tables and not others. Is that split deliberate? ")
	}
	b.WriteString("Whether partitioning is worth anything here depends on each table's real row count, growth ")
	b.WriteString("rate and retention policy, which are runtime facts absent from static DDL — codefit cannot ")
	b.WriteString("see them, and does not guess. Which of these fact tables is (or will become) large enough ")
	b.WriteString("that retention deletes, period-scoped scans and backfills would benefit from partitioning?")
	return b.String()
}
