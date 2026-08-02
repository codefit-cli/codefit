package paradigm_test

import (
	"fmt"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// SIGNAL 6 — column-type profile split.
//
// The other five signals read NAMES and RELATIONAL STRUCTURE. This one reads
// the one thing the neutral model already carries and nobody asked: db.Type.
//
// It is deliberately NOT a per-table test. order_items(order_id int,
// product_id int, quantity int, price float) has an exact fact profile, and
// northwind's order_details and chinook's invoice_line — both measured — have
// it too. What a warehouse has and a transactional schema does not is the
// SCHEMA-LEVEL SPLIT: a few numeric-dominated tables plus several
// text-dominated ones, with the numeric pole large enough to be a pole.
//
// The fixtures here carry REAL db.Type values, because that is the whole
// subject. typedTable is the only fixture builder in this package that sets
// them; provenTable (schemagate_test.go) leaves Type at its zero value, which
// this signal treats as UNCLASSIFIED — locked below, since it is exactly the
// fail-closed default the AdventureWorksDW measurement depends on.

// columnGroup is n columns of one neutral type.
type columnGroup struct {
	t db.Type
	n int
}

func group(t db.Type, n int) columnGroup { return columnGroup{t: t, n: n} }

// typedTable builds a structure-PROVEN table whose columns carry REAL neutral
// types. Column names are generated WITHOUT an underscore on purpose: a
// trailing `_id`/`_sk`/`_key` segment would drag the surrogate-key and
// bulk-load signals into every fixture here and turn an exact-set assertion
// into noise.
func typedTable(name string, groups ...columnGroup) db.Table {
	tb := db.Table{Name: name, Complete: true}
	i := 0
	for _, g := range groups {
		for n := 0; n < g.n; n++ {
			i++
			tb.Columns = append(tb.Columns, db.Column{Name: fmt.Sprintf("c%d%s", i, g.t), Type: g.t})
		}
	}
	return tb
}

// factShaped is the measured shape of a real fact table: wide, numeric, no
// descriptive text (TPC-DS store_sales is 23 columns and 23 numeric).
func factShaped(name string) db.Table {
	return typedTable(name, group(db.TypeInt, 9), group(db.TypeFloat, 3))
}

// dimShaped is the measured shape of a real dimension: a few keys plus a
// majority of descriptive attributes (TPC-DS customer_address is 13 columns,
// 11 of them descriptive).
func dimShaped(name string) db.Table {
	return typedTable(name, group(db.TypeString, 5), group(db.TypeInt, 2))
}

// mixedShaped is the measured shape of an ordinary OLTP table: numeric, text
// and datetime in comparable proportions, dominated by nothing.
func mixedShaped(name string) db.Table {
	return typedTable(name, group(db.TypeInt, 2), group(db.TypeString, 2), group(db.TypeDateTime, 2))
}

// warehouseSplit is the positive fixture: one numeric pole, three text-pole
// tables. no_audit_timestamps fires alongside it because these tables carry no
// created_at — that is a true fact about the fixture, asserted rather than
// hidden.
func warehouseSplit() *db.Schema {
	return &db.Schema{Tables: []db.Table{
		factShaped("sales"),
		dimShaped("customer"),
		dimShaped("product"),
		dimShaped("store"),
	}}
}

// ---------------------------------------------------------------------------
// The positive: a schema that SPLITS
// ---------------------------------------------------------------------------

func TestSignalTypeProfileSplit_FiresOnBimodalSchema(t *testing.T) {
	assertFired(t, paradigm.WarehouseSignals(warehouseSplit()),
		paradigm.SignalNoAuditTimestamps, paradigm.SignalTypeProfileSplit)
}

// TestSignalTypeProfileSplit_WideNumericTableCarryingTextIsNotAFact is the
// text-cap threshold, and the fixture is a measured real table: northwind's
// products is 10 columns, 8 numeric and 2 descriptive — wide enough and
// numeric enough, and NOT a fact. A fact row is keys and measures; two text
// columns in ten is a product catalogue.
func TestSignalTypeProfileSplit_WideNumericTableCarryingTextIsNotAFact(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		typedTable("products", group(db.TypeInt, 8), group(db.TypeString, 2)),
		typedTable("suppliers", group(db.TypeInt, 8), group(db.TypeString, 2)),
		dimShaped("customer"), dimShaped("employee"), dimShaped("shipper"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalNoAuditTimestamps)
}

// ---------------------------------------------------------------------------
// The discriminators, each measured over real corpora
// ---------------------------------------------------------------------------

// TestSignalTypeProfileSplit_NarrowAllNumericTableIsNotAPole is the width
// floor, and it is the single most load-bearing threshold in this signal.
// order_items(order_id, product_id, quantity, price) is 100% numeric and is a
// join table, not a fact — measured in northwind (order_details, 5 columns, 5
// numeric), chinook (invoice_line, 5/5) and pagila (22 payment partitions,
// 6 columns, 5 numeric). Dropping the floor below 8 makes pagila and Sakila
// fire.
func TestSignalTypeProfileSplit_NarrowAllNumericTableIsNotAPole(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		typedTable("orderitems", group(db.TypeInt, 4), group(db.TypeFloat, 1)),
		dimShaped("customer"), dimShaped("product"), dimShaped("store"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalNoAuditTimestamps)
}

// TestSignalTypeProfileSplit_LoneNumericTableInALargeSchemaIsNotAPole is the
// share floor. Synapse — a chat server's OLTP schema, 134 tables — has exactly
// ONE wide numeric-dominated table (room_stats_current, a stats aggregate) and
// 67 text-dominated ones. One table in 133 is an outlier, not a pole.
func TestSignalTypeProfileSplit_LoneNumericTableInALargeSchemaIsNotAPole(t *testing.T) {
	tables := []db.Table{factShaped("stats")}
	for i := 0; i < 11; i++ {
		tables = append(tables, dimShaped(fmt.Sprintf("texty%d", i)))
	}
	assertFired(t, paradigm.WarehouseSignals(&db.Schema{Tables: tables}),
		paradigm.SignalNoAuditTimestamps)
}

// TestSignalTypeProfileSplit_OneTextTableIsNotSeveral: "a few numeric-dominated
// tables plus SEVERAL text-dominated ones". One text-dominated table is a
// lookup table, and every schema has one.
func TestSignalTypeProfileSplit_OneTextTableIsNotSeveral(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		factShaped("sales"), dimShaped("customer"),
		mixedShaped("orders"), mixedShaped("shipments"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalNoAuditTimestamps)
}

// ---------------------------------------------------------------------------
// FAIL CLOSED ON WHAT THE PARSER DID NOT CLASSIFY
// ---------------------------------------------------------------------------

// TestSignalTypeProfileSplit_UnclassifiedTypesAbstain is not a hypothetical
// guard: a profile computed over types the parser did not classify is a guess.
//
// THE WORKED EXAMPLE THIS COMMENT USED TO CITE IS GONE. It read "the real
// AdventureWorksDW install script parses with 359 of 359 columns at
// db.TypeUnknown, because its T-SQL brackets its type names ([int],
// [nvarchar](50)) and the dialect's type map never sees them". That was a
// parser defect and it is fixed (internal/providers/sqlddl/types.go,
// typeLookupKey); re-measured, the same script parses with 6 of 359
// unclassified — [sysname] and [xml], genuinely outside the T-SQL vocabulary.
// The GUARD is unchanged and still needed: an unmapped keyword still yields
// db.TypeUnknown, which is exactly the state this test constructs.
//
// The fixture puts the unclassified columns exactly where they can do damage:
// the WOULD-BE numeric pole. Its three text-dominated companions are real, so
// every other guard is already clear and only the unclassified rule can stop
// the fire. (A fixture where every table is unclassified proves less than it
// looks: unclassified columns feed neither pole, so such a schema cannot fire
// under any threshold, and the test could never have failed.)
func TestSignalTypeProfileSplit_UnclassifiedTypesAbstain(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		typedTable("sales", group(db.TypeUnknown, 12)),
		dimShaped("customer"), dimShaped("product"), dimShaped("store"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalNoAuditTimestamps)
}

// TestSignalTypeProfileSplit_TableOverTheUnknownBudgetIsNotProfiled isolates
// the per-table budget: the schema still has three profiled text-pole tables
// and clears every other guard, so ONLY the unclassified share of the numeric
// table can stop it. 3 of 12 columns is 25%, over the 20% budget.
func TestSignalTypeProfileSplit_TableOverTheUnknownBudgetIsNotProfiled(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		typedTable("sales", group(db.TypeInt, 9), group(db.TypeUnknown, 3)),
		dimShaped("customer"), dimShaped("product"), dimShaped("store"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalNoAuditTimestamps)
}

// TestSignalTypeProfileSplit_TableAtTheUnknownBudgetIsProfiled is the other
// side of the same line: 2 of 10 columns is exactly 20%, which is inside the
// budget, so the table is profiled and the schema splits.
func TestSignalTypeProfileSplit_TableAtTheUnknownBudgetIsProfiled(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		typedTable("sales", group(db.TypeInt, 8), group(db.TypeUnknown, 2)),
		dimShaped("customer"), dimShaped("product"), dimShaped("store"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s),
		paradigm.SignalNoAuditTimestamps, paradigm.SignalTypeProfileSplit)
}

