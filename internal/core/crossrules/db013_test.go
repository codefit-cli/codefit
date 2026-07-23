package crossrules

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/query"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

func run013(s *db.Schema, filters ...query.QueryFilter) []findings.SurfaceItem {
	_, surf := db013{}.Check(s, filters)
	return surf
}

// TestDB013_UncoveredPairEmits — a multi-column filter with no composite index
// covering the pair is surfaced (schema-anchored, surface not finding — ADR 0031).
func TestDB013_UncoveredPairEmits(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3), col("b", 4)))
	s.Tables[0].PrimaryKey = []string{"id"}
	fs, surf := db013{}.Check(s, []query.QueryFilter{filter("Event", "a", "b")})
	if fs != nil {
		t.Errorf("DB-013 must emit no findings (surface only), got %v", fs)
	}
	if len(surf) != 1 {
		t.Fatalf("want 1 surface item, got %d: %+v", len(surf), surf)
	}
	if surf[0].Category != string(surface.CategoryDBNoCompositeIndex) {
		t.Errorf("category = %q, want %q", surf[0].Category, surface.CategoryDBNoCompositeIndex)
	}
}

// TestDB013_LeadingPrefixOfWiderIndexCovers — index [a,b,c], filter (a,b): the
// filter's columns are the leading prefix of a wider index → COVERED, emit nothing.
func TestDB013_LeadingPrefixOfWiderIndexCovers(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3), col("b", 4), col("c", 5)))
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"a", "b", "c"}}}
	if got := run013(s, filter("Event", "a", "b")); len(got) != 0 {
		t.Errorf("filter (a,b) is the leading prefix of index [a,b,c] → covered, got %+v", got)
	}
}

// TestDB013_GapInPrefixNotCovered — index [a,b,c], filter (a,c): a is leading but c
// is NOT in the leading len-2 prefix (b intervenes) → NOT covered, emit.
func TestDB013_GapInPrefixNotCovered(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3), col("b", 4), col("c", 5)))
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"a", "b", "c"}}}
	if got := run013(s, filter("Event", "a", "c")); len(got) != 1 {
		t.Fatalf("filter (a,c) against index [a,b,c]: c is not in the leading 2 → not covered, must emit 1, got %+v", got)
	}
}

// TestDB013_OrderInsensitiveIsTheHeart is the semantic decision (ADR 0031): a
// WHERE a=? AND b=? is order-insensitive, so index [b,a] covers filter (a,b) — both
// filtered columns are the index's leading SET. Ordered-prefix coverage would wrongly
// emit here; DB-013 uses SET-of-leading-columns coverage. This is the case that
// distinguishes the two.
func TestDB013_OrderInsensitiveIsTheHeart(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3), col("b", 4)))
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"b", "a"}}}
	if got := run013(s, filter("Event", "a", "b")); len(got) != 0 {
		t.Errorf("index [b,a] covers filter (a,b) — same leading set, order-insensitive → emit nothing, got %+v", got)
	}
}

// TestDB013_ShorterIndexCannotCover — index [a], filter (a,b): the index is shorter
// than the pair, so it cannot cover both → emit. This is the DB-010/DB-013 overlap
// case seen from DB-013's side (DB-010 defers the whole filter here).
func TestDB013_ShorterIndexCannotCover(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3), col("b", 4)))
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"a"}}}
	if got := run013(s, filter("Event", "a", "b")); len(got) != 1 {
		t.Fatalf("index [a] cannot cover the pair (a,b) → emit 1, got %+v", got)
	}
}

// TestDB013_SingleColumnFilterIsDB010s — DB-013 handles only multi-column filters;
// a single-column filter is DB-010's and DB-013 emits nothing for it.
func TestDB013_SingleColumnFilterIsDB010s(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3)))
	s.Tables[0].PrimaryKey = []string{"id"}
	if got := run013(s, filter("Event", "a")); len(got) != 0 {
		t.Errorf("a single-column filter is DB-010's — DB-013 must emit nothing, got %+v", got)
	}
}

