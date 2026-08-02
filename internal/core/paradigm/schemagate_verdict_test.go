package paradigm_test

import (
	"sort"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// STAGE 2 — the schema gate's VERDICT, and its wiring into Detect/Resolve.
//
// Stage 1 computed six signals and decided nothing. This file locks what stage 2
// decides, and it locks BOTH halves of the split:
//
//   - a schema is a warehouse iff ANY ONE of calendar_table,
//     surrogate_key_names, type_profile_split fires (measured 9/0, 3/0 and 4/0
//     over 26 corpora — zero transactional false positives across the NINE of
//     the 13 transactional corpora that have parseable tables; the other four
//     parse to zero tables and are refused below minJudgeableTables, so they
//     could not have produced one. ADR 0037, re-measured 2026-08-02);
//   - bulk_load_shape, no_audit_timestamps and star_topology are still COMPUTED
//     and still REPORTED, and can never open the gate on their own.
//
// The second half is the one worth having a test for. The two noisy signals fire
// on 5 transactional corpora each, and bulk_load_shape fired on nothing at all;
// a future change that folds any of them back into the verdict must fail here
// rather than quietly re-admitting Sakila as a warehouse.

// ---------------------------------------------------------------------------
// Fixtures — each fires ONE signal, and asserts the EXACT fired set so it can
// never qualify for a reason other than the one it is named for.
// ---------------------------------------------------------------------------

// calendarOnlySchema fires calendar_table and nothing else: every table carries
// created_at (no_audit_timestamps off), nothing declares a foreign key
// (star_topology off), no column name ends in a key-like segment
// (bulk_load_shape off) and no column carries a type (type_profile_split off).
func calendarOnlySchema() *db.Schema {
	return &db.Schema{Tables: []db.Table{
		provenTable("dim_date", "id", "label", "created_at"),
		provenTable("orders", "id", "total", "created_at"),
		provenTable("customers", "id", "name", "created_at"),
	}}
}

// surrogateOnlySchema fires surrogate_key_names and nothing else: 3 _sk columns
// across 2 tables, but only 2 key-like columns in any single table, which is
// below bulk_load_shape's per-table concentration floor.
func surrogateOnlySchema() *db.Schema {
	return &db.Schema{Tables: []db.Table{
		provenTable("sales", "sale_sk", "customer_sk", "created_at"),
		provenTable("people", "person_sk", "created_at"),
		provenTable("things", "id", "created_at"),
	}}
}

// excludedOnlySchema is the motivating shape of the whole inversion, and the
// exact one ADR 0035 measured on real Sakila: one table named dim_status with
// fan-in >= 1 sitting in an otherwise purely transactional schema. It fires the
// two NOISY signals — no_audit_timestamps (these tables spell their stamp
// last_update) and star_topology (order_items is a depth-1 join table) — and
// not one deciding signal.
func excludedOnlySchema() *db.Schema {
	return &db.Schema{Tables: []db.Table{
		refs(provenTable("orders", "id", "customer_id", "status_id", "placed_on"), "customers", "dim_status"),
		refs(provenTable("order_items", "id", "order_id", "product_id", "quantity", "price"), "orders", "products"),
		provenTable("customers", "id", "name", "last_update"),
		provenTable("products", "id", "name", "last_update"),
		provenTable("dim_status", "id", "label", "last_update"),
	}}
}

// bulkLoadOnlySchema fires bulk_load_shape (plus no_audit_timestamps, which is
// unavoidable in a schema deliberately built without created_at) and no
// deciding signal. bulk_load_shape fired on NOTHING across 26 real corpora, so
// this synthetic fixture is the only way to prove it cannot decide.
func bulkLoadOnlySchema() *db.Schema {
	return &db.Schema{Tables: []db.Table{
		provenTable("t1", "order_id", "customer_id", "product_id", "store_id"),
		provenTable("t2", "a_id", "b_id"),
		provenTable("t3", "c_id", "d_id"),
	}}
}

// gateOpeningTables is the minimal, EXPLICIT schema-wide warehouse evidence a
// fixture needs before any question about ROLE ASSIGNMENT is reachable at all.
// Every other test file in this package that asks such a question composes on
// it, so the qualifying evidence is stated once, in one place, and can never be
// mistaken for part of the shape under test.
//
// It is two tables, and both are load-bearing:
//
//   - dim_date is a declared calendar — SignalCalendarTable, the strongest
//     deciding signal measured (8 warehouse fires, 0 transactional, over 26
//     corpora). Nothing references it, so it is demoted to unclassified and
//     holds NO role of its own: it supplies evidence without adding a role that
//     could be mistaken for the one a test is asserting.
//   - region carries no warehouse token at all. It is here for arithmetic, and
//     the arithmetic is a real property of the design: the gate refuses to judge
//     a schema below minJudgeableTables = 3 at all (the no-vacuous-truths guard,
//     unchanged by stage 2), so these two plus AT LEAST ONE table of the
//     fixture's own are the minimum that can qualify. A schema of two tables can
//     never be a warehouse, and the developer's explicit database.paradigm
//     override is the escape hatch for the rare case where that is wrong.
//
// Both are Complete so they cannot perturb the schema-wide completeness
// bookkeeping that Classification.Unprovable rests on.
func gateOpeningTables() []db.Table {
	return []db.Table{
		{Name: "dim_date", Complete: true, PrimaryKey: []string{"id"}},
		{Name: "region", Complete: true, PrimaryKey: []string{"id"}},
	}
}

// withGateOpen appends gateOpeningTables to s and returns it.
func withGateOpen(s *db.Schema) *db.Schema {
	s.Tables = append(s.Tables, gateOpeningTables()...)
	return s
}

// ---------------------------------------------------------------------------
// The verdict itself
// ---------------------------------------------------------------------------

// TestGateOpeningTables_ActuallyOpenTheGate is the POSITIVE PROBE for the
// helper above, and it is not ceremony: a dozen fixtures across this package now
// compose on gateOpeningTables, and if it ever stopped opening the gate every
// one of them would go on passing — the ones asserting "unclassified" by
// vacuity, and the ones asserting a role by failing loudly. Half of that set
// failing silently is exactly the ornament CLAUDE.md's mutation rule exists to
// catch, so the helper is proven directly.
func TestGateOpeningTables_ActuallyOpenTheGate(t *testing.T) {
	// One host table, the smallest a caller ever brings — and the case where the
	// minJudgeableTables floor is tightest.
	cls := paradigm.Detect(withGateOpen(&db.Schema{Tables: []db.Table{{Name: "orders", Complete: true}}}))
	if !cls.Gate.Open {
		t.Fatalf("gateOpeningTables does not open the gate (Fired = %v) — every fixture composed on it "+
			"is now testing the closed-gate path instead of the shape it names", cls.Gate.Fired)
	}
	if got := cls.Gate.Deciding; len(got) != 1 || got[0] != paradigm.SignalCalendarTable {
		t.Errorf("Gate.Deciding = %v, want [calendar_table]", got)
	}
	// And it contributes NO role of its own, so a test asserting a role on some
	// other table cannot be reading this helper's output by accident.
	for _, name := range []string{"dim_date", "region"} {
		if got := cls.Roles[name]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[%s] = %q, want unclassified — the helper must supply evidence, not roles", name, got)
		}
	}
}

