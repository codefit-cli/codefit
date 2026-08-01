package paradigm_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// This file locks the SCHEMA GATE signals (stage 1: computed, named, and wired
// to NOTHING). Every test here asks one question of one signal, and the whole
// set is built around the guard that matters more than any individual signal:
// NO VACUOUS TRUTHS. Two of the five signals ("no foreign keys", "no audit
// timestamps") conclude from ABSENCE, and absence is trivially true of an empty
// or toy schema — a gate that fires on an empty schema would classify anything
// as a warehouse.
//
// Fixtures here are hand-built db.Schema values because the edge cases the
// guards need (an empty schema, a 2-table schema, a table the parser could not
// prove complete) are cheaper to state directly than to provoke through DDL.
// The POSITIVE, real-DDL half lives in
// internal/providers/sqlddl/schemagate_corpus_test.go, which runs these same
// signals over every vendored corpus through the real parser — the reading
// CLAUDE.md asks for whenever a hand-built fixture could hold a value
// production never produces.

// provenTable builds a table whose structure the parser PROVED complete
// (Complete=true is what the real sqlddl reducer sets for a table it fully
// reduced). Absence-based signals abstain on unproven tables, so a fixture that
// forgot this would silently test the abstention path instead of the signal.
func provenTable(name string, cols ...string) db.Table {
	t := db.Table{Name: name, Complete: true}
	for _, c := range cols {
		t.Columns = append(t.Columns, db.Column{Name: c})
	}
	return t
}

// refs adds a foreign key from t to every named table.
func refs(t db.Table, targets ...string) db.Table {
	for _, target := range targets {
		t.ForeignKeys = append(t.ForeignKeys, db.ForeignKey{
			Columns: []string{target + "_id"}, RefTable: target, RefColumns: []string{"id"},
		})
	}
	return t
}

func firedNames(e paradigm.WarehouseEvidence) []string {
	out := make([]string, 0, len(e.Fired))
	for _, s := range e.Fired {
		out = append(out, string(s))
	}
	return out
}

// assertFired checks the fired set EXACTLY. Asserting the exact set (not just
// "contains X") is what makes each test discriminating: a change that makes a
// second signal leak in fails here instead of hiding behind a Has() check.
func assertFired(t *testing.T, e paradigm.WarehouseEvidence, want ...paradigm.Signal) {
	t.Helper()
	if len(e.Fired) != len(want) {
		t.Fatalf("Fired = %v, want %v", firedNames(e), want)
	}
	for i, w := range want {
		if e.Fired[i] != w {
			t.Fatalf("Fired = %v, want %v", firedNames(e), want)
		}
	}
	if e.Count() != len(want) {
		t.Errorf("Count() = %d, want %d", e.Count(), len(want))
	}
	for _, w := range want {
		if !e.Has(w) {
			t.Errorf("Has(%q) = false, want true", w)
		}
	}
}

// ---------------------------------------------------------------------------
// THE VACUITY GUARDS — written first, and the reason the floor exists at all.
// ---------------------------------------------------------------------------

// TestWarehouseSignals_NilSchema_FiresNothing: a nil schema has no evidence of
// anything. It is separate from the empty-schema case because nil is a
// different code path (Detect handles it explicitly too).
func TestWarehouseSignals_NilSchema_FiresNothing(t *testing.T) {
	assertFired(t, paradigm.WarehouseSignals(nil))
}

// TestWarehouseSignals_EmptySchema_FiresNothing is the headline guard. An empty
// schema declares no foreign keys and carries no created_at/updated_at, so
// signals 3 and 4 are TRUE of it read literally. They must still not fire.
func TestWarehouseSignals_EmptySchema_FiresNothing(t *testing.T) {
	assertFired(t, paradigm.WarehouseSignals(&db.Schema{}))
}

