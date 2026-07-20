package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-030 (dynamic SQL construction in a routine) dogfood over REAL parsed DDL.
// PostgreSQL has the only real POSITIVE (Pagila rewards_report builds a query
// string with quote_literal and runs it via EXECUTE); MySQL and T-SQL positives
// are constructed (declared synthetic), their real corpora being clean; every
// dialect has real NEGATIVES.

func db030On(s *db.Schema, name string) []findings.SurfaceItem {
	_, items := dbrules.Run(s)
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category != string(surface.CategoryDBDynamicSQLInRoutine) {
			continue
		}
		for _, sig := range it.StructuralSignals {
			if sig == "routine: "+name {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

// --- PostgreSQL: real POSITIVE + real NEGATIVE ---

func TestDB030_PG_PagilaRewardsReport_DynamicExecute_Fires(t *testing.T) {
	s := goldenSchema(t, "pagila_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	if got := db030On(s, "rewards_report"); len(got) != 1 {
		t.Fatalf("DB-030 on rewards_report = %d, want 1 (it builds SQL with quote_literal and runs it via EXECUTE)", len(got))
	}
}

func TestDB030_PG_Pagila_LastUpdated_Static_DoesNotFire(t *testing.T) {
	s := goldenSchema(t, "pagila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	if got := db030On(s, "last_updated"); len(got) != 0 {
		t.Errorf("DB-030 on last_updated = %d, want 0 (only assigns NEW.last_update, no dynamic SQL)", len(got))
	}
}

// --- MySQL: constructed POSITIVE + real NEGATIVE ---

func TestDB030_MySQL_ConstructedPrepare_Fires(t *testing.T) {
	s := goldenSchema(t, "mysql/constructed_dynamic_sql_proc.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	if got := db030On(s, "search_orders"); len(got) != 1 {
		t.Fatalf("DB-030 on search_orders = %d, want 1 (PREPARE ... FROM a CONCATenated string)", len(got))
	}
}

func TestDB030_MySQL_SakilaRewardsReport_Static_DoesNotFire(t *testing.T) {
	s := goldenSchema(t, "mysql/sakila_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	if got := db030On(s, "rewards_report"); len(got) != 0 {
		t.Errorf("DB-030 on MySQL rewards_report = %d, want 0 (temp-table based, no dynamic SQL)", len(got))
	}
}

// --- T-SQL: constructed POSITIVE + real NEGATIVE ---

func TestDB030_TSQL_ConstructedSpExecutesql_Fires(t *testing.T) {
	s := goldenSchema(t, "tsql/constructed_dynamic_sql_proc.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	if got := db030On(s, "SearchProducts"); len(got) != 1 {
		t.Fatalf("DB-030 on SearchProducts = %d, want 1 (builds a string and runs it via sp_executesql)", len(got))
	}
}

func TestDB030_TSQL_uspGetBillOfMaterials_Static_DoesNotFire(t *testing.T) {
	s := goldenSchema(t, "tsql/adventureworks_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	if got := db030On(s, "uspGetBillOfMaterials"); len(got) != 0 {
		t.Errorf("DB-030 on uspGetBillOfMaterials = %d, want 0 (a recursive CTE, no dynamic SQL)", len(got))
	}
}