// TestWarehouseEvidence_EachDecidingSignalQualifiesAlone locks the "ANY ONE"
// half. Requiring one of these three reaches 10 of 13 real warehouses with zero
// false positives over the nine transactional corpora that have parseable
// tables; requiring two reaches 5.
func TestWarehouseEvidence_EachDecidingSignalQualifiesAlone(t *testing.T) {
	cases := []struct {
		name   string
		schema *db.Schema
		// fired is the EXACT fired set, so a leaked second signal cannot make
		// Qualifies() true for a reason other than the one under test.
		fired  []paradigm.Signal
		signal paradigm.Signal
	}{
		{
			name: "calendar_table", schema: calendarOnlySchema(),
			fired: []paradigm.Signal{paradigm.SignalCalendarTable}, signal: paradigm.SignalCalendarTable,
		},
		{
			name: "surrogate_key_names", schema: surrogateOnlySchema(),
			fired: []paradigm.Signal{paradigm.SignalSurrogateKeyNames}, signal: paradigm.SignalSurrogateKeyNames,
		},
		{
			// warehouseSplit (schemagate_typeprofile_test.go) is the measured
			// bimodal fixture. It also fires no_audit_timestamps — an EXCLUDED
			// signal, asserted rather than hidden — which is precisely why the
			// Deciding() assertion below carries the weight here.
			name: "type_profile_split", schema: warehouseSplit(),
			fired:  []paradigm.Signal{paradigm.SignalNoAuditTimestamps, paradigm.SignalTypeProfileSplit},
			signal: paradigm.SignalTypeProfileSplit,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := paradigm.WarehouseSignals(c.schema)
			assertFired(t, e, c.fired...)
			if !e.Qualifies() {
				t.Errorf("Qualifies() = false, want true — %s alone decides", c.signal)
			}
			if got := e.Deciding(); len(got) != 1 || got[0] != c.signal {
				t.Errorf("Deciding() = %v, want [%s]", got, c.signal)
			}
		})
	}
}

