package dbrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// --- DB-011 (prefix-redundant half) — Unit E, Phase 2.2 ---
//
// Closes the gap declared at coverage.go:34 ("Prefix-redundant indexes ([a]
// subsumed by [a,b]) are NOT yet detected — a known gap for a later
// slice."). Same PRD number as the existing exact-duplicate case (the PRD
// has ONE id, DB-011, for "índices duplicados/redundantes" — see
// docs/PRD-codefit-v1.4.md:371), implemented as its OWN Go type / category /
// Check() so the two mechanisms structurally cannot double-report the same
// index pair — see db011prefix.go's doc comment for the full rationale.

func TestDB011Prefix_FiresOnStrictPrefixSubsumedByCompositeIndex(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T",
		Indexes: []db.Index{
			{Columns: []string{"a"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 10}},
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 20}},
		},
	}}}
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBPrefixRedundantIndex)
	if len(got) != 1 {
		t.Fatalf("prefix-redundant index = %d, want 1 ([a] subsumed by [a,b])", len(got))
	}
	if got[0].Line != 10 {
		t.Errorf("must report the SUBSUMED (narrower) index's own line, got %d, want 10", got[0].Line)
	}
	if got[0].StructuralFacts["subsumed_by_primary_key"] {
		t.Error("subsumed by a regular index, not the PK — subsumed_by_primary_key must be false")
	}
	if got[0].StructuralFacts["subsumed_index_is_unique"] {
		t.Error("the subsumed index is not unique — subsumed_index_is_unique must be false")
	}
}

func TestDB011Prefix_UniqueIndexNeverFires(t *testing.T) {
	// A UNIQUE [a] enforces a constraint a wider, non-unique [a,b] does not —
	// dropping it changes semantics. Must NOT fire even though it IS a
	// strict prefix.
	s := &db.Schema{Tables: []db.Table{{
		Name: "T",
		Indexes: []db.Index{
			{Columns: []string{"a"}, Unique: true, Pos: db.Pos{File: "s.sql", Line: 10}},
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 20}},
		},
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBPrefixRedundantIndex); len(got) != 0 {
		t.Errorf("a UNIQUE index must never be reported as prefix-redundant, got %d", len(got))
	}
}

func TestDB011Prefix_SubsumedByPrimaryKey(t *testing.T) {
	// The PK counts as an implicit index (consistent with DB-001, rules.go's
	// indexLike): [a] subsumed by a PK of [a,b] fires, same as under a real
	// composite index.
	s := &db.Schema{Tables: []db.Table{{
		Name:       "T",
		PrimaryKey: []string{"a", "b"},
		Indexes: []db.Index{
			{Columns: []string{"a"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 10}},
		},
	}}}
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBPrefixRedundantIndex)
	if len(got) != 1 {
		t.Fatalf("prefix-redundant index (subsumed by PK) = %d, want 1", len(got))
	}
	if !got[0].StructuralFacts["subsumed_by_primary_key"] {
		t.Error("subsumed_by_primary_key must be true when the subsuming coverer is the PK")
	}
}

func TestDB011Prefix_NonPrefixDistinctColumnsDoesNotFire(t *testing.T) {
	// [b] is NOT a leading prefix of [a,b] (order matters) — must not fire.
	s := &db.Schema{Tables: []db.Table{{
		Name: "T",
		Indexes: []db.Index{
			{Columns: []string{"b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 10}},
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 20}},
		},
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBPrefixRedundantIndex); len(got) != 0 {
		t.Errorf("[b] is not a leading prefix of [a,b], must not fire, got %d", len(got))
	}
}

func TestDB011Prefix_ExactDuplicateNeverOverlapsDB011(t *testing.T) {
	// An EXACT duplicate ([a,b] / [a,b]) is DB-011's exact-duplicate domain
	// (CategoryDBDupIndex) — this rule (CategoryDBPrefixRedundantIndex) must
	// NOT also fire on the same pair. Locks the no-double-report constraint.
	s := &db.Schema{Tables: []db.Table{{
		Name: "T",
		Indexes: []db.Index{
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 10}},
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 20}},
		},
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDupIndex); len(got) != 1 {
		t.Fatalf("exact duplicate must fire DB-011's exact-duplicate category once, got %d", len(got))
	}
	if got := surfaceWithCategory(items, surface.CategoryDBPrefixRedundantIndex); len(got) != 0 {
		t.Errorf("an EXACT duplicate must NOT also fire the prefix-redundant category, got %d", len(got))
	}
}

