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

// TestSchemaGate_TypeProfileSplit_AbstainsOnBracketedTSQLTypes locks the
// measured cause of the reference warehouse's abstention, which is NOT the
// ALTER TABLE parser gap the rest of the DW family hits: AdventureWorksDW
// brackets its type names, the T-SQL type map never matches them, and all 359
// of its columns parse as db.TypeUnknown. The signal fails closed on that, and
// this test is what says so with real DDL rather than with a claim.
//
// Only the FACT table is bracketed here. Its three dimensions declare the same
// T-SQL types WITHOUT brackets, so they type normally and the text pole is
// already satisfied — leaving the unclassified fact as the only thing between
// this schema and a fire. A fixture with every table bracketed would prove
// less: unclassified columns feed neither pole, so it could not fire under any
// mutation.
func TestSchemaGate_TypeProfileSplit_AbstainsOnBracketedTSQLTypes(t *testing.T) {
	bracketed := `
CREATE TABLE [dbo].[FactSales](
  [DateKey] [int] NOT NULL, [CustomerKey] [int] NOT NULL, [ProductKey] [int] NOT NULL,
  [StoreKey] [int] NOT NULL, [PromoKey] [int] NOT NULL, [ShipKey] [int] NOT NULL,
  [Quantity] [int] NOT NULL, [UnitCost] [money] NOT NULL, [ListPrice] [money] NOT NULL,
  [Discount] [money] NOT NULL, [NetPaid] [money] NOT NULL, [Profit] [money] NOT NULL
);
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
	s, unclassified := parseGateDDL(t, sqlddl.SQLServer(), bracketed)
	if len(s.Tables) != 4 {
		t.Fatalf("parsed %d tables, want 4", len(s.Tables))
	}
	// The premise, asserted exactly: the bracketed fact is entirely
	// unclassified and NOTHING else is. If the T-SQL type map learns to read
	// bracketed names, this fails — which is the point: the AdventureWorksDW
	// measurement this signal reports would then have to be redone.
	fact := s.Tables[0]
	if fact.Name != "FactSales" {
		t.Fatalf("table[0] = %q, want FactSales", fact.Name)
	}
	if unclassified != len(fact.Columns) || len(fact.Columns) == 0 {
		t.Fatalf("%d columns unclassified over the whole schema, want exactly the %d bracketed fact columns — "+
			"the bracketed-type gap this test rests on has changed; re-measure before touching this expectation",
			unclassified, len(fact.Columns))
	}

	if e := paradigm.WarehouseSignals(s); e.Has(paradigm.SignalTypeProfileSplit) {
		t.Errorf("type_profile_split fired over DDL whose every column type is unclassified, Fired = %v", e.Fired)
	}
}