// TestWarehouseEvidence_ExcludedSignalsNeverQualify is the half that protects
// precision. Both fixtures fire real signals; neither may open the gate.
func TestWarehouseEvidence_ExcludedSignalsNeverQualify(t *testing.T) {
	t.Run("no_audit_timestamps + star_topology", func(t *testing.T) {
		e := paradigm.WarehouseSignals(excludedOnlySchema())
		assertFired(t, e, paradigm.SignalNoAuditTimestamps, paradigm.SignalStarTopology)
		if e.Qualifies() {
			t.Error("Qualifies() = true — the two 5-transactional-fire signals must never decide")
		}
		if got := e.Deciding(); len(got) != 0 {
			t.Errorf("Deciding() = %v, want empty", got)
		}
	})

	t.Run("bulk_load_shape", func(t *testing.T) {
		e := paradigm.WarehouseSignals(bulkLoadOnlySchema())
		assertFired(t, e, paradigm.SignalBulkLoadShape, paradigm.SignalNoAuditTimestamps)
		if e.Qualifies() {
			t.Error("Qualifies() = true — bulk_load_shape fired on 0 of 26 real corpora and must never decide")
		}
	})
}

// TestWarehouseEvidence_NoSignals_DoesNotQualify: the floor. An empty schema
// qualifies as nothing.
func TestWarehouseEvidence_NoSignals_DoesNotQualify(t *testing.T) {
	for _, s := range []*db.Schema{nil, {}} {
		e := paradigm.WarehouseSignals(s)
		assertFired(t, e)
		if e.Qualifies() {
			t.Errorf("Qualifies() = true for %v, want false", s)
		}
	}
}

// TestWarehouseEvidence_DecidingIsASubsetOfFired locks the reporting contract:
// all six signals stay computed and reported, and Deciding never invents one
// that did not fire. Written because the whole reason a counting score was
// rejected is that codefit must be able to say WHY it concluded "warehouse".
func TestWarehouseEvidence_DecidingIsASubsetOfFired(t *testing.T) {
	// A schema firing one deciding AND one excluded signal: calendarOnlySchema
	// with the created_at stamps removed, so no_audit_timestamps fires too.
	s := &db.Schema{Tables: []db.Table{
		provenTable("dim_date", "id", "label"),
		provenTable("orders", "id", "total"),
		provenTable("customers", "id", "name"),
	}}
	e := paradigm.WarehouseSignals(s)
	assertFired(t, e, paradigm.SignalCalendarTable, paradigm.SignalNoAuditTimestamps)
	if !e.Qualifies() {
		t.Fatal("Qualifies() = false, want true")
	}
	if got := e.Deciding(); len(got) != 1 || got[0] != paradigm.SignalCalendarTable {
		t.Errorf("Deciding() = %v, want [calendar_table] — an excluded signal must never appear here", got)
	}
}