// TestSignalTypeProfileSplit_ZeroValueTypeIsUnclassified locks the fail-closed
// DEFAULT. db.Column.Type's zero value is "" — not db.TypeUnknown — and a
// hand-built fixture that never set it must not be read as a profile. This is
// also why every OTHER fixture in this package (provenTable, which leaves Type
// empty) is untouched by this signal.
func TestSignalTypeProfileSplit_ZeroValueTypeIsUnclassified(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		provenTable("sales", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"),
		dimShaped("customer"), dimShaped("product"), dimShaped("store"),
	}}
	assertFired(t, paradigm.WarehouseSignals(s), paradigm.SignalNoAuditTimestamps)
}

// TestSignalTypeProfileSplit_UnprovenTableIsNotProfiled: a dropped ADD COLUMN
// changes a proportion, so a table whose structure is not proven complete is
// excluded from the distribution — the same completeness contract every other
// signal here consults (ADR 0034).
func TestSignalTypeProfileSplit_UnprovenTableIsNotProfiled(t *testing.T) {
	s := warehouseSplit()
	s.Tables[0].MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE sales ADD ...", db.Pos{})
	e := paradigm.WarehouseSignals(s)
	if e.Has(paradigm.SignalTypeProfileSplit) {
		t.Errorf("type_profile_split fired with the numeric pole unproven, Fired = %v", firedNames(e))
	}
}