// TestDB013_AbstainsOnInexactMatch — reconcile abstaining (unknown model, a column
// that is not real) emits nothing (Complete==false floor, ADR 0029).
func TestDB013_AbstainsOnInexactMatch(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3)))
	if got := run013(s, filter("Ghost", "a", "b")); len(got) != 0 {
		t.Errorf("unknown model → abstain, got %+v", got)
	}
	// (a, ghost): ghost is dropped by reconcile → only 1 real column → DB-010's, not DB-013's.
	if got := run013(s, filter("Event", "a", "ghost")); len(got) != 0 {
		t.Errorf("a filter that reconciles to <2 real columns is not DB-013's, got %+v", got)
	}
}

// TestDB013_RangePredicateIsAKnownLimit locks the range caveat as a DECLARED LIMIT
// (ADR 0031), the same treatment as OR/NOT and cross-naming-space — not prose.
//
// Set-cover is correct ONLY when every predicate is equality. With `WHERE b>2 AND
// a=1`, the range on b cuts the index there: an index [b,a] can no longer seek a,
// so it does NOT actually cover the pair — yet set-cover reports "covered". The
// root cause: prismaWhereColumns extracts column NAMES, not operators, so DB-013
// receives `where:{b:x}` and `where:{b:{gt:x}}` identically and cannot tell this is
// the case where its semantics do not apply. This test pins the current behavior
// (index [b,a], filter (a,b) — which MIGHT be a range on b — emits 0) as KNOWN, not
// a latent bug. The error direction is safe: set-cover is more permissive than
// ordered-cover → false NEGATIVES (a missed suggestion), never false positives (a
// wrong one) — the Complete==false floor holds. Resolving it needs the operator in
// the neutral QueryFilter, which is a separate slice.
func TestDB013_RangePredicateIsAKnownLimit(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3), col("b", 4)))
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"b", "a"}}}
	// The filter carries no operator — if b were a range predicate, [b,a] would NOT
	// cover (a,b), but DB-013 cannot see that and reports covered (emits 0).
	if got := run013(s, filter("Event", "a", "b")); len(got) != 0 {
		t.Fatalf("known limit: with no operator captured, DB-013 treats the filter as equality and "+
			"considers [b,a] covering (a,b) → emits 0; got %+v", got)
	}
}

// TestDB013_DedupByColumnSet — two queries filtering the same column set (in any
// order) yield ONE item (one @@index fixes both).
func TestDB013_DedupByColumnSet(t *testing.T) {
	s := schema(tbl("Event", col("id", 2), col("a", 3), col("b", 4)))
	s.Tables[0].PrimaryKey = []string{"id"}
	got := run013(s, filter("Event", "a", "b"), filter("Event", "b", "a"))
	if len(got) != 1 {
		t.Errorf("two filters over the same column set → 1 deduped item, got %+v", got)
	}
}

// TestDB013_UniqueSubsetShortCircuit fixes FIX 1 (ADR 0032) with the CRUDE dogfood
// cases: a filter that constrains a unique key (PK or @@unique subset) resolves to
// ≤1 row → no composite index missing → emit 0.
func TestDB013_UniqueSubsetShortCircuit(t *testing.T) {
	// User: (id, salonId), id is the PK → the tenant-scoped-lookup false positive.
	su := schema(tbl("User", col("id", 2), col("salonId", 3)))
	su.Tables[0].PrimaryKey = []string{"id"}
	if got := run013(su, filter("User", "id", "salonId")); len(got) != 0 {
		t.Fatalf("(id, salonId) with id=PK is a ≤1-row lookup → emit 0, got %+v", got)
	}
	// Transaction: (deletedAt, id, salonId) — 3 cols, id is PK → 0 (FIX 1, before arity).
	st := schema(tbl("Transaction", col("id", 2), col("deletedAt", 3), col("salonId", 4)))
	st.Tables[0].PrimaryKey = []string{"id"}
	if got := run013(st, filter("Transaction", "deletedAt", "id", "salonId")); len(got) != 0 {
		t.Fatalf("(deletedAt, id, salonId) with id=PK → 0, got %+v", got)
	}
	// @@unique([a,b]) + filter (a,b,c): the unique key is a subset → 0.
	sm := schema(tbl("M", col("a", 2), col("b", 3), col("c", 4)))
	sm.Tables[0].Indexes = []db.Index{{Columns: []string{"a", "b"}, Unique: true}}
	if got := run013(sm, filter("M", "a", "b", "c")); len(got) != 0 {
		t.Fatalf("@@unique([a,b]) ⊆ (a,b,c) → 0, got %+v", got)
	}
}