// ---------------------------------------------------------------------------
// The wiring: Detect asks the schema BEFORE it assigns any role
// ---------------------------------------------------------------------------

// withheldNames returns the sorted table names whose role the gate withheld.
func withheldNames(cls paradigm.Classification) []string {
	out := make([]string, 0, len(cls.Gate.Withheld))
	for name := range cls.Gate.Withheld {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestDetect_ClosedGate_WithholdsWarehouseRoles is the headline behavior change,
// on the exact schema that motivated it (ADR 0035's Context, ADR 0037's
// Decision). Before stage 2, dim_status promoted itself to a dimension on its
// own name plus one inbound foreign key, folded the whole schema to "mixed", and
// silenced its own DB-002/DB-003 1NF surface. The schema got no vote.
//
// It votes now, and it says no.
func TestDetect_ClosedGate_WithholdsWarehouseRoles(t *testing.T) {
	cls := paradigm.Detect(excludedOnlySchema())

	if cls.Gate.Open {
		t.Error("Gate.Open = true, want false — no deciding signal fired on this schema")
	}
	if got := cls.Roles["dim_status"]; got != paradigm.RoleUnclassified {
		t.Errorf("Roles[dim_status] = %q, want unclassified — the schema is not a warehouse, "+
			"so no table inside it gets a warehouse role", got)
	}
	if cls.Paradigm != paradigm.ParadigmOLTP {
		t.Errorf("Paradigm = %q, want oltp", cls.Paradigm)
	}

	// The withholding is REPORTED, never silent: the sensor's note is built
	// from exactly this, and a dev who disagrees needs to know what was taken.
	if got, want := withheldNames(cls), []string{"dim_status"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("Gate.Withheld names = %v, want %v", got, want)
	}
	if got := cls.Gate.Withheld["dim_status"]; got != paradigm.RoleDimension {
		t.Errorf("Gate.Withheld[dim_status] = %q, want dimension — the note must be able to say WHAT was withheld", got)
	}
	// All six signals stay reported even when the gate closes.
	if len(cls.Gate.Fired) != 2 {
		t.Errorf("Gate.Fired = %v, want the two excluded signals reported", cls.Gate.Fired)
	}
	if len(cls.Gate.Deciding) != 0 {
		t.Errorf("Gate.Deciding = %v, want empty", cls.Gate.Deciding)
	}
}

// TestDetect_OpenGate_AssignsRolesExactlyAsBefore is the A/B half, and it is
// deliberately ONE TABLE NAME away from the test above: rename dim_status to
// dim_date and the schema now declares a calendar, which is real schema-wide
// warehouse evidence. Everything else — the fan-in, the join table, the
// last_update stamps — is byte-identical.
//
// That single-token delta is the point: the gate changes WHOSE evidence decides,
// not how roles are assigned once the schema qualifies.
func TestDetect_OpenGate_AssignsRolesExactlyAsBefore(t *testing.T) {
	s := excludedOnlySchema()
	for i := range s.Tables {
		if s.Tables[i].Name == "dim_status" {
			s.Tables[i].Name = "dim_date"
		}
		for j := range s.Tables[i].ForeignKeys {
			if s.Tables[i].ForeignKeys[j].RefTable == "dim_status" {
				s.Tables[i].ForeignKeys[j].RefTable = "dim_date"
			}
		}
	}

	cls := paradigm.Detect(s)

	if !cls.Gate.Open {
		t.Fatalf("Gate.Open = false, want true — dim_date is a declared calendar (Fired = %v)", cls.Gate.Fired)
	}
	if got := cls.Gate.Deciding; len(got) != 1 || got[0] != paradigm.SignalCalendarTable {
		t.Errorf("Gate.Deciding = %v, want [calendar_table]", got)
	}
	if got := cls.Roles["dim_date"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[dim_date] = %q, want dimension — inside a qualifying schema, roles are "+
			"assigned exactly as they were before stage 2", got)
	}
	if cls.Paradigm != paradigm.ParadigmMixed {
		t.Errorf("Paradigm = %q, want mixed", cls.Paradigm)
	}
	if len(cls.Gate.Withheld) != 0 {
		t.Errorf("Gate.Withheld = %v, want empty when the gate is open", cls.Gate.Withheld)
	}
}

// TestDetect_ClosedGate_WithholdsStagingAndMartToo: staging and mart are
// warehouse roles too, and mart is one of the three roles the sensor's
// 3NF-suppression acts on (isOLAPRole). A gate that withheld only fact and
// dimension would leave the hole half-open.
func TestDetect_ClosedGate_WithholdsStagingAndMartToo(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("stg_raw_events", "id", "payload", "created_at"),
		provenTable("mart_customer_summary", "id", "total", "created_at"),
		provenTable("orders", "id", "total", "created_at"),
	}}

	cls := paradigm.Detect(s)
	if cls.Gate.Open {
		t.Fatalf("Gate.Open = true, want false (Fired = %v)", cls.Gate.Fired)
	}
	for name, wantWithheld := range map[string]paradigm.Role{
		"stg_raw_events":        paradigm.RoleStaging,
		"mart_customer_summary": paradigm.RoleMart,
	} {
		if got := cls.Roles[name]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[%s] = %q, want unclassified", name, got)
		}
		if got := cls.Gate.Withheld[name]; got != wantWithheld {
			t.Errorf("Gate.Withheld[%s] = %q, want %q", name, got, wantWithheld)
		}
	}
}

