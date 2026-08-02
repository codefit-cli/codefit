package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// The schema gate's SIXTH signal (type_profile_split) through the REAL parser
// and the REAL dialect type map, not a hand-built db.Schema.
//
// This matters more here than for the other five signals: this one reasons over
// db.Column.Type, which NO hand-built fixture produces the way a dialect does.
// A fixture that sets Type itself proves the arithmetic and nothing about
// whether a real CREATE TABLE ever lands on those values — the exact
// "fixture holds what production cannot produce" failure CLAUDE.md names.
//
// The DDL below is CONSTRUCTED rather than vendored, deliberately: no corpus
// this repository vendors exhibits the split (measured — see
// schemagate_corpus_test.go, where the signal fires on nothing), and the real
// public warehouses that DO exhibit it are not vendored here.

// parseGateDDL parses inline DDL and returns the schema plus a census of
// unclassified column types, so a test can prove the dialect actually typed the
// columns before it concludes anything about their profile.
func parseGateDDL(t *testing.T, d sqlddl.Dialect, ddl string) (*db.Schema, int) {
	t.Helper()
	p := sqlddl.New(sqlddl.WithDialect(d))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(ddl)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	unclassified := 0
	for _, tbl := range s.Tables {
		for _, c := range tbl.Columns {
			if c.Type == db.TypeUnknown || c.Type == "" {
				unclassified++
			}
		}
	}
	return s, unclassified
}

const warehouseSplitDDL = `
CREATE TABLE sales_fact (
  date_key       integer NOT NULL,
  customer_key   integer NOT NULL,
  product_key    integer NOT NULL,
  store_key      integer NOT NULL,
  promo_key      integer NOT NULL,
  ship_key       integer NOT NULL,
  quantity       integer NOT NULL,
  unit_cost      numeric(10,2) NOT NULL,
  list_price     numeric(10,2) NOT NULL,
  discount_amt   numeric(10,2) NOT NULL,
  net_paid       numeric(10,2) NOT NULL,
  profit         numeric(10,2) NOT NULL
);
CREATE TABLE customer_dimension (
  customer_key integer NOT NULL,
  first_name   varchar(50),
  last_name    varchar(50),
  email        varchar(120),
  city         varchar(60),
  country      varchar(60)
);
CREATE TABLE product_dimension (
  product_key  integer NOT NULL,
  product_name varchar(120),
  brand        varchar(60),
  category     varchar(60),
  description  text
);
CREATE TABLE store_dimension (
  store_key  integer NOT NULL,
  store_name varchar(80),
  manager    varchar(80),
  address    varchar(160)
);
`

// TestSchemaGate_TypeProfileSplit_FiresOnRealWarehouseDDL is the positive
// control: one wide numeric-dominated table plus three text-dominated ones,
// through the real PostgreSQL type map (integer/numeric -> numeric,
// varchar/text -> descriptive).
func TestSchemaGate_TypeProfileSplit_FiresOnRealWarehouseDDL(t *testing.T) {
	s, unclassified := parseGateDDL(t, sqlddl.Postgres(), warehouseSplitDDL)

	// Parse facts first: a dialect that stopped mapping types would leave every
	// column unclassified, and the signal would then abstain for a reason that
	// has nothing to do with what this test claims to prove.
	if len(s.Tables) != 4 {
		t.Fatalf("parsed %d tables, want 4", len(s.Tables))
	}
	if unclassified != 0 {
		t.Fatalf("%d columns parsed as unclassified, want 0 — the dialect is not typing this DDL", unclassified)
	}
	if got := len(s.Tables[0].Columns); got != 12 {
		t.Fatalf("sales_fact has %d columns, want 12", got)
	}

	e := paradigm.WarehouseSignals(s)
	if !e.Has(paradigm.SignalTypeProfileSplit) {
		t.Errorf("type_profile_split did not fire over warehouse-shaped DDL, Fired = %v", e.Fired)
	}
}