// TestDB013_UniqueSubsetNegativeControl — do NOT over-kill: @@unique([a,b]) is NOT a
// subset of (a,c), so the filter is still uncovered and emits.
func TestDB013_UniqueSubsetNegativeControl(t *testing.T) {
	s := schema(tbl("M", col("a", 2), col("b", 3), col("c", 4)))
	s.Tables[0].Indexes = []db.Index{{Columns: []string{"a", "b"}, Unique: true}}
	if got := run013(s, filter("M", "a", "c")); len(got) != 1 {
		t.Fatalf("@@unique([a,b]) ⊄ (a,c) → the filter is uncovered, emit 1, got %+v", got)
	}
}

// TestDB013_HighArityAbstains fixes FIX 3 (ADR 0032): a filter of 4+ real columns is
// an abstention (which subset to index depends on selectivity codefit cannot see);
// 2 and 3 columns still emit. No PK/unique here, so FIX 1 does not interfere.
func TestDB013_HighArityAbstains(t *testing.T) {
	s4 := schema(tbl("M", col("a", 2), col("b", 3), col("c", 4), col("d", 5)))
	if got := run013(s4, filter("M", "a", "b", "c", "d")); len(got) != 0 {
		t.Fatalf("4-column filter → abstain, got %+v", got)
	}
	s3 := schema(tbl("N", col("a", 2), col("b", 3), col("c", 4)))
	if got := run013(s3, filter("N", "a", "b", "c")); len(got) != 1 {
		t.Fatalf("3-column filter → emit 1, got %+v", got)
	}
	s2 := schema(tbl("O", col("a", 2), col("b", 3)))
	if got := run013(s2, filter("O", "a", "b")); len(got) != 1 {
		t.Fatalf("2-column filter → emit 1, got %+v", got)
	}
}

// TestDB013_GroupsSameSetAcrossTables fixes FIX 4 (ADR 0032): the SAME uncovered
// column set across different models is ONE grouped item listing all affected
// models — an architectural observation (tenant scoping + soft-delete), not N
// findings. Grouped, NOT suppressed: every model is named.
func TestDB013_GroupsSameSetAcrossTables(t *testing.T) {
	a := tbl("Supplier", col("id", 2), col("salonId", 3), col("deletedAt", 4))
	a.PrimaryKey = []string{"id"}
	b := tbl("Service", col("id", 2), col("salonId", 3), col("deletedAt", 4))
	b.PrimaryKey = []string{"id"}
	s := schema(a, b)
	got := run013(s, filter("Supplier", "salonId", "deletedAt"), filter("Service", "salonId", "deletedAt"))
	if len(got) != 1 {
		t.Fatalf("same uncovered set across 2 models → 1 grouped item, got %d: %+v", len(got), got)
	}
	joined := strings.Join(got[0].StructuralSignals, " | ")
	if !strings.Contains(joined, "Service") || !strings.Contains(joined, "Supplier") {
		t.Errorf("grouped item must list BOTH models (grouped, not suppressed), got %+v", got[0].StructuralSignals)
	}
}

// TestDB013_DistinctSetsNotGrouped — different column sets stay separate items.
func TestDB013_DistinctSetsNotGrouped(t *testing.T) {
	a := tbl("A", col("id", 2), col("x", 3), col("y", 4))
	a.PrimaryKey = []string{"id"}
	b := tbl("B", col("id", 2), col("p", 3), col("q", 4))
	b.PrimaryKey = []string{"id"}
	s := schema(a, b)
	if got := run013(s, filter("A", "x", "y"), filter("B", "p", "q")); len(got) != 2 {
		t.Fatalf("distinct sets → 2 separate items, got %d: %+v", len(got), got)
	}
}