// TestDetect_ClosedGate_ReportsNoUnprovableDemotion locks the demotion-CAUSE
// contract Classification.Unprovable carries. Unprovable means "this table's
// name nominated a role and STRUCTURE demoted it, and that structure might be a
// dropped statement". When the gate closes, the cause is the SCHEMA VERDICT, not
// structure — marking those tables unprovable would be a false claim about why.
func TestDetect_ClosedGate_ReportsNoUnprovableDemotion(t *testing.T) {
	unproven := db.Table{Name: "fact_sales"}
	unproven.MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE fact_sales ...;", db.Pos{File: "x.sql", Line: 1})
	s := &db.Schema{Tables: []db.Table{
		unproven,
		provenTable("orders", "id", "total", "created_at"),
		provenTable("customers", "id", "name", "created_at"),
	}}

	cls := paradigm.Detect(s)
	if cls.Gate.Open {
		t.Fatalf("Gate.Open = true, want false (Fired = %v)", cls.Gate.Fired)
	}
	if len(cls.Unprovable) != 0 {
		t.Errorf("Unprovable = %v, want empty — the demotion cause is the closed gate, not structure", cls.Unprovable)
	}
}

// ---------------------------------------------------------------------------
// Developer autonomy: the explicit override outranks the gate
// ---------------------------------------------------------------------------