const oltpNoSplitDDL = `
CREATE TABLE orders (
  order_id     integer NOT NULL,
  customer_id  integer NOT NULL,
  status       varchar(20),
  placed_at    timestamp,
  shipped_at   timestamp,
  total        numeric(10,2)
);
CREATE TABLE order_items (
  order_id   integer NOT NULL,
  product_id integer NOT NULL,
  quantity   integer NOT NULL,
  unit_price numeric(10,2) NOT NULL,
  discount   numeric(10,2) NOT NULL
);
CREATE TABLE customers (
  customer_id integer NOT NULL,
  first_name  varchar(50),
  last_name   varchar(50),
  email       varchar(120),
  signed_up   timestamp
);
CREATE TABLE products (
  product_id  integer NOT NULL,
  name        varchar(120),
  description text,
  brand       varchar(60),
  price       numeric(10,2),
  updated     timestamp
);
`

// TestSchemaGate_TypeProfileSplit_DoesNotFireOnRealOLTPDDL is the negative
// control, and it carries the shape the signal is most likely to be wrong
// about: order_items is 100% numeric and has an exact fact profile. It is five
// columns wide, and that is the whole reason it is not a pole.
//
// customers and products are text-dominated on purpose, so the text pole is
// already satisfied and the WIDTH FLOOR is the only thing left holding: drop
// it to 5 and this schema fires.
func TestSchemaGate_TypeProfileSplit_DoesNotFireOnRealOLTPDDL(t *testing.T) {
	s, unclassified := parseGateDDL(t, sqlddl.Postgres(), oltpNoSplitDDL)
	if len(s.Tables) != 4 {
		t.Fatalf("parsed %d tables, want 4", len(s.Tables))
	}
	if unclassified != 0 {
		t.Fatalf("%d columns parsed as unclassified, want 0", unclassified)
	}

	// The trap is real, not asserted in prose: order_items IS all-numeric.
	items := s.Tables[1]
	if items.Name != "order_items" {
		t.Fatalf("table[1] = %q, want order_items", items.Name)
	}
	for _, c := range items.Columns {
		if c.Type != db.TypeInt && c.Type != db.TypeFloat {
			t.Fatalf("order_items.%s is %q — the fixture no longer carries the all-numeric trap", c.Name, c.Type)
		}
	}

	if e := paradigm.WarehouseSignals(s); e.Has(paradigm.SignalTypeProfileSplit) {
		t.Errorf("type_profile_split fired over transactional DDL, Fired = %v", e.Fired)
	}
}