// TestWarehouseSignals_DegenerateTwoTableSchema_FiresNothing: the same vacuity
// one step up. Two tables, no foreign keys, no audit columns, and key-shaped
// column names — every absence-based signal is literally true, and the schema
// is still far too small to be evidence of a warehouse.
func TestWarehouseSignals_DegenerateTwoTableSchema_FiresNothing(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("dim_date", "date_sk", "customer_key", "product_key", "store_key"),
		provenTable("thing", "thing_sk", "other_sk", "a_key", "b_key"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// ---------------------------------------------------------------------------
// SIGNAL 1 — calendar/time table present
// ---------------------------------------------------------------------------

// TestSignalCalendarTable_BareCalendarCounts locks the difference from
// dwrules.timeDimensionName: THAT function asks "is this DIMENSION a time
// dimension" and requires a role token; the GATE asks "does this SCHEMA contain
// a calendar table at all", so the role token is OPTIONAL. A bare `calendar`
// counts.
func TestSignalCalendarTable_BareCalendarCounts(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("calendar", "day", "created_at"),
		provenTable("orders", "id", "created_at"),
		provenTable("customers", "id", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalCalendarTable)
}

// TestSignalCalendarTable_RoleTokenSpellingsCount: every spelling
// paradigm.StripRoleToken already recognizes reaches the gate for free. This is
// the composition the base branch's bug report demands — a THIRD parallel
// vocabulary is what let dwrules and paradigm drift apart.
func TestSignalCalendarTable_RoleTokenSpellingsCount(t *testing.T) {
	for _, name := range []string{"dim_date", "d_date", "DimDate", "date_dim", "DIM_CALENDAR", "dim_time"} {
		s := &db.Schema{Tables: []db.Table{
			provenTable(name, "day", "created_at"),
			provenTable("orders", "id", "created_at"),
			provenTable("customers", "id", "created_at"),
		}}
		e := paradigm.WarehouseSignals(s)
		if !e.Has(paradigm.SignalCalendarTable) {
			t.Errorf("table %q: calendar signal did not fire, Fired = %v", name, firedNames(e))
		}
	}
}

// TestSignalCalendarTable_NearMissesDoNotCount is the discriminating half. The
// remainder is matched by EQUALITY, never containment — dim_candidate and
// dim_update both CONTAIN "date" once separators are dropped, and neither is a
// calendar.
func TestSignalCalendarTable_NearMissesDoNotCount(t *testing.T) {
	for _, name := range []string{"dim_candidate", "dim_update", "dim_validate", "date_ranges", "update_log"} {
		s := &db.Schema{Tables: []db.Table{
			provenTable(name, "day", "created_at"),
			provenTable("orders", "id", "created_at"),
			provenTable("customers", "id", "created_at"),
		}}
		e := paradigm.WarehouseSignals(s)
		if e.Has(paradigm.SignalCalendarTable) {
			t.Errorf("table %q: calendar signal fired, want no fire", name)
		}
	}
}

// ---------------------------------------------------------------------------
// SIGNAL 2 — surrogate-key vocabulary (_sk)
// ---------------------------------------------------------------------------

// TestSignalSurrogateKeyNames_FiresOnSchemaWideConvention: three _sk columns
// spread over two tables is a SCHEMA convention, which is what the signal
// claims to see.
func TestSignalSurrogateKeyNames_FiresOnSchemaWideConvention(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("store_sales", "ss_item_sk", "ss_customer_sk", "created_at"),
		provenTable("item", "i_item_sk", "created_at"),
		provenTable("customer", "c_id", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalSurrogateKeyNames)
}

// TestSignalSurrogateKeyNames_TooFewColumnsDoesNotFire controls the COLUMN
// half of the threshold, and the second case is the one that makes it a
// control rather than an ornament.
//
// The first case (one stray column) was originally the whole test — and it is
// worthless on its own: it is already blocked by the DISTINCT-TABLES half, so
// lowering the column threshold to 1 left it passing. Proven by mutation, not
// by inspection. The second case spreads two columns over two tables, clearing
// the table half, so only the column threshold can stop it.
func TestSignalSurrogateKeyNames_TooFewColumnsDoesNotFire(t *testing.T) {
	cases := map[string]*db.Schema{
		"one incidental column": {Tables: []db.Table{
			provenTable("orders", "id", "thing_sk", "created_at"),
			provenTable("customers", "id", "created_at"),
			provenTable("addresses", "id", "created_at"),
		}},
		"two columns over two tables": {Tables: []db.Table{
			provenTable("orders", "id", "thing_sk", "created_at"),
			provenTable("customers", "id", "other_sk", "created_at"),
			provenTable("addresses", "id", "created_at"),
		}},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) { assertFired(t, paradigm.WarehouseSignals(s)) })
	}
}