// TestResolve_ExplicitWarehouseOverride_ReopensAClosedGate locks the
// INNEGOCIABLE rule of this project: the developer decides, and codefit never
// overrules an explicit setting.
//
// database.paradigm: olap (or mixed) is the developer ASSERTING that this is a
// warehouse. A gate that then withheld every role would be codefit answering
// "no it isn't" — and it would do real damage beyond 3NF-suppression, because
// the whole DW-0xx family reads Classification.Roles: with every role withheld,
// an explicit olap schema would get zero warehouse rules run over it.
func TestResolve_ExplicitWarehouseOverride_ReopensAClosedGate(t *testing.T) {
	for _, override := range []paradigm.Paradigm{paradigm.ParadigmOLAP, paradigm.ParadigmMixed} {
		t.Run(string(override), func(t *testing.T) {
			detected := paradigm.Detect(excludedOnlySchema())
			if detected.Gate.Open || detected.Roles["dim_status"] != paradigm.RoleUnclassified {
				t.Fatal("fixture drift: detection must have CLOSED the gate for this test to mean anything")
			}

			got := paradigm.Resolve(detected, override)

			if got.Paradigm != override {
				t.Errorf("Paradigm = %q, want %q", got.Paradigm, override)
			}
			if r := got.Roles["dim_status"]; r != paradigm.RoleDimension {
				t.Errorf("Roles[dim_status] = %q, want dimension — an explicit %s override asserts "+
					"this IS a warehouse, and the gate must not overrule the developer", r, override)
			}
			if !got.Gate.Open {
				t.Error("Gate.Open = false, want true — the override reopened it")
			}
			if !got.Gate.ByOverride {
				t.Error("Gate.ByOverride = false, want true — the note must be able to say the gate was " +
					"opened by config, not by evidence")
			}
			if len(got.Gate.Withheld) != 0 {
				t.Errorf("Gate.Withheld = %v, want empty — nothing is withheld once the roles are restored", got.Gate.Withheld)
			}
			// The evidence itself is a FACT and must survive the override: the
			// gate reports what it saw, the override changes what is done with it.
			if len(got.Gate.Fired) != len(detected.Gate.Fired) {
				t.Errorf("Gate.Fired = %v, want the detected evidence preserved (%v)", got.Gate.Fired, detected.Gate.Fired)
			}
			if len(got.Gate.Deciding) != 0 {
				t.Errorf("Gate.Deciding = %v, want empty — no deciding signal fired; config opened this gate", got.Gate.Deciding)
			}
		})
	}
}

// TestResolve_ExplicitOLTP_NeverManufacturesWarehouseRoles is the other
// direction of the same principle, and it must stay closed: an explicit oltp is
// the developer asserting this is NOT a warehouse. Restoring the withheld roles
// there would be codefit overruling them in the direction that SILENCES 1NF
// findings — the exact failure this whole slice exists to prevent.
func TestResolve_ExplicitOLTP_NeverManufacturesWarehouseRoles(t *testing.T) {
	detected := paradigm.Detect(excludedOnlySchema())
	got := paradigm.Resolve(detected, paradigm.ParadigmOLTP)

	if got.Paradigm != paradigm.ParadigmOLTP {
		t.Errorf("Paradigm = %q, want oltp", got.Paradigm)
	}
	if r := got.Roles["dim_status"]; r != paradigm.RoleUnclassified {
		t.Errorf("Roles[dim_status] = %q, want unclassified — an explicit oltp override must never "+
			"restore a role the gate withheld", r)
	}
	if got.Gate.Open || got.Gate.ByOverride {
		t.Errorf("Gate = {Open:%v ByOverride:%v}, want both false", got.Gate.Open, got.Gate.ByOverride)
	}
}

// TestResolve_ExplicitWarehouseOverride_OnAnOpenGate_ChangesNothingButParadigm:
// when detection already qualified the schema, an explicit olap/mixed has
// nothing to restore, and ByOverride must stay FALSE so the note keeps telling
// the truth about why the gate is open.
func TestResolve_ExplicitWarehouseOverride_OnAnOpenGate_ChangesNothingButParadigm(t *testing.T) {
	detected := paradigm.Detect(calendarOnlySchema())
	if !detected.Gate.Open {
		t.Fatalf("fixture drift: the gate must be OPEN here (Fired = %v)", detected.Gate.Fired)
	}

	got := paradigm.Resolve(detected, paradigm.ParadigmOLAP)
	if got.Paradigm != paradigm.ParadigmOLAP {
		t.Errorf("Paradigm = %q, want olap", got.Paradigm)
	}
	if got.Gate.ByOverride {
		t.Error("Gate.ByOverride = true, want false — evidence opened this gate, not config")
	}
	if got := got.Gate.Deciding; len(got) != 1 || got[0] != paradigm.SignalCalendarTable {
		t.Errorf("Gate.Deciding = %v, want [calendar_table] preserved", got)
	}
}