// TestSchemaGate_TypeProfileSplit_UnclassifiedBudget locks the FAIL-CLOSED
// budget (maxUnclassifiedPct): a table whose column types the dialect could not
// classify beyond one in five is not profiled at all, because a proportion
// computed over unclassified types is a guess.
//
// THIS TEST USED TO REST ON THE WRONG CAUSE, and its predecessor said so in
// writing: it declared that AdventureWorksDW abstains because it BRACKETS its
// type names ([int], [nvarchar](50)), and it told its successor that "if the
// T-SQL type map learns to read bracketed names, this fails — re-measure before
// touching this expectation". It has. A delimited type name is now unwrapped
// before the TypeMap lookup (internal/providers/sqlddl/types.go, typeLookupKey),
// so bracketing classifies exactly like the bare word and is no longer a cause
// of anything. The re-measurement, through the real sensor:
//
//   - the vendored 3-table excerpt (testdata/tsql/adventureworksdw_real_objects.sql)
//     goes from 74 of 74 columns unclassified to 0 of 74;
//   - the FULL upstream install script, which is NOT vendored here, goes from
//     359 of 359 to 6 of 359 — and those 6 are the honest fallback still
//     working, not a residue of the old gap: five [sysname] columns and one
//     [xml], both real T-SQL types deliberately absent from sqlserverTypeMap.
//
// IT ALSO USED TO BE AN ORNAMENT WITH RESPECT TO THE BUDGET, which the rewrite
// is really for. Its fact table was 100% unclassified, so deleting the budget
// from profileOf changed NOTHING: the fact became profiled with zero numeric
// and zero descriptive columns, qualified as neither pole, and the signal still
// did not fire. It asserted the outcome while protecting none of the mechanism
// — proven by mutation, which SURVIVED.
//
// So the fixture is now placed on the BOUNDARY and the test is two-sided. The
// fact is numeric-dominated in every respect EXCEPT its unclassified share, and
// the only difference between the two sub-cases is one more [sql_variant]
// column: 3 of 12 (25%) is over the budget and abstains, 2 of 12 (17%) is under
// it and fires. Delete the budget and the first sub-case fires — the mutation is
// now available, and was run.
//
// [sql_variant] is a real T-SQL type deliberately absent from sqlserverTypeMap,
// and the one whose whole meaning is "the type is not fixed". The schema is
// CONSTRUCTED and declared synthetic (ADR 0028).
func TestSchemaGate_TypeProfileSplit_UnclassifiedBudget(t *testing.T) {
	// dims supplies the text pole (three text-dominated tables) so that the
	// FACT's profile is the only variable between the two sub-cases below.
	const dims = `
CREATE TABLE DimCustomer(
  CustomerKey int NOT NULL, FirstName nvarchar(50), LastName nvarchar(50),
  Email nvarchar(120), City nvarchar(60), Country nvarchar(60)
);
CREATE TABLE DimProduct(
  ProductKey int NOT NULL, ProductName nvarchar(120), Brand nvarchar(60),
  Category nvarchar(60), Descr nvarchar(400)
);
CREATE TABLE DimStore(
  StoreKey int NOT NULL, StoreName nvarchar(80), Manager nvarchar(80),
  Address nvarchar(160)
);
`
	// Both facts are 12 columns wide with ZERO descriptive columns; they differ
	// only in how many of those 12 the dialect cannot classify.
	overBudget := `
CREATE TABLE [dbo].[FactSales](
  [DateKey] [int] NOT NULL, [CustomerKey] [int] NOT NULL, [ProductKey] [int] NOT NULL,
  [StoreKey] [int] NOT NULL, [PromoKey] [int] NOT NULL, [ShipKey] [int] NOT NULL,
  [Quantity] [int] NOT NULL, [UnitCost] [money] NOT NULL, [ListPrice] [money] NOT NULL,
  [Discount] [sql_variant] NOT NULL, [NetPaid] [sql_variant] NOT NULL, [Profit] [sql_variant] NOT NULL
);` + dims
	underBudget := `
CREATE TABLE [dbo].[FactSales](
  [DateKey] [int] NOT NULL, [CustomerKey] [int] NOT NULL, [ProductKey] [int] NOT NULL,
  [StoreKey] [int] NOT NULL, [PromoKey] [int] NOT NULL, [ShipKey] [int] NOT NULL,
  [Quantity] [int] NOT NULL, [UnitCost] [money] NOT NULL, [ListPrice] [money] NOT NULL,
  [Discount] [money] NOT NULL, [NetPaid] [sql_variant] NOT NULL, [Profit] [sql_variant] NOT NULL
);` + dims

	for _, tc := range []struct {
		name             string
		ddl              string
		wantUnclassified int
		wantFire         bool
	}{
		{"3_of_12_over_the_20pct_budget", overBudget, 3, false},
		{"2_of_12_under_the_20pct_budget", underBudget, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, unclassified := parseGateDDL(t, sqlddl.SQLServer(), tc.ddl)
			if len(s.Tables) != 4 {
				t.Fatalf("parsed %d tables, want 4", len(s.Tables))
			}
			fact := s.Tables[0]
			if fact.Name != "FactSales" || len(fact.Columns) != 12 {
				t.Fatalf("table[0] = %q with %d columns, want FactSales with 12", fact.Name, len(fact.Columns))
			}
			// The premise, asserted exactly rather than assumed: the unclassified
			// columns are the [sql_variant] ones and NOTHING else is unclassified.
			// A fixture whose premise drifted would make the verdict meaningless.
			if unclassified != tc.wantUnclassified {
				t.Fatalf("%d columns unclassified over the whole schema, want exactly %d — "+
					"if sqlserverTypeMap learned [sql_variant], choose another unmapped keyword, "+
					"never a weaker assertion", unclassified, tc.wantUnclassified)
			}
			got := paradigm.WarehouseSignals(s)
			if fired := got.Has(paradigm.SignalTypeProfileSplit); fired != tc.wantFire {
				t.Errorf("type_profile_split fired = %v, want %v (%d of 12 fact columns unclassified), Fired = %v",
					fired, tc.wantFire, tc.wantUnclassified, got.Fired)
			}
		})
	}
}
