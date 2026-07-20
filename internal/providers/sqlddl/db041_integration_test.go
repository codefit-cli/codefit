package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-041 (trigger external-effecting call) dogfood over REAL parsed DDL. Under
// the STRICT vocabulary, the real T-SQL uPurchaseOrderDetail — whose only calls
// are EXECUTE of INTERNAL logging procs — is the NEGATIVE / trap, not a positive;
// the positive is a constructed xp_cmdshell trigger. PostgreSQL exercises the
// ADR-0026 trigger→function resolution (the NOTIFY lives in the function).

func db041On(s *db.Schema, name string) []findings.SurfaceItem {
	_, items := dbrules.Run(s)
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category != string(surface.CategoryDBTriggerExternalCall) {
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

// --- T-SQL: constructed POSITIVE + real NEGATIVE / trap ---

func TestDB041_TSQL_ConstructedXpCmdshell_Fires(t *testing.T) {
	s := goldenSchema(t, "tsql/constructed_external_call_trigger.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	got := db041On(s, "trg_orders_shell")
	if len(got) != 1 {
		t.Fatalf("DB-041 on trg_orders_shell = %d, want 1 (it EXECs xp_cmdshell)", len(got))
	}
	if !db040SignalsHave(got[0], "external_call: xp_cmdshell") {
		t.Errorf("want xp_cmdshell external_call signal, got %v", got[0].StructuralSignals)
	}
}

func TestDB041_TSQL_uPurchaseOrderDetail_InternalExecute_DoesNotFire(t *testing.T) {
	// The real trap: its CATCH does EXECUTE [dbo].[uspPrintError]/[uspLogError]
	// — INTERNAL procs. Under the strict vocabulary this is NOT an external call.
	s := goldenSchema(t, "tsql/adventureworks_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	if got := db041On(s, "uPurchaseOrderDetail"); len(got) != 0 {
		t.Errorf("DB-041 on uPurchaseOrderDetail = %d, want 0 (EXECUTE of internal logging procs is not an external call — the trap)", len(got))
	}
	if got := db041On(s, "dEmployee"); len(got) != 0 {
		t.Errorf("DB-041 on dEmployee = %d, want 0 (RAISERROR/ROLLBACK only, no external call)", len(got))
	}
}

// --- PostgreSQL: constructed POSITIVE (trigger→function resolution) + real NEGATIVE ---

func TestDB041_PG_ConstructedNotify_ResolvesFunction_Fires(t *testing.T) {
	s := goldenSchema(t, "pg_constructed_external_call_trigger.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	got := db041On(s, "order_notify_trigger")
	if len(got) != 1 {
		t.Fatalf("DB-041 on order_notify_trigger = %d, want 1 (the resolved function issues NOTIFY)", len(got))
	}
	if !db040SignalsHave(got[0], "external_call: notify") {
		t.Errorf("want notify external_call signal from the RESOLVED function, got %v", got[0].StructuralSignals)
	}
}

func TestDB041_PG_Pagila_LastUpdated_NoExternal_DoesNotFire(t *testing.T) {
	s := goldenSchema(t, "pagila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	if got := db041On(s, "last_updated"); len(got) != 0 {
		t.Errorf("DB-041 on last_updated = %d, want 0 (function only sets NEW.last_update, no external call)", len(got))
	}
}
