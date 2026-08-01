package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-011a (exact-duplicate half) real-fixture dogfood on PostgreSQL/Pagila —
// architecture/pagila-fixture-real-indexes.
//
// Unlike MySQL/T-SQL (whose DB-011a positive path is proven only at the unit
// level with a synthetic schema — TestDBRules_NoCoreEnrichment_MySQLSakila
// and its T-SQL sibling assert the CLEAN NEGATIVE, since neither real corpus
// happens to ship a duplicate index), real upstream Pagila genuinely ships
// an exact-duplicate index pair: on the payment_p2022_01 partition-child
// table, "idx_fk_payment_p2022_01_customer_id" and
// "payment_p2022_01_customer_id_idx" are BOTH plain (non-unique) indexes on
// the identical single-column list [customer_id] — no mutation needed, this
// is a genuine antipattern Pagila's own maintainers shipped upstream.
//
// Parser-gap note (session rule; reported, NOT absorbed): upstream Pagila
// declares payment_p2022_01 as a partition of "payment" (PARTITION BY RANGE
// on the parent, ATTACHed via a separate "ALTER TABLE ONLY ... ATTACH
// PARTITION" statement) — but pg_dump's own on-disk form of a partition
// CHILD table is an entirely ordinary, standalone CREATE TABLE, with no
// "PARTITION OF" grammar in its own statement at all. Verified directly
// (throwaway parse, not assumed): AS OF THIS FIXTURE'S VENDORING codefit's
// sqlddl parser did NOT handle the literal "CREATE TABLE x PARTITION OF y
// FOR VALUES ..." grammar form at all — the whole table vanished from the
// model. That has since CHANGED (partition-capture): the child is now
// registered as its own table, naming its parent in Partitioning.Of and
// marked structurally unproven, because that statement still declares no
// columns. The parser separately does NOT handle
// PostgreSQL's "ALTER TABLE ONLY <table> ..." idiom used for the ATTACH
// PARTITION statement and for every real pg_dump PRIMARY KEY/FOREIGN KEY
// constraint (mis-parses the literal token "ONLY" as the table name,
// silently dropping the real constraint into a phantom "only" table — this
// is why actor and every table this fixture vendors stay PK-free: their
// real PKs are declared exactly this way upstream and are deliberately NOT
// vendored). Neither limit is touched by this fixture: the vendored
// payment_p2022_01 excerpt below is JUST its real, standalone CREATE TABLE
// plus its two real CREATE INDEX statements — no PARTITION OF, no ATTACH
// PARTITION, no ALTER TABLE ONLY anywhere in the vendored text.
func TestDB011_Pagila_RealDuplicateIndex_PositiveCase(t *testing.T) {
	s := parseFixture(t, "pagila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))

	var paymentP1Found bool
	for _, tbl := range s.Tables {
		if tbl.Name != "payment_p2022_01" {
			continue
		}
		paymentP1Found = true
		if len(tbl.Indexes) != 2 {
			t.Fatalf("test assumption broken: payment_p2022_01 has %d indexes, want 2 (idx_fk_payment_p2022_01_customer_id, payment_p2022_01_customer_id_idx)", len(tbl.Indexes))
		}
	}
	if !paymentP1Found {
		t.Fatal("test assumption broken: payment_p2022_01 table not found in pagila_excerpt.sql — fixture drifted")
	}

	_, surf := dbrules.Run(s)
	got := surfaceWithCategoryLocal(surf, surface.CategoryDBDupIndex)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 real exact-duplicate-index item (payment_p2022_01's two customer_id indexes), got %d: %+v", len(got), got)
	}
	if !got[0].StructuralFacts["exact_duplicate_index"] {
		t.Error("exact_duplicate_index must be true")
	}
	if got[0].File == "" {
		t.Error("File must be populated (real fixture position, not a synthetic zero-value Pos)")
	}
}