func TestDB011Prefix_PrefixPairNeverFiresExactDuplicateCategory(t *testing.T) {
	// The converse: a genuine prefix pair must not fire DB-011's exact-
	// duplicate category either — the two categories partition disjoint
	// cases, never overlapping in either direction.
	s := &db.Schema{Tables: []db.Table{{
		Name: "T",
		Indexes: []db.Index{
			{Columns: []string{"a"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 10}},
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 20}},
		},
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDupIndex); len(got) != 0 {
		t.Errorf("a strict prefix pair must NOT fire DB-011's exact-duplicate category, got %d", len(got))
	}
}

func TestDB011Prefix_NeverAffirms(t *testing.T) {
	// Locks ADR 0015: pure surface, never a deterministic Finding, even for
	// the most unambiguous case. PrimaryKey must be set here — an empty PK
	// would additionally (and irrelevantly) fire DB-050, contaminating the
	// "0 findings" assertion this test exists to make.
	s := &db.Schema{Tables: []db.Table{{
		Name:       "T",
		PrimaryKey: []string{"id"},
		Indexes: []db.Index{
			{Columns: []string{"a"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 10}},
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 20}},
		},
	}}}
	findingsOut, items := dbrules.Run(s)
	if len(findingsOut) != 0 {
		t.Fatalf("dbrules.Run should have 0 deterministic findings, got %d: %+v", len(findingsOut), findingsOut)
	}
	if len(surfaceWithCategory(items, surface.CategoryDBPrefixRedundantIndex)) == 0 {
		t.Fatal("sanity check failed: expected the fixture to fire as SURFACE at all")
	}
}

// TestDB011Prefix_ZeroColumnIndexNeverSubsumesOrIsSubsumed is the DB-011b
// mirror of TestDB011_TwoZeroColumnIndexes_NeverFlaggedAsDuplicates
// (index_method_render_test.go, DB-011a) — spec "DB-011b MUST be proven safe
// against zero-column indexes" (sql-ddl-phantom-index). isStrictPrefix
// already guards `len(short) == 0` (db011prefix.go), so this table names a
// real limit, not a fix: a zero-column index (T-SQL's CLUSTERED COLUMNSTORE
// INDEX, applyCreateColumnstoreIndex, reduce.go:2027 — Columns: nil is
// LEGITIMATE, not a parser gap) must never be reported as subsuming another
// index (isStrictPrefix(nil, ...) is never a prefix of anything) or as
// subsumed by one (nothing can be a strict prefix of a nil column list —
// len(long) <= len(short) is true whenever long is empty).
func TestDB011Prefix_ZeroColumnIndexNeverSubsumesOrIsSubsumed(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "FactSales",
		Indexes: []db.Index{
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 10}},
			{Method: "columnstore", Pos: db.Pos{File: "s.sql", Line: 20}}, // Columns: nil, on purpose
		},
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBPrefixRedundantIndex); len(got) != 0 {
		t.Errorf("a zero-column index must never be reported as subsuming or subsumed, got %d: %+v", len(got), got)
	}
}

func TestDB011Prefix_MultipleSubsumedIndexes_EachEmitOwnItem(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T",
		Indexes: []db.Index{
			{Columns: []string{"a"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 10}},
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 15}},
			{Columns: []string{"a", "b"}, Unique: false, Pos: db.Pos{File: "s.sql", Line: 20}},
		},
	}}}
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBPrefixRedundantIndex)
	if len(got) != 1 {
		t.Fatalf("only the ONE narrower [a] index is prefix-redundant, want 1, got %d", len(got))
	}
}
