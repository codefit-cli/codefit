package sqlddl_test

import (
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-031 (routine without exception handling, 0.2.3 routine-body rules)
// real-fixture dogfood. DB-031 states an ABSENCE — no dialect exception-
// handling construct in the captured routine body — as SURFACE, never an
// affirmation: whether the missing handler matters, and whether a PRESENT
// handler is adequate (an empty CATCH is "present" here yet may be a bug), is
// the agent's judgment. Functions and procedures both surface as db.Procedure,
// so both are covered by the single s.Procedures pass.
//
// Per-dialect coverage proven below against REAL vendored DDL:
//   - MySQL/Sakila: rewards_report (no HANDLER) POS; inventory_held_by_customer
//     (real DECLARE ... HANDLER) NEG.
//   - T-SQL/AdventureWorks: uspGetBillOfMaterials (no TRY/CATCH) POS;
//     uspUpdateEmployeePersonalInfo (real BEGIN TRY/CATCH) NEG.
//   - PostgreSQL: real rewards_report (RAISE EXCEPTION but NO EXCEPTION WHEN)
//     POS — the bare-EXCEPTION trap; plus a CONSTRUCTED negative (declared
//     synthetic in testdata/pg_constructed_exception_handler.sql) that carries
//     a real EXCEPTION WHEN handler block.

// --- MySQL / Sakila ---

func TestDB031_MySQL_RewardsReport_NoHandler_Fires(t *testing.T) {
	s := parseFixture(t, filepath.Join("mysql", "sakila_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	_, surf := dbrules.Run(s)
	got := surfaceForRoutine(surf, "rewards_report")
	if len(got) != 1 {
		t.Fatalf("DB-031 on MySQL rewards_report = %d, want 1 (real proc, NO DECLARE ... HANDLER)", len(got))
	}
}

func TestDB031_MySQL_InventoryHeldByCustomer_HasHandler_DoesNotFire(t *testing.T) {
	s := parseFixture(t, filepath.Join("mysql", "sakila_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	_, surf := dbrules.Run(s)
	if got := surfaceForRoutine(surf, "inventory_held_by_customer"); len(got) != 0 {
		t.Errorf("DB-031 on MySQL inventory_held_by_customer = %d, want 0 (real DECLARE EXIT HANDLER FOR NOT FOUND present)", len(got))
	}
}

// --- T-SQL / AdventureWorks ---

func TestDB031_TSQL_UspGetBillOfMaterials_NoTryCatch_Fires(t *testing.T) {
	s := parseFixture(t, filepath.Join("tsql", "adventureworks_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	_, surf := dbrules.Run(s)
	got := surfaceForRoutine(surf, "uspGetBillOfMaterials")
	if len(got) != 1 {
		t.Fatalf("DB-031 on T-SQL uspGetBillOfMaterials = %d, want 1 (real proc, NO BEGIN TRY/CATCH — its body is a recursive CTE)", len(got))
	}
}

func TestDB031_TSQL_UspUpdateEmployeePersonalInfo_HasTryCatch_DoesNotFire(t *testing.T) {
	s := parseFixture(t, filepath.Join("tsql", "adventureworks_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	_, surf := dbrules.Run(s)
	if got := surfaceForRoutine(surf, "uspUpdateEmployeePersonalInfo"); len(got) != 0 {
		t.Errorf("DB-031 on T-SQL uspUpdateEmployeePersonalInfo = %d, want 0 (real BEGIN TRY ... BEGIN CATCH present)", len(got))
	}
}

// --- PostgreSQL / Pagila (the trap) + constructed negative ---

// TestDB031_Postgres_RewardsReport_RaiseButNoHandler_Fires locks the rule's
// signature failure mode: the REAL vendored Pagila rewards_report contains two
// RAISE EXCEPTION throws and ZERO EXCEPTION WHEN handler clauses. RAISE
// EXCEPTION is a THROW, not a handler — DB-031 MUST fire DESPITE it. A scanner
// that matched bare "EXCEPTION" (or "RAISE EXCEPTION") would false-negative
// here, silently hiding a real un-handled routine.
func TestDB031_Postgres_RewardsReport_RaiseButNoHandler_Fires(t *testing.T) {
	s := parseFixture(t, "pagila_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	// Guard: prove the trap is actually present in the real body before asserting.
	body := findProc(t, s, "rewards_report").Body
	if !body.Complete {
		t.Fatalf("precondition: rewards_report body must be Complete, got Note=%q", body.Note)
	}
	_, surf := dbrules.Run(s)
	got := surfaceForRoutine(surf, "rewards_report")
	if len(got) != 1 {
		t.Fatalf("DB-031 on PG rewards_report = %d, want 1 — it RAISEs EXCEPTION but has NO EXCEPTION WHEN handler; the rule MUST fire DESPITE the RAISE EXCEPTION (bare-EXCEPTION trap)", len(got))
	}
}

func TestDB031_Postgres_ConstructedSafeDivide_HasHandler_DoesNotFire(t *testing.T) {
	s := parseFixture(t, "pg_constructed_exception_handler.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	_, surf := dbrules.Run(s)
	if got := surfaceForRoutine(surf, "safe_divide"); len(got) != 0 {
		t.Errorf("DB-031 on constructed PG safe_divide = %d, want 0 (real BEGIN ... EXCEPTION WHEN ... END handler present)", len(got))
	}
}

// surfaceForRoutine filters DB-031 surface items to those naming the given
// routine (via the "routine: <name>" structural signal), so a fixture with
// several routines can be asserted per-routine.
func surfaceForRoutine(items []findings.SurfaceItem, name string) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category != string(surface.CategoryDBRoutineNoExceptionHandling) {
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
