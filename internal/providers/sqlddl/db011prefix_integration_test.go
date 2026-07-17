package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-011 (prefix-redundant half, Unit E / Phase 2.2) real-fixture dogfood.
//
// EXPECTED SITUATION, confirmed by direct inspection, not assumed: a
// prefix-redundant index ([a] made obsolete by a composite [a,b]) is an
// antipattern nobody commits to a clean, hand-maintained schema — none of
// the real corpora dogfooded here (Sakila/MySQL, AdventureWorks/T-SQL,
// Pagila/PostgreSQL) contain one.
//
// PostgreSQL/Pagila's excerpt USED TO have zero indexes and zero primary
// keys at all (NotCovered for this whole rule family). The fixture
// extension at architecture/pagila-fixture-real-indexes vendored real
// upstream Pagila CREATE INDEX statements (idx_last_name, idx_fk_address_id,
// idx_fk_city_id, the UNIQUE rental index, plus the real payment_p2022_01
// exact-duplicate pair) — PostgreSQL now has real index-like coverers to run
// this rule against, same as MySQL/T-SQL below. idx_title is NOT vendored
// (it indexed film.title, and film itself is deliberately not vendored —
// discovery/sqlddl-postgres-parser-gaps, obs #1062, HALLAZGO 2).
//
// The POSITIVE fire path is proven at the unit level
// (internal/core/dbrules/db011prefix_test.go) with synthetic-but-
// representative schemas, AND at the mechanism level below by mutating a
// copy of Sakila's own REAL composite primary key (film_actor's
// [actor_id, film_id]) — same discipline as DB-020's "AS SID" -> "AS ssn"
// mutation (db020_integration_test.go).

func TestDB011Prefix_Pagila_RealFixture_NegativeCase(t *testing.T) {
	s := parseFixture(t, "pagila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	indexCount := 0
	for _, tbl := range s.Tables {
		indexCount += len(tbl.Indexes)
	}
	if indexCount == 0 {
		t.Fatal("test assumption broken: expected real Pagila indexes to be present (idx_last_name, idx_fk_address_id, idx_fk_city_id, idx_unq_rental_rental_date_inventory_id_customer_id, payment_p2022_01's two duplicate indexes)")
	}
	_, surf := dbrules.Run(s)
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBPrefixRedundantIndex); len(got) != 0 {
		t.Errorf("real Pagila excerpt has no prefix-redundant index, got %d: %+v", len(got), got)
	}
}

func TestDB011Prefix_Sakila_RealFixture_NegativeCase(t *testing.T) {
	s := parseFixture(t, "mysql/sakila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	indexCount := 0
	for _, tbl := range s.Tables {
		indexCount += len(tbl.Indexes)
	}
	if indexCount == 0 {
		t.Fatal("test assumption broken: expected real Sakila indexes to be present (idx_actor_last_name, idx_fk_film_id)")
	}
	_, surf := dbrules.Run(s)
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBPrefixRedundantIndex); len(got) != 0 {
		t.Errorf("real Sakila excerpt has no prefix-redundant index, got %d: %+v", len(got), got)
	}
}

func TestDB011Prefix_AdventureWorks_RealFixture_NegativeCase(t *testing.T) {
	s := parseFixture(t, "tsql/adventureworks_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	indexCount := 0
	for _, tbl := range s.Tables {
		indexCount += len(tbl.Indexes)
	}
	if indexCount == 0 {
		t.Fatal("test assumption broken: expected the real AdventureWorks idx_customer_account index to be present")
	}
	_, surf := dbrules.Run(s)
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBPrefixRedundantIndex); len(got) != 0 {
		t.Errorf("real AdventureWorks excerpt has no prefix-redundant index, got %d: %+v", len(got), got)
	}
}

// TestDB011Prefix_Sakila_WouldFireIfPrefixIndexExisted proves the mechanism
// genuinely walks a REAL composite primary key (film_actor's own
// [actor_id, film_id], from the real vendored Sakila excerpt), not merely a
// fully synthetic fixture: it adds ONE synthetic index on the PK's leading
// column alone to a mutated COPY of that real table.
func TestDB011Prefix_Sakila_WouldFireIfPrefixIndexExisted(t *testing.T) {
	s := parseFixture(t, "mysql/sakila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	var filmActor *db.Table
	for i := range s.Tables {
		if s.Tables[i].Name == "film_actor" {
			filmActor = &s.Tables[i]
		}
	}
	if filmActor == nil {
		t.Fatal("test setup broken: real film_actor table not found in the Sakila excerpt")
	}
	if len(filmActor.PrimaryKey) != 2 || filmActor.PrimaryKey[0] != "actor_id" || filmActor.PrimaryKey[1] != "film_id" {
		t.Fatalf("test assumption broken: expected film_actor's real composite PK [actor_id, film_id], got %v", filmActor.PrimaryKey)
	}

	patched := *filmActor
	patched.Indexes = append(append([]db.Index(nil), filmActor.Indexes...), db.Index{
		Columns: []string{"actor_id"}, Unique: false, Pos: db.Pos{File: "synthetic", Line: 999},
	})

	_, surf := dbrules.Run(&db.Schema{Tables: []db.Table{patched}})
	got := surfaceWithCategoryLocal(surf, surface.CategoryDBPrefixRedundantIndex)
	if len(got) != 1 {
		t.Fatalf("expected 1 prefix-redundant item once a real index subsumed by the real PK is added, got %d", len(got))
	}
	if !got[0].StructuralFacts["subsumed_by_primary_key"] {
		t.Error("subsumed_by_primary_key must be true — the subsuming coverer here is film_actor's real PK")
	}
}
