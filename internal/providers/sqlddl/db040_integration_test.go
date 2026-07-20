package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-040 (trigger cross-table cascade) dogfood over REAL parsed DDL, proving the
// rule bites on genuinely parsed schemas, not only hand-built db.Schema values.
// Per-dialect matrix (see COVERAGE): T-SQL real POS+NEG; MySQL real POS +
// constructed NEG; PostgreSQL constructed POS (exercising the ADR-0026
// trigger→function resolution) + real NEG. The constructed fixtures declare
// their synthetic origin in their headers.

// db040On returns the DB-040 surface items whose trigger-name signal matches
// name (empty name = all DB-040 items in the schema).
func db040On(s *db.Schema, name string) []findings.SurfaceItem {
	_, items := dbrules.Run(s)
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category != string(surface.CategoryDBTriggerCrossTableCascade) {
			continue
		}
		if name == "" {
			out = append(out, it)
			continue
		}
		for _, sig := range it.StructuralSignals {
			if sig == "trigger: "+name {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

func db040SignalsHave(it findings.SurfaceItem, sig string) bool {
	for _, s := range it.StructuralSignals {
		if s == sig {
			return true
		}
	}
	return false
}

// --- T-SQL AdventureWorks: real POSITIVE + real NEGATIVE ---

func TestDB040_TSQL_uPurchaseOrderDetail_Cascades_Fires(t *testing.T) {
	s := goldenSchema(t, "tsql/adventureworks_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	got := db040On(s, "uPurchaseOrderDetail")
	if len(got) != 1 {
		t.Fatalf("DB-040 on uPurchaseOrderDetail = %d, want 1 (it INSERTs TransactionHistory and UPDATEs PurchaseOrderHeader — other tables)", len(got))
	}
	// It writes to other tables (cross-table) but NOT to its own table via the
	// same-table UPDATE to PurchaseOrderDetail (excluded).
	if !db040SignalsHave(got[0], "writes_other_table: TransactionHistory") {
		t.Errorf("want a TransactionHistory cross-table signal, got %v", got[0].StructuralSignals)
	}
	if !db040SignalsHave(got[0], "writes_other_table: PurchaseOrderHeader") {
		t.Errorf("want a PurchaseOrderHeader cross-table signal, got %v", got[0].StructuralSignals)
	}
	if db040SignalsHave(got[0], "writes_other_table: PurchaseOrderDetail") {
		t.Errorf("PurchaseOrderDetail is the trigger's OWN table — must NOT be reported as cross-table: %v", got[0].StructuralSignals)
	}
}

func TestDB040_TSQL_dEmployee_NoCrossTable_DoesNotFire(t *testing.T) {
	s := goldenSchema(t, "tsql/adventureworks_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	if got := db040On(s, "dEmployee"); len(got) != 0 {
		t.Errorf("DB-040 on dEmployee = %d, want 0 (INSTEAD OF DELETE that only RAISERRORs and ROLLBACKs — no cross-table DML)", len(got))
	}
}

// --- MySQL Sakila: real POSITIVE (film triggers) + constructed NEGATIVE ---

func TestDB040_MySQL_FilmTriggers_CascadeToFilmText_Fire(t *testing.T) {
	s := goldenSchema(t, "mysql/sakila_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	for _, name := range []string{"ins_film", "upd_film", "del_film"} {
		got := db040On(s, name)
		if len(got) != 1 {
			t.Fatalf("DB-040 on %s = %d, want 1 (cascades a write into film_text)", name, len(got))
		}
		if !db040SignalsHave(got[0], "writes_other_table: film_text") {
			t.Errorf("%s: want film_text cross-table signal, got %v", name, got[0].StructuralSignals)
		}
	}
}

func TestDB040_MySQL_ConstructedNonCascading_DoesNotFire(t *testing.T) {
	s := goldenSchema(t, "mysql/constructed_non_cascading_trigger.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	if got := db040On(s, ""); len(got) != 0 {
		t.Errorf("DB-040 = %d, want 0 (the synthetic trg_orders_stamp only SETs NEW — no cross-table DML)", len(got))
	}
}

// --- PostgreSQL: constructed POSITIVE (trigger→function resolution) + real NEGATIVE ---

func TestDB040_PG_ConstructedCascade_ResolvesFunction_Fires(t *testing.T) {
	s := goldenSchema(t, "pg_constructed_cascade_trigger.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	got := db040On(s, "order_audit_trigger")
	if len(got) != 1 {
		t.Fatalf("DB-040 on order_audit_trigger = %d, want 1 (the trigger is bodyless; the cascade lives in the resolved function audit_order_changes, which INSERTs into order_audit)", len(got))
	}
	if !db040SignalsHave(got[0], "writes_other_table: order_audit") {
		t.Errorf("want order_audit cross-table signal from the RESOLVED function, got %v", got[0].StructuralSignals)
	}
	if got[0].StructuralFacts["documented_by_comment"] {
		t.Errorf("the constructed cascade is deliberately UNDOCUMENTED — documented_by_comment must be false, facts=%v", got[0].StructuralFacts)
	}
}

func TestDB040_PG_Pagila_LastUpdated_NoCrossTable_DoesNotFire(t *testing.T) {
	s := goldenSchema(t, "pagila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	// last_updated's function only sets NEW.last_update — a real DB-040 negative.
	if got := db040On(s, "last_updated"); len(got) != 0 {
		t.Errorf("DB-040 on last_updated = %d, want 0 (resolved function only sets NEW.last_update, no cross-table write)", len(got))
	}
}