// TestSignalSurrogateKeyNames_SingleTableHabitDoesNotFire: three _sk columns in
// ONE table is one developer's local habit, not a schema-wide convention. The
// ">= 2 distinct tables" half of the threshold is what makes this signal about
// the SCHEMA rather than about a table — the whole point of the gate.
func TestSignalSurrogateKeyNames_SingleTableHabitDoesNotFire(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("orders", "a_sk", "b_sk", "c_sk", "d_sk", "created_at"),
		provenTable("customers", "id", "created_at"),
		provenTable("addresses", "id", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// TestSignalSurrogateKeyNames_WordEndingInSkDoesNotFire is the false-positive
// this signal's spelling rule exists to prevent. Dropping separators and asking
// "does the name end in sk" accepts `risk`, `disk` and `asterisk`; matching the
// last UNDERSCORE-DELIMITED segment does not. The delimiter is the word
// boundary — the same doctrine segmentRole already applies to role tokens.
func TestSignalSurrogateKeyNames_WordEndingInSkDoesNotFire(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("orders", "risk", "credit_risk", "created_at"),
		provenTable("customers", "disk", "asterisk", "created_at"),
		provenTable("addresses", "risk", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// ---------------------------------------------------------------------------
// SIGNAL 3 — bulk-load shape (no declared FKs, many key-like columns)
// ---------------------------------------------------------------------------

// TestSignalBulkLoadShape_FiresOnFKLessKeyHeavySchema: the pattern of a
// warehouse that drops FK constraints for load speed — one wide fact-shaped
// table of nothing but dimension references, and no constraint anywhere.
func TestSignalBulkLoadShape_FiresOnFKLessKeyHeavySchema(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("sales", "date_key", "customer_key", "product_key", "store_key", "promo_key", "created_at"),
		provenTable("customer", "customer_key", "c_name", "created_at"),
		provenTable("product", "product_key", "p_name", "created_at"),
		provenTable("store", "store_key", "s_name", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalBulkLoadShape)
}

// TestSignalBulkLoadShape_OneDeclaredForeignKeyDisqualifies: the signal claims
// "no declared foreign keys". One anywhere in the schema falsifies it.
func TestSignalBulkLoadShape_OneDeclaredForeignKeyDisqualifies(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("sales", "date_key", "customer_key", "product_key", "store_key", "promo_key", "created_at"),
		refs(provenTable("customer", "customer_key", "c_name", "created_at"), "product"),
		provenTable("product", "product_key", "p_name", "created_at"),
		provenTable("store", "store_key", "s_name", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// TestSignalBulkLoadShape_PlainIdPerTableDoesNotFire: every toy schema without
// declared constraints has one id-ish column per table. That is not a
// bulk-loaded warehouse, and the per-table concentration threshold is what
// separates them.
func TestSignalBulkLoadShape_PlainIdPerTableDoesNotFire(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("orders", "order_id", "total", "created_at"),
		provenTable("customers", "customer_id", "name", "created_at"),
		provenTable("addresses", "address_id", "street", "created_at"),
		provenTable("countries", "country_id", "name", "created_at"),
		provenTable("cities", "city_id", "name", "created_at"),
		provenTable("regions", "region_id", "name", "created_at"),
		provenTable("zones", "zone_id", "name", "created_at"),
		provenTable("areas", "area_id", "name", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// TestSignalBulkLoadShape_UnprovenStructureAbstains is the doctrinal guard, and
// the one that keeps the gate honest on REAL corpora. "No foreign keys" is an
// absence claim; a table the parser could not fully reduce may have declared
// the very FK block that would falsify it. AdventureWorksDW is exactly this
// case: its DDL plainly declares eight foreign keys that the T-SQL reducer
// drops. Affirming "this schema declares no foreign keys" there would be a
// confident false claim over DDL that says otherwise.
func TestSignalBulkLoadShape_UnprovenStructureAbstains(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("sales", "date_key", "customer_key", "product_key", "store_key", "promo_key", "created_at"),
		provenTable("customer", "customer_key", "c_name", "created_at"),
		provenTable("product", "product_key", "p_name", "created_at"),
		provenTable("store", "store_key", "s_name", "created_at"),
	}}
	s.Tables[3].MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE store ADD CONSTRAINT ...", db.Pos{})
	assertFired(t, paradigm.WarehouseSignals(s))
}

// ---------------------------------------------------------------------------
// SIGNAL 4 — no audit timestamps anywhere in the schema
// ---------------------------------------------------------------------------

// TestSignalNoAuditTimestamps_FiresWhenNoTableCarriesThem.
func TestSignalNoAuditTimestamps_FiresWhenNoTableCarriesThem(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("orders", "id", "total"),
		provenTable("customers", "id", "name"),
		provenTable("addresses", "id", "street"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalNoAuditTimestamps)
}

// TestSignalNoAuditTimestamps_OneStampAnywhereDisqualifies: "anywhere in the
// schema" means exactly that — a single created_at on a single table falsifies
// the claim.
func TestSignalNoAuditTimestamps_OneStampAnywhereDisqualifies(t *testing.T) {
	for _, stamp := range []string{"created_at", "createdAt", "updated_at", "updatedAt", "CREATED_AT"} {
		s := &db.Schema{Tables: []db.Table{
			provenTable("orders", "id", "total"),
			provenTable("customers", "id", "name"),
			provenTable("addresses", "id", stamp),
		}}
		e := paradigm.WarehouseSignals(s)
		if e.Has(paradigm.SignalNoAuditTimestamps) {
			t.Errorf("column %q: no-audit-timestamps fired, want no fire", stamp)
		}
	}
}

// TestSignalNoAuditTimestamps_UnprovenStructureAbstains: same absence doctrine
// as db052, which skips a table whose structure is unproven because a dropped
// ADD COLUMN might have declared created_at.
func TestSignalNoAuditTimestamps_UnprovenStructureAbstains(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("orders", "id", "total"),
		provenTable("customers", "id", "name"),
		provenTable("addresses", "id", "street"),
	}}
	s.Tables[2].MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE addresses ADD COLUMN ...", db.Pos{})
	assertFired(t, paradigm.WarehouseSignals(s))
}

// TestSignalNoAuditTimestamps_ColumnlessTableAbstains: a table with no columns
// at all cannot testify to the ABSENCE of an audit column.
func TestSignalNoAuditTimestamps_ColumnlessTableAbstains(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("orders", "id", "total"),
		provenTable("customers", "id", "name"),
		provenTable("addresses"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// ---------------------------------------------------------------------------
// SIGNAL 5 — star topology (hub-and-spoke of depth 1)
// ---------------------------------------------------------------------------

// TestSignalStarTopology_FiresOnDepthOneHub.
func TestSignalStarTopology_FiresOnDepthOneHub(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("sales", "id", "created_at"), "customer", "product"),
		provenTable("customer", "id", "created_at"),
		provenTable("product", "id", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalStarTopology)
}

// TestSignalStarTopology_OLTPMeshDoesNotFire is the contrast the signal is
// defined against: order -> customer -> address -> country runs 3+ deep, so no
// hub's spokes are leaves.
func TestSignalStarTopology_OLTPMeshDoesNotFire(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("orders", "id", "created_at"), "customers", "shipments"),
		refs(provenTable("customers", "id", "created_at"), "addresses"),
		refs(provenTable("addresses", "id", "created_at"), "countries"),
		provenTable("countries", "id", "created_at"),
		refs(provenTable("shipments", "id", "created_at"), "addresses"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// TestSignalStarTopology_FanOutOfOneDoesNotFire: a hub is hub-AND-SPOKE, plural.
func TestSignalStarTopology_FanOutOfOneDoesNotFire(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("orders", "id", "created_at"), "customers"),
		provenTable("customers", "id", "created_at"),
		provenTable("addresses", "id", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// TestSignalStarTopology_DuplicateFKsToOneTableAreOneSpoke: two foreign keys to
// the same table (a fact with both a bill_to and a ship_to customer) is fan-out
// of ONE distinct table, not two.
func TestSignalStarTopology_DuplicateFKsToOneTableAreOneSpoke(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("sales", "id", "created_at"), "customer", "customer"),
		provenTable("customer", "id", "created_at"),
		provenTable("product", "id", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// TestSignalStarTopology_AbsentSpokeAbstains: a spoke that is not in the schema
// cannot be shown to reference nothing. Fail closed — the alternative is
// claiming a star from a table codefit never read.
func TestSignalStarTopology_AbsentSpokeAbstains(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("sales", "id", "created_at"), "customer", "product_not_in_schema"),
		provenTable("customer", "id", "created_at"),
		provenTable("other", "id", "created_at"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s))
}

// TestSignalStarTopology_UnprovenSpokeAbstains: "the spoke references nothing"
// is an absence claim about the SPOKE, so an unproven spoke abstains. The HUB
// needs no such proof — a dropped statement can only UNDERcount its fan-out,
// never fabricate it (the same asymmetry paradigm.unprovableDemotions rests on).
func TestSignalStarTopology_UnprovenSpokeAbstains(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("sales", "id", "created_at"), "customer", "product"),
		provenTable("customer", "id", "created_at"),
		provenTable("product", "id", "created_at"),
	}}
	s.Tables[2].MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE product ADD CONSTRAINT ...", db.Pos{})
	assertFired(t, paradigm.WarehouseSignals(s))
}

// ---------------------------------------------------------------------------
// Result shape
// ---------------------------------------------------------------------------

// TestWarehouseSignals_FiredOrderIsStable: consumers report WHICH signals fired,
// so the order must not depend on map iteration.
func TestWarehouseSignals_FiredOrderIsStable(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		refs(provenTable("fact_sales", "sales_sk", "customer_sk", "product_sk"), "dim_customer", "dim_date"),
		provenTable("dim_customer", "customer_sk", "name"),
		provenTable("dim_date", "date_sk", "day"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s),
		paradigm.SignalCalendarTable,
		paradigm.SignalSurrogateKeyNames,
		paradigm.SignalNoAuditTimestamps,
		paradigm.SignalStarTopology,
	)
}

// TestWarehouseSignals_HasIsFalseForUnfired.
func TestWarehouseSignals_HasIsFalseForUnfired(t *testing.T) {
	e := paradigm.WarehouseSignals(&db.Schema{})
	for _, s := range []paradigm.Signal{
		paradigm.SignalCalendarTable,
		paradigm.SignalSurrogateKeyNames,
		paradigm.SignalBulkLoadShape,
		paradigm.SignalNoAuditTimestamps,
		paradigm.SignalStarTopology,
	} {
		if e.Has(s) {
			t.Errorf("Has(%q) = true on an empty schema, want false", s)
		}
	}
}