// TestSignalTypeProfileSplit_ProfileMustCoverTheSchema is the coverage floor,
// and it is the same NO-VACUOUS-TRUTHS doctrine the 3-table floor rests on:
// "this SCHEMA splits into two poles" cannot be concluded from a minority of
// the schema. Here four tables profile and split exactly as in the positive
// fixture, but five more are unreadable, so the claim would rest on 44% of the
// tables.
func TestSignalTypeProfileSplit_ProfileMustCoverTheSchema(t *testing.T) {
	s := warehouseSplit()
	for i := 0; i < 5; i++ {
		tb := typedTable(fmt.Sprintf("opaque%d", i), group(db.TypeInt, 4))
		tb.MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE ...", db.Pos{})
		s.Tables = append(s.Tables, tb)
	}
	e := paradigm.WarehouseSignals(s)
	if e.Has(paradigm.SignalTypeProfileSplit) {
		t.Errorf("type_profile_split fired over 4 profiled tables of 9, Fired = %v", firedNames(e))
	}
}

// NOT WRITTEN, and stated so nobody adds it back believing it protects
// something: a "the 3-table floor gates this signal too" test. The text pole
// needs two tables and the numeric pole one, so the smallest schema that can
// fire at all already has three — no 2-table fixture exists that a lowered
// floor would let through, and the test could never fail. The floor is real
// (WarehouseSignals returns before any signal runs); it is simply not
// observable through THIS signal.
